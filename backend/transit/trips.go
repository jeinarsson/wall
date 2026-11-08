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
