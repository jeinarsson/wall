package transit

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)

func sec(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }

func sameKnots(a, b []Knot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Time.Sub(b[i].Time).Abs() > time.Millisecond || !near(a[i].Along, b[i].Along) {
			return false
		}
	}
	return true
}

func TestPositionAt(t *testing.T) {
	knots := []Knot{{sec(0), 0}, {sec(60), 1000}, {sec(120), 1000}}
	for _, tc := range []struct {
		at   int
		ease bool
		want float64
	}{
		{-60, true, 0},
		{15, true, 156.25}, // slow off the stop
		{15, false, 250},
		{30, true, 500},
		{90, true, 1000},
		{300, true, 1000},
	} {
		if got, ok := PositionAt(knots, tc.ease, sec(tc.at)); !ok || !near(got, tc.want) {
			t.Errorf("at %ds (ease %v): got %.2f, want %.2f", tc.at, tc.ease, got, tc.want)
		}
	}
	if _, ok := PositionAt(nil, true, t0); ok {
		t.Errorf("placed a train without knots")
	}
}

func TestDeadReckon(t *testing.T) {
	for _, tc := range []struct {
		name                string
		along, speed, until float64
		want                []Knot
	}{
		{"on until the next stop", 100, 20, 500, []Knot{{sec(0), 100}, {sec(20), 500}}},
		{"for at most a while", 100, 20, 5000, []Knot{{sec(0), 100}, {sec(30), 700}}},
		{"standing", 100, 0, 500, []Knot{{sec(0), 100}}},
		{"at the stop", 500, 3, 500, []Knot{{sec(0), 500}}},
	} {
		if got := DeadReckon(t0, tc.along, tc.speed, tc.until, 30*time.Second); !sameKnots(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSubwayKnots(t *testing.T) {
	// Stations a kilometer apart, A to D; a run between them takes 100 s.
	m := SubwayModel{Dwell: 20 * time.Second, Speed: 10}
	stations := map[string]float64{"A": 0, "B": 1000, "C": 2000, "D": 3000}
	along := func(id string) (float64, bool) { a, ok := stations[id]; return a, ok }
	all := []float64{0, 1000, 2000, 3000}
	stop := func(id string, s int) LiveStop { return LiveStop{StopID: id, Time: sec(s)} }
	trip := func(ss ...LiveStop) LiveTrip { return LiveTrip{Stops: ss} }
	at := func(id string, s int) *Arrival { return &Arrival{id, sec(s)} }

	for _, tc := range []struct {
		name string
		trip LiveTrip
		last *Arrival
		want []Knot
	}{
		{
			"from the stop reached, standing there, then waiting at the next",
			trip(stop("X", 30), stop("C", 60), stop("D", 180)), at("B", -10),
			[]Knot{{sec(-10), 1000}, {sec(10), 1000}, {sec(110), 2000}},
		},
		{
			"on to the next stop even if it is late for it",
			trip(stop("C", -5), stop("D", 90)), at("B", -120),
			[]Knot{{sec(-120), 1000}, {sec(-100), 1000}, {sec(0), 2000}},
		},
		{
			"leaving the start of the path in time for the next stop",
			trip(stop("B", 300)), at("A", -60),
			[]Knot{{sec(-60), 0}, {sec(200), 0}, {sec(300), 1000}},
		},
		{
			"without an arrival, from the last stop the feed lists as left",
			trip(stop("B", -30), stop("C", -10), stop("D", 90)), nil,
			[]Knot{{sec(-10), 2000}, {sec(10), 2000}, {sec(110), 3000}},
		},
		{
			"with nothing known, from the station before, as predicted",
			trip(stop("C", 60), stop("D", 180)), nil,
			[]Knot{{sec(-40), 1000}, {sec(60), 2000}},
		},
		{
			"at its last stop",
			trip(stop("D", -10)), at("D", -10),
			[]Knot{{sec(-10), 3000}},
		},
	} {
		if got := m.Knots(tc.trip, tc.last, along, all, t0); !sameKnots(got, tc.want) {
			t.Errorf("%s:\n got %v\nwant %v", tc.name, got, tc.want)
		}
	}
}

func TestArrivals(t *testing.T) {
	var a Arrivals
	report := func(stopped string, reported int, ss ...LiveStop) []LiveTrip {
		return []LiveTrip{{TripID: "t", StoppedAt: stopped, Reported: sec(reported), Stops: ss}}
	}
	check := func(when string, stop string, s int) {
		t.Helper()
		if arr, ok := a.Get("t"); !ok || arr.StopID != stop || !arr.Time.Equal(sec(s)) {
			t.Errorf("%s: got %+v, want %s at %+ds", when, arr, stop, s)
		}
	}

	a.Update(report("B", 0), sec(5))
	check("first seen", "B", 0)
	a.Update(report("B", 17), sec(22))
	check("reported again", "B", 0)
	a.Update(report("C", 34, LiveStop{StopID: "C", Time: sec(28)}), sec(39))
	check("at the next stop, which the feed lists", "C", 28)

	a.Update(nil, sec(39+11*60))
	if _, ok := a.Get("t"); ok {
		t.Errorf("still remembered after leaving the feed")
	}
}
