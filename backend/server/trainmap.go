package main

import (
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jeinarsson/wall/basemap"
	"github.com/jeinarsson/wall/transit"
)

// trainMap is a map of the tracks around the station, with the trains on
// their way to it. It serves the map (/api/map), which changes daily, and
// the trains on it (/api/map/trains).
//
// The LIRR trains are where their GPS says, and go on at their speed until
// the next fix. Subway trains have no GPS (they're underground); they are
// placed from their arrivals at stops (see transit.SubwayModel).
type trainMap struct {
	view mapView
	area transit.BBox // trains and tracks shown; somewhat more than view
	// Subway trains on routes that will still leave from platform.
	subwayRoutes   []string
	subwayPlatform string // "G08S"
	subwayModel    transit.SubwayModel
	// LIRR trains in direction that will still stop at lirrStation, going
	// on from their last GPS fix for at most lirrAhead.
	lirrStation   string // GTFS stop id: "55"
	lirrDirection string // "E" or "W"
	lirrAhead     time.Duration

	subwayTrips  []*transit.TripFeed
	subwayStatic *transit.File[*subwayStatic]
	// arrivals is updated on every request for the trains, which the
	// frontend makes more often than the feeds are refreshed.
	arrivals   transit.Arrivals
	lirrTrains *lirrPositions
	lirrStatic *transit.File[*lirrStatic]
	basemap    *transit.File[*basemap.Layers]

	mu     sync.Mutex
	cached mapCache
}

// mapView is the part of the map shown: Width x Height meters around
// Center, turned Rotate degrees counterclockwise, so that the tracks can
// run along the screen.
type mapView struct {
	Center        transit.LatLon
	Width, Height float64
	Rotate        float64
}

// bounds is a bounding box of the view and margin meters around it.
func (v mapView) bounds(margin float64) transit.BBox {
	k := math.Cos(v.Center.Lat*math.Pi/180) * 111_320
	sin, cos := math.Sincos(v.Rotate * math.Pi / 180)
	b := transit.BBox{West: math.Inf(1), South: math.Inf(1), East: math.Inf(-1), North: math.Inf(-1)}
	for _, c := range [][2]float64{{-1, -1}, {-1, 1}, {1, -1}, {1, 1}} {
		// A corner of the view, turned back to north up.
		x, y := c[0]*(v.Width/2+margin), c[1]*(v.Height/2+margin)
		lon := v.Center.Lon + (x*cos+y*sin)/k
		lat := v.Center.Lat + (-x*sin+y*cos)/111_320
		b = transit.BBox{West: min(b.West, lon), South: min(b.South, lat), East: max(b.East, lon), North: max(b.North, lat)}
	}
	return b
}

// Car lengths in meters, and how many cars when the feed doesn't say.
const (
	subwayCarLength = 18.4 // 60 ft, as on the E and F
	subwayCars      = 10
	lirrCarLength   = 25.9 // 85 ft, an M7 or M9
	lirrCars        = 8
)

// nearTrack is how close to a track a stop or GPS fix must be to be on it,
// in meters.
const nearTrack = 60

type mapJSON struct {
	View     viewJSON
	Basemap  basemap.Layers // nil until loaded
	Tracks   []trackJSON
	Stations []stationJSON
	// Complete is false until all the static data has been loaded; until
	// then, ask again soon.
	Complete bool
}

type viewJSON struct {
	Center        [2]float64 // lon, lat
	Width, Height float64    // meters
	Rotate        float64    // degrees counterclockwise
}

// trackJSON is a set of track segments: [lon, lat, lon, lat], ...
type trackJSON struct {
	Kind     string // "lirr" or "subway"
	Segments [][4]float64
}

type stationJSON struct {
	Name string
	Kind string
	Lon  float64
	Lat  float64
	Home bool // the station the map is for
}

type trainsJSON struct {
	Trains []mapTrainJSON
}

// mapTrainJSON is a train on Path, a list of lon, lat. Its middle follows
// Knots along Path, easing between them with Ease (see
// transit.PositionAt).
type mapTrainJSON struct {
	ID    string
	Kind  string // "lirr" or "subway"
	Label string // route or train number
	colorJSON
	Cars      int
	CarLength float64 // meters
	Path      []float64
	Knots     []transit.Knot
	Ease      bool
}

// mapCache is the map as JSON, made from the static data it holds.
type mapCache struct {
	lirr   *lirrStatic
	subway *subwayStatic
	layers *basemap.Layers
	json   []byte
}

// ServeMap serves the map: what it shows, the basemap, tracks and stations.
func (m *trainMap) ServeMap(w http.ResponseWriter, r *http.Request) {
	lirr, _ := m.lirrStatic.Get()
	subway, _ := m.subwayStatic.Get()
	layers, _ := m.basemap.Get()

	m.mu.Lock()
	c := m.cached
	if c.json == nil || c.lirr != lirr || c.subway != subway || c.layers != layers {
		c = mapCache{lirr, subway, layers, nil}
		c.json, _ = json.Marshal(m.board(lirr, subway, layers))
		m.cached = c
	}
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Write(c.json)
}

