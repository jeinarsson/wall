package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jeinarsson/wall/transit"
)

// trainMap is a map of the tracks around the station, with the trains
// that are on their way to it. The LIRR trains are where their GPS says;
// subway trains have no GPS (they're underground), so their way is planned
// from the predicted times at their next stops.
type trainMap struct {
	area transit.BBox
	// Subway trains on routes that will still leave from platform.
	subwayRoutes   []string
	subwayPlatform string // "G08S"
	// LIRR trains in direction that will still stop at lirrStation.
	lirrStation   string // GTFS stop id: "55"
	lirrDirection string // "E" or "W"

	subwayTrips  []*transit.TripFeed
	subwayStatic *transit.File[*subwayStatic]
	// arrivals is updated on every request, which the frontend makes more
	// often than the feeds are refreshed.
	arrivals   arrivals
	lirrTrains *transit.LocationFeed
	lirrStatic *transit.File[*lirrStatic]
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
	Tracks   []trackJSON
	Stations []stationJSON
	Trains   []mapTrainJSON
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

// mapTrainJSON is a train on Path, a list of lon, lat. Its middle moves
// from knot to knot along Path: steadily, or if Ease is set, speeding up
// after each knot and slowing down into the next, like a train between
// stations. Before the first knot and after the last it stands still.
type mapTrainJSON struct {
	ID    string
	Kind  string // "lirr" or "subway"
	Label string // route or train number
	colorJSON
	Cars      int
	CarLength float64
	Path      []float64
	Knots     []knot
	Ease      bool
}

// knot is where a train is at a time, in meters along its path.
type knot struct {
	Time  time.Time
	Along float64
}

func (m *trainMap) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	out := mapJSON{
		Tracks:   []trackJSON{},
		Stations: []stationJSON{},
		Trains:   []mapTrainJSON{},
	}
	if static, ok := m.lirrStatic.Get(); ok {
		trains, _ := m.lirrTrains.Get(now)
		m.lirrBoard(&out, static, trains)
	}
	if static, ok := m.subwayStatic.Get(); ok {
		var trips []transit.LiveTrip
		for _, f := range m.subwayTrips {
			if msg, ok := f.Get(now); ok {
				trips = append(trips, transit.LiveTrips(msg)...)
			}
		}
		m.arrivals.update(trips, now)
		m.subwayBoard(&out, static, trips, now)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// lirrBoard adds the LIRR's tracks, its stations and trains to out.
func (m *trainMap) lirrBoard(out *mapJSON, static *lirrStatic, trains []transit.TrainLocation) {
	shapes := sortedShapes(static.shapes)
	out.Tracks = append(out.Tracks, trackJSON{Kind: "lirr", Segments: segments(transit.Network(shapes, m.area))})
	stations := map[string]transit.Stop{}
	for id, s := range static.stops {
		if static.served[id] {
			stations[id] = s
		}
	}
	out.Stations = append(out.Stations, stationsOn(stations, shapes, m.area, "lirr", m.lirrStation)...)

	home := static.stops[m.lirrStation].Code
	codes := map[string]transit.Stop{}
	for _, s := range static.stops {
		codes[s.Code] = s
	}

	for _, t := range trains {
		if t.Canceled || t.Direction != m.lirrDirection || !m.area.Contains(t.LatLon) {
			continue
		}
		var next *transit.TrainLocationStop
		stopsHome := false
		for i, s := range t.Stops {
			if s.Departed || s.Canceled {
				continue
			}
			if next == nil {
				next = &t.Stops[i]
			}
			if s.Code == home {
				stopsHome = true
			}
		}
		if !stopsHome {
			continue
		}

		// The train's own shape, so that its cars follow the right
		// branch; or else whichever track is nearest.
		trip := static.trains[t.TrainNum]
		path, ok := static.shapes[trip.ShapeID]
		along, off := path.Locate(t.LatLon)
		if !ok || off > nearTrack {
			for _, p := range shapes {
				if a, o := p.Locate(t.LatLon); o < off {
					path, along, off = p, a, o
				}
			}
		}
		if off > nearTrack {
			continue
		}

		piece, start := path.Within(m.area, along)
		until := piece.Length()
		if next != nil {
			if a, o := piece.Locate(codes[next.Code].LatLon); o < nearTrack && a >= along-start {
				until = a
			}
		}
		// Go on at the speed the GPS says, until the next stop.
		knots := []knot{{t.Time, along - start}}
		if t.GPS && t.Speed > 0.5 && until > along-start {
			knots = append(knots, knot{t.Time.Add(seconds((until - along + start) / t.Speed)), until})
		}
		cars := t.Cars
		if cars == 0 {
			cars = lirrCars
		}
		out.Trains = append(out.Trains, mapTrainJSON{
			ID:        "lirr-" + t.TrainNum,
			Kind:      "lirr",
			Label:     t.TrainNum,
			colorJSON: routeColor(static.routes[trip.RouteID]),
			Cars:      cars,
			CarLength: lirrCarLength,
			Path:      flatten(piece.Points),
			Knots:     knots,
		})
	}
}

// subwayBoard adds the subway's tracks, its stations and trains to out.
func (m *trainMap) subwayBoard(out *mapJSON, static *subwayStatic, trips []transit.LiveTrip, now time.Time) {
	shapes := sortedShapes(static.shapes)
	out.Tracks = append(out.Tracks, trackJSON{Kind: "subway", Segments: segments(transit.Network(shapes, m.area))})
	// Platforms ("G08S") are at their stations ("G08"), which are the
	// stops without a direction.
	home := m.subwayPlatform[:len(m.subwayPlatform)-1]
	stations := map[string]transit.Stop{}
	for id, s := range static.stops {
		if !strings.HasSuffix(id, "N") && !strings.HasSuffix(id, "S") {
			stations[id] = s
		}
	}
	out.Stations = append(out.Stations, stationsOn(stations, shapes, m.area, "subway", home)...)
	var inArea []transit.LatLon
	for _, s := range stations {
		if m.area.Contains(s.LatLon) {
			inArea = append(inArea, s.LatLon)
		}
	}

	for _, t := range trips {
		started := !t.Reported.IsZero() && !t.Reported.After(now)
		if !slices.Contains(m.subwayRoutes, t.RouteID) || !started || !leavesFrom(t, m.subwayPlatform, now) {
			continue
		}
		// NYCT trip ids end with the shape: "089900_E..S69R".
		_, shapeID, _ := strings.Cut(t.TripID, "_")
		path, ok := static.shapes[shapeID]
		if !ok {
			continue
		}
		var stations []float64
		for _, s := range inArea {
			if a, off := path.Locate(s); off < nearTrack {
				stations = append(stations, a)
			}
		}
		var arr *arrival
		if a, ok := m.arrivals.get(t.TripID); ok {
			arr = &a
		}
		knots := subwayKnots(t, arr, path, static.stops, stations, now)
		along, ok := positionAt(knots, now)
		if !ok || !m.area.Contains(path.At(along)) {
			continue
		}
		piece, start := path.Within(m.area, along)
		for i := range knots {
			knots[i].Along -= start
		}
		out.Trains = append(out.Trains, mapTrainJSON{
			ID:        "subway-" + t.TripID,
			Kind:      "subway",
			Label:     t.RouteID,
			colorJSON: routeColor(static.routes[t.RouteID]),
			Cars:      subwayCars,
			CarLength: subwayCarLength,
			Path:      flatten(piece.Points),
			Knots:     knots,
			Ease:      true,
		})
	}
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

// arrivals remembers when each subway train got to the stop it is at. The
// feed only says which stop a train is at (or last passed), with the time
// of its latest report; the time the stop changes is when it arrived.
type arrivals struct {
	mu    sync.Mutex
	trips map[string]arrival // by trip id
}

type arrival struct {
	stop string
	time time.Time
	seen time.Time // last in the feed
}

// update notes the trains that have reached a new stop since the last
// update, and forgets those that have gone from the feed.
func (a *arrivals) update(trips []transit.LiveTrip, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.trips == nil {
		a.trips = map[string]arrival{}
	}
	for _, t := range trips {
		if t.StoppedAt == "" {
			continue
		}
		prev, ok := a.trips[t.TripID]
		if !ok || prev.stop != t.StoppedAt {
			prev = arrival{stop: t.StoppedAt, time: t.Reported}
			// Just after leaving, the feed still lists the stop with its
			// time.
			for _, s := range t.Stops {
				if s.StopID == t.StoppedAt && s.Time.Before(prev.time) {
					prev.time = s.Time
				}
			}
		}
		prev.seen = now
		a.trips[t.TripID] = prev
	}
	for id, arr := range a.trips {
		if now.Sub(arr.seen) > 10*time.Minute {
			delete(a.trips, id)
		}
	}
}

func (a *arrivals) get(tripID string) (arrival, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	arr, ok := a.trips[tripID]
	return arr, ok
}

// How subway trains are thought to move: they stand at a stop for
// subwayDwell, then run to the next one at subwaySpeed on average.
const (
	subwayDwell = 20 * time.Second
	subwaySpeed = 10.0 // m/s
)

// subwayKnots plans the way of a subway train from the stop it last
// reached, as remembered in arr, if any: it stands there, runs to its next stop on path,
// and waits there until the feed says it got there. So a train is never
// drawn more than a stop ahead of where it is known to be, and never moves
// backwards. Only at the start of its path, where trains wait for their
// time, does it leave in time for its predicted arrival at the next stop.
// With nothing known it comes from the station before its next stop, in
// time for the predicted arrival. stations are the stations on path, in
// meters along it.
//
// The predictions alone don't make for smooth motion: they are revised
// by up to a minute at every update, which moves trains back and forth by
// hundreds of meters.
func subwayKnots(t transit.LiveTrip, arr *arrival, path transit.Path, stops map[string]transit.Stop, stations []float64, now time.Time) []knot {
	type stop struct {
		knot
		left bool // the time is past
	}
	var on []stop
	for _, s := range t.Stops {
		if a, off := path.Locate(stops[s.StopID].LatLon); off < nearTrack {
			on = append(on, stop{knot{s.Time, a}, !s.Time.After(now)})
		}
	}

	var last *knot
	if arr != nil {
		if a, off := path.Locate(stops[arr.stop].LatLon); off < nearTrack {
			last = &knot{arr.time, a}
		}
	}
	// Without arr, the last stop the feed lists as left. With it, a stop
	// listed as left after arr may just be late.
	for _, s := range on {
		if arr == nil && s.left {
			last = &s.knot
		}
	}
	var next *knot
	for _, s := range on {
		if (last != nil && s.Along > last.Along+nearTrack) || (last == nil && !s.left) {
			next = &s.knot
			break
		}
	}
	run := func(from, to float64) time.Duration { return seconds((to - from) / subwaySpeed) }

	switch {
	case next == nil && last == nil:
		return nil
	case next == nil:
		return []knot{*last}
	case last == nil:
		prev := -1.0
		for _, a := range stations {
			if a < next.Along-nearTrack && a > prev {
				prev = a
			}
		}
		if prev < 0 {
			return []knot{*next}
		}
		return []knot{{next.Time.Add(-run(prev, next.Along)), prev}, *next}
	}
	leave := last.Time.Add(subwayDwell)
	if l := next.Time.Add(-run(last.Along, next.Along)); last.Along < nearTrack && l.After(leave) {
		leave = l
	}
	return []knot{*last, {leave, last.Along}, {leave.Add(run(last.Along, next.Along)), next.Along}}
}

// positionAt is where a subway train following knots is at t, in meters,
// the same way as the frontend has it.
func positionAt(knots []knot, t time.Time) (float64, bool) {
	if len(knots) == 0 {
		return 0, false
	}
	for i := 1; i < len(knots); i++ {
		a, b := knots[i-1], knots[i]
		if t.Before(b.Time) {
			if t.Before(a.Time) {
				return a.Along, true
			}
			f := float64(t.Sub(a.Time)) / float64(b.Time.Sub(a.Time))
			return a.Along + (b.Along-a.Along)*f*f*(3-2*f), true
		}
	}
	return knots[len(knots)-1].Along, true
}

func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

// stationsOn lists the stops in area that are on any of paths.
func stationsOn(stops map[string]transit.Stop, paths []transit.Path, area transit.BBox, kind, home string) []stationJSON {
	var out []stationJSON
	for id, s := range stops {
		if !area.Contains(s.LatLon) {
			continue
		}
		for _, p := range paths {
			if _, off := p.Locate(s.LatLon); off < nearTrack {
				out = append(out, stationJSON{Name: s.Name, Kind: kind, Lon: s.Lon, Lat: s.Lat, Home: id == home})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
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
