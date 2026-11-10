package transit

import (
	"encoding/json"
	"net/http"
	"time"
)

// TrainLocation is where an LIRR train is, from the backend of the MTA's
// TrainTime app. Unlike the GTFS-Realtime vehicle positions, which jump
// from station to station, these are GPS fixes every few seconds, and they
// come with the train's cars.
//
// The API is not documented and might change without notice.
type TrainLocation struct {
	TrainID   string // "LIRR_2026-11-08_6267", for NewTrainLocationFeed
	TrainNum  string // the GTFS trip_short_name
	Direction string // "E" or "W"
	Canceled  bool
	Stops     []TrainLocationStop
	Cars      int

	LatLon
	Time    time.Time // of the fix
	Speed   float64   // m/s
	Heading float64   // degrees clockwise from north
	GPS     bool      // false if the position is the dispatcher's
}

type TrainLocationStop struct {
	Code      string // "FHL"
	Departed  bool
	Canceled  bool
	Scheduled time.Time
}

// LocationFeed is the list of LIRR trains running now.
type LocationFeed = Live[[]TrainLocation]

// TrainLocationFeed is the location of one train, which is much smaller
// than the whole list.
type TrainLocationFeed = Live[TrainLocation]

var trainTimeHeader = http.Header{"Accept-Version": {"3.0"}}

// NewLocationFeed returns a feed of the trains at url, such as
// https://backend-unified.mylirr.org/locations?geometry=NONE&railroad=LIRR
func NewLocationFeed(url string) *LocationFeed {
	return &LocationFeed{url: url, parse: parseLocations, header: trainTimeHeader}
}

// NewTrainLocationFeed returns a feed of the train at url, such as
// https://backend-unified.mylirr.org/locations/LIRR_2026-11-08_6267?geometry=NONE
func NewTrainLocationFeed(url string) *TrainLocationFeed {
	return &TrainLocationFeed{url: url, parse: parseLocation, header: trainTimeHeader}
}

func parseLocation(b []byte) (TrainLocation, error) {
	trains, err := parseLocations(append(append([]byte("["), b...), ']'))
	if err != nil || len(trains) != 1 {
		return TrainLocation{}, err
	}
	return trains[0], nil
}

func parseLocations(b []byte) ([]TrainLocation, error) {
	var trains []struct {
		TrainID  string `json:"train_id"`
		TrainNum string `json:"train_num"`
		Details  struct {
			Direction string
			Stops     []struct {
				Code        string
				SchedTime   int64  `json:"sched_time"`
				StopStatus  string `json:"stop_status"`
				LocalCancel bool   `json:"local_cancel"`
			}
		}
		Consist struct {
			Cars []struct {
				Revenue bool
			}
		}
		Location struct {
			Latitude, Longitude float64
			Speed, Heading      float64
			Source              string
			Timestamp           int64
		}
		Status struct {
			Canceled bool
		}
	}
	if err := json.Unmarshal(b, &trains); err != nil {
		return nil, err
	}

	const mph = 0.44704
	var out []TrainLocation
	for _, t := range trains {
		l := TrainLocation{
			TrainID:   t.TrainID,
			TrainNum:  t.TrainNum,
			Direction: t.Details.Direction,
			Canceled:  t.Status.Canceled,
			Cars:      len(t.Consist.Cars),
			LatLon:    LatLon{t.Location.Latitude, t.Location.Longitude},
			Time:      time.Unix(t.Location.Timestamp, 0),
			Speed:     t.Location.Speed * mph,
			Heading:   t.Location.Heading,
			GPS:       t.Location.Source == "GPS",
		}
		for _, s := range t.Details.Stops {
			l.Stops = append(l.Stops, TrainLocationStop{
				Code:      s.Code,
				Departed:  s.StopStatus == "DEPARTED",
				Canceled:  s.LocalCancel,
				Scheduled: time.Unix(s.SchedTime, 0),
			})
		}
		out = append(out, l)
	}
	return out, nil
}
