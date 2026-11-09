package transit

import (
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
)

// StopTime is a trip's predicted departure from a stop, from a realtime
// feed.
type StopTime struct {
	TripID      string
	RouteID     string
	DirectionID *uint32 // not set by the NYC subway
	Time        time.Time
	Delay       time.Duration // zero if the feed doesn't say
	// Canceled is set if the trip is canceled or skips the stop. Time
	// may then be zero.
	Canceled   bool
	LastStopID string // where the trip ends
}

// Scheduled is the nominal departure time.
func (st StopTime) Scheduled() time.Time {
	return st.Time.Add(-st.Delay)
}

// StopTimes lists the trips in msg that stop at stopID, in feed order.
//
// Feeds keep trips for a while after they have left, so callers should
// drop those whose Time has passed.
func StopTimes(msg *gtfs.FeedMessage, stopID string) []StopTime {
	var sts []StopTime
	for _, e := range msg.Entity {
		tu := e.TripUpdate
		if tu == nil || tu.Trip == nil {
			continue
		}
		for _, stu := range tu.StopTimeUpdate {
			if stu.GetStopId() != stopID {
				continue
			}
			st := StopTime{
				TripID:      tu.Trip.GetTripId(),
				RouteID:     tu.Trip.GetRouteId(),
				DirectionID: tu.Trip.DirectionId,
				Canceled: tu.Trip.GetScheduleRelationship() == gtfs.TripDescriptor_CANCELED ||
					stu.GetScheduleRelationship() == gtfs.TripUpdate_StopTimeUpdate_SKIPPED,
			}
			ev := stu.Departure
			if ev.GetTime() == 0 {
				ev = stu.Arrival
			}
			if ev.GetTime() != 0 {
				st.Time = time.Unix(ev.GetTime(), 0)
				st.Delay = time.Duration(ev.GetDelay()) * time.Second
			} else if !st.Canceled {
				break
			}
			st.LastStopID = tu.StopTimeUpdate[len(tu.StopTimeUpdate)-1].GetStopId()
			sts = append(sts, st)
			break
		}
	}
	return sts
}

// CanceledTrips returns the ids of the trips in msg that are canceled
// altogether. Feeds may list these without any stops.
func CanceledTrips(msg *gtfs.FeedMessage) map[string]bool {
	canceled := map[string]bool{}
	for _, e := range msg.Entity {
		if tu := e.TripUpdate; tu != nil && tu.Trip.GetScheduleRelationship() == gtfs.TripDescriptor_CANCELED {
			canceled[tu.Trip.GetTripId()] = true
		}
	}
	return canceled
}

// LiveTrip is a trip in a realtime feed with its predicted stops.
type LiveTrip struct {
	TripID  string
	RouteID string
	// Stops are the stops still ahead, in order. Feeds also keep the last
	// stop or two the trip has left for a while, with times in the past.
	Stops []LiveStop
	// Reported is when the train last reported its position, or zero if
	// it hasn't. The NYC subway also lists trips that haven't started yet,
	// with Reported in the future.
	Reported time.Time
	// StoppedAt is the stop where the train is standing, if any.
	StoppedAt string
}

type LiveStop struct {
	StopID string
	Time   time.Time // departure, or arrival at the last stop
}

// LiveTrips lists the trips in msg that aren't canceled.
func LiveTrips(msg *gtfs.FeedMessage) []LiveTrip {
	var trips []LiveTrip
	index := map[string]int{}
	for _, e := range msg.Entity {
		tu := e.TripUpdate
		if tu == nil || tu.Trip == nil || tu.Trip.GetScheduleRelationship() == gtfs.TripDescriptor_CANCELED {
			continue
		}
		t := LiveTrip{TripID: tu.Trip.GetTripId(), RouteID: tu.Trip.GetRouteId()}
		for _, stu := range tu.StopTimeUpdate {
			if stu.GetScheduleRelationship() == gtfs.TripUpdate_StopTimeUpdate_SKIPPED {
				continue
			}
			ev := stu.Departure
			if ev.GetTime() == 0 {
				ev = stu.Arrival
			}
			if ev.GetTime() != 0 {
				t.Stops = append(t.Stops, LiveStop{stu.GetStopId(), time.Unix(ev.GetTime(), 0)})
			}
		}
		index[t.TripID] = len(trips)
		trips = append(trips, t)
	}
	for _, e := range msg.Entity {
		v := e.Vehicle
		if v == nil || v.Trip == nil {
			continue
		}
		i, ok := index[v.Trip.GetTripId()]
		if !ok {
			continue
		}
		trips[i].Reported = time.Unix(int64(v.GetTimestamp()), 0)
		if v.GetCurrentStatus() == gtfs.VehiclePosition_STOPPED_AT {
			trips[i].StoppedAt = v.GetStopId()
		}
	}
	return trips
}
