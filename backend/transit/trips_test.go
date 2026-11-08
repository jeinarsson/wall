package transit

import (
	"testing"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

func tripUpdate(id string, rel gtfs.TripDescriptor_ScheduleRelationship, stops ...*gtfs.TripUpdate_StopTimeUpdate) *gtfs.FeedEntity {
	return &gtfs.FeedEntity{
		Id: proto.String(id),
		TripUpdate: &gtfs.TripUpdate{
			Trip:           &gtfs.TripDescriptor{TripId: proto.String(id), RouteId: proto.String("F"), ScheduleRelationship: rel.Enum()},
			StopTimeUpdate: stops,
		},
	}
}

func stopAt(stop string, t, delay int64) *gtfs.TripUpdate_StopTimeUpdate {
	return &gtfs.TripUpdate_StopTimeUpdate{
		StopId:    proto.String(stop),
		Departure: &gtfs.TripUpdate_StopTimeEvent{Time: proto.Int64(t), Delay: proto.Int32(int32(delay))},
	}
}

func TestStopTimes(t *testing.T) {
	skipped := stopAt("G08S", 0, 0)
	skipped.Departure = nil
	skipped.ScheduleRelationship = gtfs.TripUpdate_StopTimeUpdate_SKIPPED.Enum()
	arrivalOnly := stopAt("G08S", 0, 0)
	arrivalOnly.Departure, arrivalOnly.Arrival = nil, &gtfs.TripUpdate_StopTimeEvent{Time: proto.Int64(300)}

	msg := &gtfs.FeedMessage{Entity: []*gtfs.FeedEntity{
		tripUpdate("a", gtfs.TripDescriptor_SCHEDULED, stopAt("G07S", 50, 0), stopAt("G08S", 100, 60), stopAt("G09S", 150, 0)),
		tripUpdate("b", gtfs.TripDescriptor_SCHEDULED, stopAt("G09S", 150, 0)),
		tripUpdate("c", gtfs.TripDescriptor_SCHEDULED, skipped, stopAt("G09S", 150, 0)),
		tripUpdate("d", gtfs.TripDescriptor_CANCELED),
		tripUpdate("e", gtfs.TripDescriptor_SCHEDULED, arrivalOnly),
	}}

	sts := StopTimes(msg, "G08S")
	if len(sts) != 3 {
		t.Fatalf("got %d stop times, want 3: %+v", len(sts), sts)
	}
	a := sts[0]
	if a.TripID != "a" || a.RouteID != "F" || a.Time != time.Unix(100, 0) || a.Delay != time.Minute || a.Canceled || a.LastStopID != "G09S" {
		t.Errorf("got %+v", a)
	}
	if a.Scheduled() != time.Unix(40, 0) {
		t.Errorf("got scheduled %v", a.Scheduled())
	}
	if c := sts[1]; c.TripID != "c" || !c.Canceled {
		t.Errorf("got %+v, want c canceled", c)
	}
	if e := sts[2]; e.TripID != "e" || e.Time != time.Unix(300, 0) {
		t.Errorf("got %+v, want e at 300", e)
	}

	if got := CanceledTrips(msg); len(got) != 1 || !got["d"] {
		t.Errorf("CanceledTrips = %v, want d", got)
	}
}
