package transit

import (
	"slices"
	"sort"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
)

// Filter selects departures from one stop.
//
// For the NYC subway the direction is part of the stop id ("G08S"), and
// direction_id is not set in the feed. The LIRR uses plain stop ids and sets
// direction_id (1 is westbound, towards the city).
type Filter struct {
	StopID      string
	Routes      []string // route ids; empty means all
	DirectionID *uint32  // nil means any
}

type Departure struct {
	Route       Route
	Destination string    // name of the trip's last stop
	Time        time.Time // expected departure
	Delay       time.Duration
}

// Scheduled is the nominal departure time.
func (d Departure) Scheduled() time.Time {
	return d.Time.Add(-d.Delay)
}

// Departures lists upcoming departures matching filter across feeds,
// ordered by expected time.
func Departures(feeds []*Feed, filter Filter, now time.Time) []Departure {
	var deps []Departure
	for _, f := range feeds {
		// Without the static data there are no names or colors to show.
		msg, static := f.current(now), f.static.current()
		if msg == nil || static == nil {
			continue
		}
		for _, e := range msg.Entity {
			if d, ok := departure(static, e.TripUpdate, filter, now); ok {
				deps = append(deps, d)
			}
		}
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].Time.Before(deps[j].Time) })
	return deps
}

func departure(static *Static, tu *gtfs.TripUpdate, filter Filter, now time.Time) (Departure, bool) {
	if tu == nil || tu.Trip == nil || len(tu.StopTimeUpdate) == 0 {
		return Departure{}, false
	}
	trip := tu.Trip
	if trip.GetScheduleRelationship() == gtfs.TripDescriptor_CANCELED {
		return Departure{}, false
	}
	if len(filter.Routes) > 0 && !slices.Contains(filter.Routes, trip.GetRouteId()) {
		return Departure{}, false
	}
	if filter.DirectionID != nil && (trip.DirectionId == nil || *trip.DirectionId != *filter.DirectionID) {
		return Departure{}, false
	}

	for _, st := range tu.StopTimeUpdate {
		if st.GetStopId() != filter.StopID {
			continue
		}
		if st.GetScheduleRelationship() == gtfs.TripUpdate_StopTimeUpdate_SKIPPED {
			return Departure{}, false
		}
		ev := st.Departure
		if ev.GetTime() == 0 {
			ev = st.Arrival
		}
		if ev.GetTime() == 0 {
			return Departure{}, false
		}
		// Feeds keep trips for a while after they have left; drop those.
		t := time.Unix(ev.GetTime(), 0)
		if t.Before(now) {
			return Departure{}, false
		}
		last := tu.StopTimeUpdate[len(tu.StopTimeUpdate)-1].GetStopId()
		return Departure{
			Route:       static.Route(trip.GetRouteId()),
			Destination: static.StopName(last),
			Time:        t,
			Delay:       time.Duration(ev.GetDelay()) * time.Second,
		}, true
	}
	return Departure{}, false
}
