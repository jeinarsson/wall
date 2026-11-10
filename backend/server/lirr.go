package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jeinarsson/wall/transit"
)

// lirrSign is a timetable for one LIRR station and direction: the
// scheduled trains with how late they are, cancellations, whether they are
// at the station, and service alerts for the station.
//
// The trains come from the station's board in the TrainTime app, which
// lists them until they leave. Without it, they come from the timetable
// and the GTFS-Realtime feed, which only have predictions.
type lirrSign struct {
	stop        string // GTFS stop id: "55"
	direction   string // "E" or "W", in the TrainTime app
	directionID uint32 // the same in GTFS: 0 is eastbound, 1 westbound
	ahead       time.Duration
	alerts      transit.AlertFilter
	// cancelAlertType is the alert type naming canceled trains.
	cancelAlertType string

	arrivals  *transit.ArrivalFeed
	trips     *transit.TripFeed
	alertFeed *transit.AlertFeed
	static    *transit.File[*lirrStatic]
}

// lirrStatus is what the sign says about a train, by its status on the
// TrainTime board. Trains on their way aren't marked.
var lirrStatus = map[string]string{
	"EN_ROUTE": "",
	"ARRIVING": "Arriving",
	"BERTHED":  "At station",
}

// lirrStatic is what the sign and the map need from the LIRR static GTFS
// feed.
type lirrStatic struct {
	routes    map[string]transit.Route
	stops     map[string]transit.Stop
	timetable *transit.Timetable
	shapes    map[string]transit.Path
	// trains has a trip for each train number, for its route and shape.
	trains map[string]transit.Trip
	// stations has, for each shape, the stations on it (stops where
	// passengers get on or off) by how far along it they are.
	stations map[string]map[string]float64
}

// loadLIRRStatic returns a loader for the static data of a sign at stop,
// whose times are in loc.
func loadLIRRStatic(stop string, loc *time.Location) func(fs.FS) (*lirrStatic, error) {
	return func(gtfs fs.FS) (*lirrStatic, error) {
		routes, err := transit.LoadRoutes(gtfs)
		if err != nil {
			return nil, err
		}
		stops, err := transit.LoadStops(gtfs)
		if err != nil {
			return nil, err
		}
		tt, err := transit.LoadTimetable(gtfs, stop, loc)
		if err != nil {
			return nil, err
		}
		shapes, err := transit.LoadShapes(gtfs, func(string) bool { return true })
		if err != nil {
			return nil, err
		}
		trips, err := transit.LoadTrips(gtfs)
		if err != nil {
			return nil, err
		}
		trains := map[string]transit.Trip{}
		for _, t := range trips {
			trains[t.ShortName] = t
		}
		served, err := transit.LoadServedStops(gtfs)
		if err != nil {
			return nil, err
		}
		stations := map[string]transit.Stop{}
		for id, s := range stops {
			if served[id] {
				stations[id] = s
			}
		}
		along := map[string]map[string]float64{}
		for id, p := range shapes {
			along[id] = transit.StopsAlong(p, stations, nearTrack)
		}
		return &lirrStatic{routes: routes, stops: stops, timetable: tt, shapes: shapes, trains: trains, stations: along}, nil
	}
}

type lirrJSON struct {
	Departures []lirrDepartureJSON
	Alerts     []alertJSON
}

type lirrDepartureJSON struct {
	Train       string // train number, if known
	Destination string
	Color       string
	Scheduled   time.Time
	Expected    *time.Time // nil without a live prediction
	Canceled    bool
	// Status is "Arriving", "At station" or "Held", or empty.
	Status string
}

