package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jeinarsson/wall/transit"
)

// subwaySign is a countdown sign for one subway platform, like the ones at
// station entrances: the next few trains on each route, plus any service
// alerts that affect the ride.
type subwaySign struct {
	platform string   // GTFS stop id, including direction: "G08S"
	routes   []string // in display order
	alerts   transit.AlertFilter
	// alertBoroughs adds every station in these boroughs (as in
	// Stations.csv: "M", "Bk", ...) to alerts.Stops.
	alertBoroughs []string

	trips     []*transit.TripFeed
	alertFeed *transit.AlertFeed
	static    *transit.File[*subwayStatic]
	stations  *transit.File[map[string]transit.Station]
}

// subwayStatic is what the sign and the map need from the subway's static
// GTFS feed.
type subwayStatic struct {
	routes map[string]transit.Route
	stops  map[string]transit.Stop
	shapes map[string]transit.Path // of the routes on the map
	// stations has, for each shape, the stations on it by how far along
	// it they are. Stations are the stops without a direction ("G08");
	// their platforms ("G08S") are in the same place.
	stations map[string]map[string]float64
}

// loadSubwayStatic returns a loader for the static data, with the shapes
// of routes. Subway shape ids start with the route: "E..S69R".
func loadSubwayStatic(routes []string) func(fs.FS) (*subwayStatic, error) {
	return func(gtfs fs.FS) (*subwayStatic, error) {
		r, err := transit.LoadRoutes(gtfs)
		if err != nil {
			return nil, err
		}
		stops, err := transit.LoadStops(gtfs)
		if err != nil {
			return nil, err
		}
		shapes, err := transit.LoadShapes(gtfs, func(id string) bool {
			route, _, _ := strings.Cut(id, ".")
			return slices.Contains(routes, route)
		})
		if err != nil {
			return nil, err
		}
		stations := map[string]transit.Stop{}
		for id, s := range stops {
			if !strings.HasSuffix(id, "N") && !strings.HasSuffix(id, "S") {
				stations[id] = s
			}
		}
		along := map[string]map[string]float64{}
		for id, p := range shapes {
			along[id] = transit.StopsAlong(p, stations, nearTrack)
		}
		return &subwayStatic{routes: r, stops: stops, shapes: shapes, stations: along}, nil
	}
}

type subwayJSON struct {
	Routes []subwayRouteJSON
	Alerts []alertJSON
	// Colors has all subway routes, for the "[E]"s in alert texts.
	Colors map[string]colorJSON
}

type subwayRouteJSON struct {
	Route string
	colorJSON
	Times []time.Time
}

type colorJSON struct {
	Color     string
	TextColor string
}

type alertJSON struct {
	Text   string
	Severe bool
}

func (s *subwaySign) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	var stopTimes []transit.StopTime
	for _, f := range s.trips {
		if msg, ok := f.Get(now); ok {
			stopTimes = append(stopTimes, transit.StopTimes(msg, s.platform)...)
		}
	}
	var routes map[string]transit.Route
	if static, ok := s.static.Get(); ok {
		routes = static.routes
	}
	alerts, _ := s.alertFeed.Get(now)
	filter := s.alerts
	if stations, ok := s.stations.Get(); ok {
		filter.Stops = append(slices.Clip(filter.Stops), stationsIn(stations, s.alertBoroughs)...)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.board(stopTimes, routes, alerts, filter, now))
}

// board lists the trains on s.routes in stopTimes that haven't left, and
// the alerts that match filter. Trains at the platform have times in the
// past.
func (s *subwaySign) board(stopTimes []transit.StopTime, routes map[string]transit.Route, alerts []transit.Alert, filter transit.AlertFilter, now time.Time) subwayJSON {
	board := subwayJSON{Routes: []subwayRouteJSON{}, Alerts: []alertJSON{}, Colors: map[string]colorJSON{}}
	for id, r := range routes {
		board.Colors[id] = routeColor(r)
	}
	for _, id := range s.routes {
		row := subwayRouteJSON{Route: id, colorJSON: routeColor(routes[id]), Times: []time.Time{}}
		for _, st := range stopTimes {
			if st.RouteID == id && !st.Canceled && !subwayLeft(st.Time, now) {
				row.Times = append(row.Times, st.Time)
			}
		}
		// The feed sometimes lists a train twice, as two trips.
		slices.SortFunc(row.Times, time.Time.Compare)
		row.Times = slices.CompactFunc(row.Times, time.Time.Equal)
		board.Routes = append(board.Routes, row)
	}
	board.Alerts = matchingAlerts(alerts, filter, now)
	return board
}

// subwayHeld is how long a train can stand at a platform, past the time
// in the feed, and still be there.
const subwayHeld = 2 * time.Minute

// subwayLeft reports whether a train that the feed still lists at a
// platform at t has left. While a train stands at a platform, the time
// there is that of its latest report, a few seconds ago; the stop drops
// off the trip some 30-45 s after the last report, about when the train
// leaves. Times long past are trips the feed has lost track of.
func subwayLeft(t, now time.Time) bool {
	return t.Before(now.Add(-subwayHeld))
}

// stationsIn returns the ids of the stations in any of boroughs.
func stationsIn(stations map[string]transit.Station, boroughs []string) []string {
	var ids []string
	for id, st := range stations {
		if slices.Contains(boroughs, st.Borough) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func routeColor(r transit.Route) colorJSON {
	c := colorJSON{Color: "#808080", TextColor: "#FFFFFF"}
	if r.Color != "" {
		c.Color = "#" + r.Color
	}
	if r.TextColor != "" {
		c.TextColor = "#" + r.TextColor
	}
	return c
}

// matchingAlerts returns the alerts that match filter, most severe first.
func matchingAlerts(alerts []transit.Alert, filter transit.AlertFilter, now time.Time) []alertJSON {
	var matched []transit.Alert
	for _, a := range alerts {
		if filter.Match(a, now) {
			matched = append(matched, a)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Severity > matched[j].Severity })
	out := []alertJSON{}
	for _, a := range matched {
		out = append(out, alertJSON{Text: a.Text, Severe: severeAlerts.MatchString(a.Type)})
	}
	return out
}
