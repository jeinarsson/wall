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
	// Stations a kilometer apart, A to D; at 10 m/s a run takes 100 s.
	m := SubwayModel{MinSpeed: 5, MaxSpeed: 20, Lost: 5 * time.Minute}
	stations := map[string]float64{"A": 0, "B": 1000, "C": 2000, "D": 3000}
	along := func(id string) (float64, bool) { a, ok := stations[id]; return a, ok }
	stop := func(id string, s int) LiveStop { return LiveStop{StopID: id, Time: sec(s)} }
	at := func(id string, arrived, reported int) Visit {
		return Visit{StopID: id, Arrived: sec(arrived), Reported: sec(reported)}
	}
	left := func(id string, s int, ahead ...LiveStop) Visit {
		return Visit{StopID: id, Arrived: sec(s - 30), Reported: sec(s), Left: sec(s), Ahead: ahead}
	}

	for _, tc := range []struct {
		name string
		v    Visit
		now  int
		want []Knot
	}{
		{"standing where it is reported", at("B", -40, -10), 0, []Knot{{sec(-40), 1000}}},
		{"lost while standing", at("B", -400, -301), 0, nil},
		{
			"running to the next stop as predicted when it left",
			left("B", -50, stop("C", 50), stop("D", 150)), 0,
			[]Knot{{sec(-50), 1000}, {sec(50), 2000}},
		},
		{
			"skipping stops it doesn't stop at, as an express",
			left("A", -50, stop("C", 150)), 0,
			[]Knot{{sec(-50), 0}, {sec(150), 2000}},
		},
		{
			"no faster than MaxSpeed",
			left("B", -50, stop("C", -40)), 0,
			[]Knot{{sec(-50), 1000}, {sec(0), 2000}},
		},
		{
			"no slower than MinSpeed",
			left("B", -50, stop("C", 500)), 0,
			[]Knot{{sec(-50), 1000}, {sec(150), 2000}},
		},
		{
			"ignoring stops behind or off the path",
			left("C", -50, stop("B", 10), stop("X", 20), stop("D", 50)), 0,
			[]Knot{{sec(-50), 2000}, {sec(50), 3000}},
		},
		{
			"lost when long overdue at the next stop",
			left("B", -500, stop("C", -400)), 0,
			nil,
		},
		{"at the end of its path", left("D", -10), 0, []Knot{{sec(-10), 3000}}},
		{"off the path", at("X", -10, -10), 0, nil},
	} {
		if got := m.Knots(tc.v, along, sec(tc.now)); !sameKnots(got, tc.want) {
			t.Errorf("%s:\n got %v\nwant %v", tc.name, got, tc.want)
		}
	}
}

func TestSubwayTracker(t *testing.T) {
	var tr SubwayTracker
	// The feed at time feed has the train at stopped, last reported at
	// reported, with stops ahead.
	update := func(feed int, stopped string, reported int, ss ...LiveStop) {
		tr.Update([]LiveTrip{{TripID: "t", StoppedAt: stopped, Reported: sec(reported), Stops: ss}}, sec(feed))
	}
	stop := func(id string, s int) LiveStop { return LiveStop{StopID: id, Time: sec(s)} }
	check := func(when string, want Visit) {
		t.Helper()
		v, ok := tr.Get("t")
		if !ok || v.StopID != want.StopID || !v.Arrived.Equal(want.Arrived) || !v.Reported.Equal(want.Reported) ||
			!v.Left.Equal(want.Left) || len(v.Ahead) != len(want.Ahead) {
			t.Errorf("%s:\n got %+v\nwant %+v", when, v, want)
		}
	}

	update(0, "B", 10, stop("C", 60))
	if _, ok := tr.Get("t"); ok {
		t.Errorf("tracking a train that hasn't started")
	}
	// At B: reported every few seconds, with B listed at the time of the
	// latest report.
	update(20, "B", 12, stop("B", 12), stop("C", 120), stop("D", 240))
	check("first seen", Visit{StopID: "B", Arrived: sec(12), Reported: sec(12)})
	update(28, "B", 25, stop("B", 25), stop("C", 125), stop("D", 245))
	update(28, "B", 25, stop("B", 25), stop("C", 125), stop("D", 245))
	check("reported again", Visit{StopID: "B", Arrived: sec(12), Reported: sec(25)})
	// The reports stop as it leaves; B drops off its trip a while later.
	update(50, "B", 25, stop("B", 25), stop("C", 125), stop("D", 245))
	check("not reported for a while", Visit{StopID: "B", Arrived: sec(12), Reported: sec(25)})
	update(65, "B", 25, stop("C", 130), stop("D", 250))
	ahead := []LiveStop{stop("C", 130), stop("D", 250)}
	check("B dropped off", Visit{StopID: "B", Arrived: sec(12), Reported: sec(25), Left: sec(25), Ahead: ahead})
	update(80, "B", 25, stop("C", 100), stop("D", 220))
	check("predictions revised", Visit{StopID: "B", Arrived: sec(12), Reported: sec(25), Left: sec(25), Ahead: ahead})
	// At C, which the feed lists with when it got there.
	update(140, "C", 136, stop("C", 128), stop("D", 230))
	check("at C", Visit{StopID: "C", Arrived: sec(128), Reported: sec(136)})
	// Reports stop, but C lingers.
	update(200, "C", 136, stop("C", 136), stop("D", 230))
	check("C still listed", Visit{StopID: "C", Arrived: sec(128), Reported: sec(136), Left: sec(136), Ahead: []LiveStop{stop("D", 230)}})
	// A train first seen after it left.
	tr.Update([]LiveTrip{{TripID: "u", StoppedAt: "A", Reported: sec(100), Stops: []LiveStop{stop("B", 300)}}}, sec(200))
	if v, _ := tr.Get("u"); !v.Left.Equal(sec(100)) || len(v.Ahead) != 1 {
		t.Errorf("first seen after leaving: got %+v", v)
	}

	tr.Update(nil, sec(200+11*60))
	if _, ok := tr.Get("t"); ok {
		t.Errorf("still remembered after leaving the feed")
	}
}
