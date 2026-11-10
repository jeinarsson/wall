package main

import (
	"math"
	"testing"

	"github.com/jeinarsson/wall/transit"
)

func TestViewBounds(t *testing.T) {
	// 2 km by 1 km on the equator, turned a quarter: 1 km by 2 km.
	v := mapView{Width: 2000, Height: 1000, Rotate: 90}
	b := v.bounds(0)
	const km = 1000 / 111_320.0
	if math.Abs(b.East-b.West-km) > 1e-9 || math.Abs(b.North-b.South-2*km) > 1e-9 {
		t.Errorf("got %+v, want 1 km wide and 2 km high", b)
	}
}

func TestTripShape(t *testing.T) {
	static := &subwayStatic{
		shapes: map[string]transit.Path{"F..S69R": {}, "F..S54R": {}, "F..N69R": {}},
		stations: map[string]map[string]float64{
			"F..S69R": {"F01": 0, "F05": 3000, "G08": 6000},
			"F..S54R": {"F05": 0, "G08": 3000},
			"F..N69R": {"F01": 6000, "F05": 3000, "G08": 0},
		},
	}
	trip := func(id string, stops ...string) transit.LiveTrip {
		t := transit.LiveTrip{TripID: id}
		for _, s := range stops {
			t.Stops = append(t.Stops, transit.LiveStop{StopID: s})
		}
		return t
	}
	for _, tc := range []struct {
		trip transit.LiveTrip
		want string
	}{
		{trip("089900_F..S54R", "F01S"), "F..S54R"},      // named
		{trip("121550_F..S", "F01S", "F05S"), "F..S69R"}, // the one with most stops
		{trip("121550_F..S", "F05S", "G08S"), "F..S54R"}, // a tie: the first by name
		{trip("121550_F..N", "F05N", "F01N"), "F..N69R"}, // the direction
	} {
		if got, ok := tripShape(static, tc.trip); !ok || got != tc.want {
			t.Errorf("%s %v: got %q, want %q", tc.trip.TripID, tc.trip.Stops, got, tc.want)
		}
	}
	if _, ok := tripShape(static, trip("1_E..S", "F01S")); ok {
		t.Errorf("found a shape for another route")
	}
}