// board is the map, with whichever static data has been loaded.
func (m *trainMap) board(lirr *lirrStatic, subway *subwayStatic, layers *basemap.Layers) mapJSON {
	out := mapJSON{
		View: viewJSON{
			Center: [2]float64{m.view.Center.Lon, m.view.Center.Lat},
			Width:  m.view.Width, Height: m.view.Height, Rotate: m.view.Rotate,
		},
		Tracks:   []trackJSON{},
		Stations: []stationJSON{},
		Complete: lirr != nil && subway != nil && layers != nil,
	}
	if layers != nil {
		out.Basemap = *layers
	}
	if lirr != nil {
		out.Tracks = append(out.Tracks, trackJSON{"lirr", segments(transit.Network(sortedShapes(lirr.shapes), m.area))})
		out.Stations = append(out.Stations, m.stations(lirr.stations, lirr.stops, "lirr", m.lirrStation)...)
	}
	if subway != nil {
		out.Tracks = append(out.Tracks, trackJSON{"subway", segments(transit.Network(sortedShapes(subway.shapes), m.area))})
		out.Stations = append(out.Stations, m.stations(subway.stations, subway.stops, "subway", station(m.subwayPlatform))...)
	}
	return out
}

// stations lists the stations in the area on any of the shapes.
func (m *trainMap) stations(byShape map[string]map[string]float64, stops map[string]transit.Stop, kind, home string) []stationJSON {
	seen := map[string]bool{}
	var out []stationJSON
	for _, stations := range byShape {
		for id := range stations {
			s := stops[id]
			if seen[id] || !m.area.Contains(s.LatLon) {
				continue
			}
			seen[id] = true
			out = append(out, stationJSON{Name: s.Name, Kind: kind, Lon: s.Lon, Lat: s.Lat, Home: id == home})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ServeTrains serves the trains on the map.
func (m *trainMap) ServeTrains(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	out := trainsJSON{Trains: []mapTrainJSON{}}
	if static, ok := m.lirrStatic.Get(); ok {
		for _, t := range m.lirrTrains.Get(now) {
			if tr, ok := m.lirrTrain(static, t); ok {
				out.Trains = append(out.Trains, tr)
			}
		}
	}
	if static, ok := m.subwayStatic.Get(); ok {
		var trips []transit.LiveTrip
		for _, f := range m.subwayTrips {
			if msg, ok := f.Get(now); ok {
				trips = append(trips, transit.LiveTrips(msg)...)
			}
		}
		m.arrivals.Update(trips, now)
		for _, t := range trips {
			if tr, ok := m.subwayTrain(static, t, now); ok {
				out.Trains = append(out.Trains, tr)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// wantsLIRR reports whether t is a train for the map: in the right
// direction and still to stop at the station.
func (m *trainMap) wantsLIRR(static *lirrStatic, t transit.TrainLocation) bool {
	if t.Canceled || t.Direction != m.lirrDirection {
		return false
	}
	home := static.stops[m.lirrStation].Code
	for _, s := range t.Stops {
		if s.Code == home {
			return !s.Departed && !s.Canceled
		}
	}
	return false
}

// lirrTrain places an LIRR train on its shape (or else the nearest track)
// and plans its way from its GPS fix to its next stop.
func (m *trainMap) lirrTrain(static *lirrStatic, t transit.TrainLocation) (mapTrainJSON, bool) {
	if !m.area.Contains(t.LatLon) || !m.wantsLIRR(static, t) {
		return mapTrainJSON{}, false
	}
	// The train's own shape, so that it follows the right branch; or else
	// the nearest one in its direction.
	trip := static.trains[t.TrainNum]
	shapeID := trip.ShapeID
	path, ok := static.shapes[shapeID]
	along, off := path.Locate(t.LatLon)
	if !ok || off > nearTrack {
		for id, p := range static.shapes {
			if a, o := p.Locate(t.LatLon); o < off && heading(p, a, t.Heading) {
				shapeID, path, along, off = id, p, a, o
			}
		}
	}
	if off > nearTrack {
		return mapTrainJSON{}, false
	}

	piece, start := path.Within(m.area, along)
	until := piece.Length()
	codes := map[string]string{}
	for id := range static.stations[shapeID] {
		codes[static.stops[id].Code] = id
	}
	for _, s := range t.Stops {
		if a, ok := static.stations[shapeID][codes[s.Code]]; ok && !s.Departed && !s.Canceled && a >= along {
			until = a - start
			break
		}
	}
	speed := t.Speed
	if !t.GPS {
		speed = 0
	}
	cars := t.Cars
	if cars == 0 {
		cars = lirrCars
	}
	return mapTrainJSON{
		ID:        "lirr-" + t.TrainNum,
		Kind:      "lirr",
		Label:     t.TrainNum,
		colorJSON: routeColor(static.routes[trip.RouteID]),
		Cars:      cars,
		CarLength: lirrCarLength,
		Path:      flatten(piece.Points),
		Knots:     transit.DeadReckon(t.Time, along-start, speed, until, m.lirrAhead),
	}, true
}

// heading reports whether p runs within 90° of heading (degrees clockwise
// from north) at along.
func heading(p transit.Path, along, heading float64) bool {
	a, b := p.At(along-10), p.At(along+10)
	east := (b.Lon - a.Lon) * math.Cos(a.Lat*math.Pi/180)
	north := b.Lat - a.Lat
	sin, cos := math.Sincos(heading * math.Pi / 180)
	return east*sin+north*cos > 0
}

// subwayTrain places a subway train on its shape, if it is one for the
// map: on the right route, under way, and still to leave from the
// platform.
func (m *trainMap) subwayTrain(static *subwayStatic, t transit.LiveTrip, now time.Time) (mapTrainJSON, bool) {
	started := !t.Reported.IsZero() && !t.Reported.After(now)
	if !slices.Contains(m.subwayRoutes, t.RouteID) || !started || !leavesFrom(t, m.subwayPlatform, now) {
		return mapTrainJSON{}, false
	}
	shapeID, ok := tripShape(static, t)
	if !ok {
		return mapTrainJSON{}, false
	}
	path, stations := static.shapes[shapeID], static.stations[shapeID]
	along := func(stopID string) (float64, bool) {
		a, ok := stations[station(stopID)]
		return a, ok
	}
	all := make([]float64, 0, len(stations))
	for _, a := range stations {
		all = append(all, a)
	}
	sort.Float64s(all)

	var last *transit.Arrival
	if a, ok := m.arrivals.Get(t.TripID); ok {
		last = &a
	}
	knots := m.subwayModel.Knots(t, last, along, all, now)
	pos, ok := transit.PositionAt(knots, true, now)
	if !ok || !m.area.Contains(path.At(pos)) {
		return mapTrainJSON{}, false
	}
	piece, start := path.Within(m.area, pos)
	for i := range knots {
		knots[i].Along -= start
	}
	return mapTrainJSON{
		ID:        "subway-" + t.TripID,
		Kind:      "subway",
		Label:     t.RouteID,
		colorJSON: routeColor(static.routes[t.RouteID]),
		Cars:      subwayCars,
		CarLength: subwayCarLength,
		Path:      flatten(piece.Points),
		Knots:     knots,
		Ease:      true,
	}, true
}

// tripShape finds the shape of a subway trip. NYCT trip ids end with it
// ("089900_E..S69R"), but some only with the route and direction
// ("121550_F..S"); then it is the shape of that route and direction on
// which most of the trip's stops are.
func tripShape(static *subwayStatic, t transit.LiveTrip) (string, bool) {
	_, id, _ := strings.Cut(t.TripID, "_")
	if _, ok := static.shapes[id]; ok {
		return id, true
	}
	best, most := "", 0
	for shape, stations := range static.stations {
		if !strings.HasPrefix(shape, id) {
			continue
		}
		n := 0
		for _, s := range t.Stops {
			if _, ok := stations[station(s.StopID)]; ok {
				n++
			}
		}
		if n > most || (n == most && shape < best) {
			best, most = shape, n
		}
	}
	return best, most > 0
}

// station is the station of a subway platform: "G08" for "G08S".
func station(platform string) string {
	if strings.HasSuffix(platform, "N") || strings.HasSuffix(platform, "S") {
		return platform[:len(platform)-1]
	}
	return platform
}

// leavesFrom reports whether t is still to leave stop.
func leavesFrom(t transit.LiveTrip, stop string, now time.Time) bool {
	for _, s := range t.Stops {
		if s.StopID == stop && s.Time.After(now) {
			return true
		}
	}
	return false
}

func sortedShapes(shapes map[string]transit.Path) []transit.Path {
	ids := make([]string, 0, len(shapes))
	for id := range shapes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	paths := make([]transit.Path, len(ids))
	for i, id := range ids {
		paths[i] = shapes[id]
	}
	return paths
}

func segments(segs [][2]transit.LatLon) [][4]float64 {
	out := make([][4]float64, len(segs))
	for i, s := range segs {
		out[i] = [4]float64{s[0].Lon, s[0].Lat, s[1].Lon, s[1].Lat}
	}
	return out
}

func flatten(pts []transit.LatLon) []float64 {
	out := make([]float64, 0, 2*len(pts))
	for _, p := range pts {
		out = append(out, p.Lon, p.Lat)
	}
	return out
}