func (s *lirrSign) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	board := lirrJSON{Departures: []lirrDepartureJSON{}, Alerts: []alertJSON{}}

	if static, ok := s.static.Get(); ok {
		alerts, _ := s.alertFeed.Get(now)
		if arrivals, ok := s.arrivals.Get(now); ok {
			board = s.trainTimeBoard(static, arrivals, alerts, now)
		} else {
			// Delayed trains can leave well after their scheduled time.
			scheduled := static.timetable.Departures(now.Add(-time.Hour), now.Add(s.ahead))
			var live []transit.StopTime
			canceled := map[string]bool{}
			if msg, ok := s.trips.Get(now); ok {
				live = transit.StopTimes(msg, s.stop)
				canceled = transit.CanceledTrips(msg)
			}
			for id := range transit.TripsIn(alerts, s.cancelAlertType, now) {
				canceled[id] = true
			}
			board = s.board(static, scheduled, live, canceled, alerts, now)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(board)
}

// trainTimeBoard lists the trains on the TrainTime board in s.direction,
// with the alerts matching s.alerts.
func (s *lirrSign) trainTimeBoard(static *lirrStatic, arrivals []transit.TrainArrival, alerts []transit.Alert, now time.Time) lirrJSON {
	board := lirrJSON{Departures: []lirrDepartureJSON{}, Alerts: matchingAlerts(alerts, s.alerts, now)}
	byCode := map[string]transit.Stop{}
	for _, st := range static.stops {
		byCode[st.Code] = st
	}
	for _, a := range arrivals {
		if a.Direction != s.direction {
			continue
		}
		trip := static.trains[a.TrainNum]
		d := lirrDepartureJSON{
			Train:       a.TrainNum,
			Destination: trip.Headsign,
			Color:       routeColor(static.routes[trip.RouteID]).Color,
			Scheduled:   a.Scheduled,
			Canceled:    a.Canceled,
		}
		if n := len(a.Stops); n > 0 {
			if st, ok := byCode[a.Stops[n-1]]; ok {
				d.Destination = st.Name
			}
		}
		if a.Late != nil {
			expected := a.Scheduled.Add(*a.Late)
			d.Expected = &expected
		}
		status, ok := lirrStatus[a.Status]
		if !ok && a.Status != "" {
			// Any others as they are: "SOME_STATUS" is "Some status".
			status = strings.ToUpper(a.Status[:1]) + strings.ToLower(strings.ReplaceAll(a.Status[1:], "_", " "))
		}
		if status == "" && a.Held {
			status = "Held"
		}
		d.Status = status
		board.Departures = append(board.Departures, d)
	}
	sort.SliceStable(board.Departures, func(i, j int) bool { return board.Departures[i].Scheduled.Before(board.Departures[j].Scheduled) })
	return board
}

// board merges the timetable with live predictions and cancellations,
// and lists the trains that haven't left yet with the alerts matching
// s.alerts.
//
// A trip in canceled counts as canceled only if there is no live
// prediction for it, as the LIRR also cancels trips in part.
func (s *lirrSign) board(static *lirrStatic, scheduled []transit.ScheduledStop, live []transit.StopTime, canceled map[string]bool, alerts []transit.Alert, now time.Time) lirrJSON {
	board := lirrJSON{Departures: []lirrDepartureJSON{}, Alerts: matchingAlerts(alerts, s.alerts, now)}

	predicted := map[string]transit.StopTime{}
	for _, st := range live {
		predicted[st.TripID] = st
	}

	for _, sch := range scheduled {
		if sch.Trip.DirectionID != s.directionID {
			continue
		}
		d := lirrDepartureJSON{
			Train:       sch.Trip.ShortName,
			Destination: sch.Trip.Headsign,
			Color:       routeColor(static.routes[sch.Trip.RouteID]).Color,
			Scheduled:   sch.Time,
		}
		if st, ok := predicted[sch.Trip.ID]; ok {
			delete(predicted, sch.Trip.ID)
			if st.Canceled {
				d.Canceled = true
			} else {
				d.Expected = &st.Time
			}
		} else if canceled[sch.Trip.ID] {
			d.Canceled = true
		}
		board.Departures = append(board.Departures, d)
	}

	// Trains running that aren't in the timetable, such as extras.
	for _, st := range predicted {
		if st.Canceled || st.DirectionID == nil || *st.DirectionID != s.directionID {
			continue
		}
		board.Departures = append(board.Departures, lirrDepartureJSON{
			Destination: static.stops[st.LastStopID].Name,
			Color:       routeColor(static.routes[st.RouteID]).Color,
			Scheduled:   st.Scheduled(),
			Expected:    &st.Time,
		})
	}

	upcoming := board.Departures[:0]
	for _, d := range board.Departures {
		leaves := d.Scheduled
		if d.Expected != nil {
			leaves = *d.Expected
		}
		if leaves.After(now) {
			upcoming = append(upcoming, d)
		}
	}
	sort.SliceStable(upcoming, func(i, j int) bool { return upcoming[i].Scheduled.Before(upcoming[j].Scheduled) })
	board.Departures = upcoming
	return board
}
