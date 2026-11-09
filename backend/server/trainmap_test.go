package main

import (
	"testing"
	"time"

	"github.com/jeinarsson/wall/transit"
)

func sec(s int) time.Time { return now.Add(time.Duration(s) * time.Second) }

func TestSubwayKnots(t *testing.T) {
	// Stations a kilometer apart along a straight track, A to D. A run
	// between them takes 100 s.
	east := func(m float64) transit.LatLon { return transit.LatLon{Lon: m / 111_320} }
	path := transit.NewPath([]transit.LatLon{east(0), east(4000)})
	stops := map[string]transit.Stop{
		"A": {LatLon: east(0)},
		"B": {LatLon: east(1000)},
		"C": {LatLon: east(2000)},
		"D": {LatLon: east(3000)},
		"X": {LatLon: transit.LatLon{Lat: 1, Lon: 1}}, // not on the track
	}
	stations := []float64{0, 1000, 2000, 3000}
	stop := func(id string, s int) transit.LiveStop { return transit.LiveStop{StopID: id, Time: sec(s)} }
	trip := func(ss ...transit.LiveStop) transit.LiveTrip { return transit.LiveTrip{Stops: ss} }
	at := func(id string, s int) *arrival { return &arrival{stop: id, time: sec(s)} }

	for _, tc := range []struct {
		name string
		trip transit.LiveTrip
		arr  *arrival
		want []knot
	}{
		{
			"from the stop reached, standing there, then waiting at the next",
			trip(stop("X", 30), stop("C", 60), stop("D", 180)), at("B", -10),
			[]knot{{sec(-10), 1000}, {sec(10), 1000}, {sec(110), 2000}},
		},
		{
			"on to the next stop even if it is late for it",
			trip(stop("C", -5), stop("D", 90)), at("B", -120),
			[]knot{{sec(-120), 1000}, {sec(-100), 1000}, {sec(0), 2000}},
		},
		{
			"leaving the start of the track in time for the next stop",
			trip(stop("B", 300)), at("A", -60),
			[]knot{{sec(-60), 0}, {sec(200), 0}, {sec(300), 1000}},
		},
		{
			"without an arrival, from the last stop the feed lists as left",
			trip(stop("B", -30), stop("C", -10), stop("D", 90)), nil,
			[]knot{{sec(-10), 2000}, {sec(10), 2000}, {sec(110), 3000}},
		},
		{
			"with nothing known, from the station before, as predicted",
			trip(stop("C", 60), stop("D", 180)), nil,
			[]knot{{sec(-40), 1000}, {sec(60), 2000}},
		},
		{
			"at its last stop",
			trip(stop("D", -10)), at("D", -10),
			[]knot{{sec(-10), 3000}},
		},
	} {
		got := subwayKnots(tc.trip, tc.arr, path, stops, stations, now)
		ok := len(got) == len(tc.want)
		for i := 0; ok && i < len(got); i++ {
			ok = got[i].Time.Sub(tc.want[i].Time).Abs() < time.Millisecond && near(got[i].Along, tc.want[i].Along)
		}
		if !ok {
			t.Errorf("%s:\n got %v\nwant %v", tc.name, got, tc.want)
		}
	}
}

func TestArrivals(t *testing.T) {
	var a arrivals
	trip := func(stopped string, reported int, ss ...transit.LiveStop) []transit.LiveTrip {
		return []transit.LiveTrip{{TripID: "t", StoppedAt: stopped, Reported: sec(reported), Stops: ss}}
	}
	check := func(when string, stop string, s int) {
		t.Helper()
		arr, ok := a.get("t")
		if !ok || arr.stop != stop || !arr.time.Equal(sec(s)) {
			t.Errorf("%s: got %+v, want %s at %+ds", when, arr, stop, s)
		}
	}

	a.update(trip("B", 0), sec(5))
	check("first seen", "B", 0)
	a.update(trip("B", 17), sec(22))
	check("reported again", "B", 0)
	a.update(trip("C", 34, transit.LiveStop{StopID: "C", Time: sec(28)}), sec(39))
	check("at the next stop, which the feed lists", "C", 28)

	a.update(nil, sec(39+11*60))
	if _, ok := a.get("t"); ok {
		t.Errorf("still remembered after leaving the feed")
	}
}

func TestPositionAt(t *testing.T) {
	knots := []knot{{at(0), 0}, {at(1), 1000}, {at(2), 1000}}
	for _, tc := range []struct {
		at   time.Time
		want float64
	}{
		{at(-1), 0},
		{sec(15), 156.25}, // eased: slow off the stop
		{sec(30), 500},
		{sec(90), 1000},
		{at(5), 1000},
	} {
		if got, ok := positionAt(knots, tc.at); !ok || !near(got, tc.want) {
			t.Errorf("at %v: got %.2f, want %.2f", tc.at.Sub(now), got, tc.want)
		}
	}
	if _, ok := positionAt(nil, now); ok {
		t.Errorf("placed a train without knots")
	}
}

func near(a, b float64) bool { return a-b < 0.5 && b-a < 0.5 }
