package main

import (
	"testing"
	"time"

	"github.com/jeinarsson/wall/transit"
)

var now = time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)

func at(min int) time.Time { return now.Add(time.Duration(min) * time.Minute) }

func TestSubwayBoard(t *testing.T) {
	s := &subwaySign{routes: []string{"E", "F"}}
	stopTimes := []transit.StopTime{
		{RouteID: "F", Time: at(9)},
		{RouteID: "E", Time: at(4)},
		{RouteID: "E", Time: at(4)}, // listed twice
		{RouteID: "F", Time: at(2)},
		{RouteID: "F", Time: at(-1)},                // at the platform
		{RouteID: "E", Time: at(-3)},                // lost
		{RouteID: "F", Time: at(5), Canceled: true}, // skips the stop
		{RouteID: "M", Time: at(3)},                 // not shown
	}
	routes := map[string]transit.Route{"E": {Color: "0062CF", TextColor: "FFFFFF"}}
	alerts := []transit.Alert{
		{Type: "Delays", Severity: 26, Text: "delays", Periods: []transit.Period{{}}, Entities: []transit.Entity{{RouteID: "E"}}},
		{Type: "Planned - Suspended", Severity: 21, Text: "suspended", Periods: []transit.Period{{}}, Entities: []transit.Entity{{RouteID: "F"}}},
		{Type: "Delays", Severity: 26, Text: "A delays", Periods: []transit.Period{{}}, Entities: []transit.Entity{{RouteID: "A"}}},
	}
	filter := transit.AlertFilter{Routes: []string{"E", "F"}, RouteWide: true}

	b := s.board(stopTimes, routes, alerts, filter, now)

	if len(b.Routes) != 2 || b.Routes[0].Route != "E" || b.Routes[1].Route != "F" {
		t.Fatalf("got routes %+v, want E, F", b.Routes)
	}
	if got := b.Routes[0].Times; len(got) != 1 || !got[0].Equal(at(4)) {
		t.Errorf("E times = %v, want [+4m]", got)
	}
	if got := b.Routes[1].Times; len(got) != 3 || !got[0].Equal(at(-1)) || !got[1].Equal(at(2)) || !got[2].Equal(at(9)) {
		t.Errorf("F times = %v, want [-1m +2m +9m]", got)
	}
	if b.Routes[0].Color != "#0062CF" || b.Routes[1].Color != "#808080" {
		t.Errorf("got colors %s, %s", b.Routes[0].Color, b.Routes[1].Color)
	}
	if len(b.Alerts) != 2 || b.Alerts[0].Text != "delays" || b.Alerts[0].Severe || b.Alerts[1].Text != "suspended" || !b.Alerts[1].Severe {
		t.Errorf("got alerts %+v, want delays then severe suspended", b.Alerts)
	}
}

func TestLIRRBoard(t *testing.T) {
	west, east := uint32(1), uint32(0)
	s := &lirrSign{directionID: west}
	static := &lirrStatic{
		routes: map[string]transit.Route{"1": {Color: "00985F"}},
		stops:  map[string]transit.Stop{"237": {Name: "Penn Station"}},
	}
	trip := func(id, headsign string, dir uint32) transit.Trip {
		return transit.Trip{ID: id, RouteID: "1", Headsign: headsign, ShortName: id, DirectionID: dir}
	}
	scheduled := []transit.ScheduledStop{
		{Trip: trip("left", "Penn Station", west), Time: at(-10)},
		{Trip: trip("late", "Penn Station", west), Time: at(-2)},
		{Trip: trip("ontime", "Grand Central", west), Time: at(10)},
		{Trip: trip("canceled", "Penn Station", west), Time: at(20)},
		{Trip: trip("partly", "Penn Station", west), Time: at(30)},
		{Trip: trip("noprediction", "Grand Central", west), Time: at(40)},
		{Trip: trip("east", "Babylon", east), Time: at(15)},
	}
	live := []transit.StopTime{
		{TripID: "left", Time: at(-9)},
		{TripID: "late", Time: at(3), Delay: 5 * time.Minute},
		{TripID: "ontime", Time: at(10)},
		{TripID: "partly", Time: at(31)},
		{TripID: "extra", RouteID: "1", DirectionID: &west, Time: at(25), Delay: time.Minute, LastStopID: "237"},
	}
	canceled := map[string]bool{"canceled": true, "partly": true}

	b := s.board(static, scheduled, live, canceled, nil, now)

	want := []struct {
		train, dest string
		expected    *time.Time
		canceled    bool
	}{
		{"late", "Penn Station", ptr(at(3)), false},
		{"ontime", "Grand Central", ptr(at(10)), false},
		{"canceled", "Penn Station", nil, true},
		{"", "Penn Station", ptr(at(25)), false}, // extra
		{"partly", "Penn Station", ptr(at(31)), false},
		{"noprediction", "Grand Central", nil, false},
	}
	if len(b.Departures) != len(want) {
		t.Fatalf("got %d departures, want %d: %+v", len(b.Departures), len(want), b.Departures)
	}
	for i, w := range want {
		d := b.Departures[i]
		if d.Train != w.train || d.Destination != w.dest || d.Canceled != w.canceled ||
			(d.Expected == nil) != (w.expected == nil) || (d.Expected != nil && !d.Expected.Equal(*w.expected)) {
			t.Errorf("departure %d = %+v, want %+v", i, d, w)
		}
	}
	if d := b.Departures[3]; !d.Scheduled.Equal(at(24)) || d.Color != "#00985F" {
		t.Errorf("extra train = %+v, want scheduled +24m in green", d)
	}
}

func TestLIRRTrainTimeBoard(t *testing.T) {
	s := &lirrSign{direction: "W"}
	static := &lirrStatic{
		routes: map[string]transit.Route{"1": {Color: "00985F"}, "2": {Color: "CE8E00"}},
		stops:  map[string]transit.Stop{"237": {Code: "NYK", Name: "Penn Station"}, "214": {Code: "WDD", Name: "Woodside"}},
		trains: map[string]transit.Trip{
			"1579": {RouteID: "1", Headsign: "Penn Station"},
			"2775": {RouteID: "2", Headsign: "Grand Central"},
		},
	}
	late, early := 2*time.Minute, -10*time.Second
	arrivals := []transit.TrainArrival{
		{TrainNum: "2775", Direction: "W", Scheduled: at(30), Status: "EN_ROUTE", Late: &early},
		{TrainNum: "1579", Direction: "W", Scheduled: at(-1), Status: "BERTHED", Late: &late, Stops: []string{"NYK"}},
		{TrainNum: "1583", Direction: "W", Scheduled: at(60), Status: "EN_ROUTE", Stops: []string{"WDD"}}, // not in the timetable
		{TrainNum: "1585", Direction: "W", Scheduled: at(70), Status: "EN_ROUTE", Held: true},
		{TrainNum: "1587", Direction: "W", Scheduled: at(80), Status: "DOORS_CLOSED", Canceled: true},
		{TrainNum: "2786", Direction: "E", Scheduled: at(5), Status: "ARRIVING"},
	}

	b := s.trainTimeBoard(static, arrivals, nil, now)

	want := []struct {
		train, dest, status string
		expected            *time.Time
		canceled            bool
	}{
		{"1579", "Penn Station", "At station", ptr(at(1)), false},
		{"2775", "Grand Central", "", ptr(at(30).Add(early)), false},
		{"1583", "Woodside", "", nil, false},
		{"1585", "", "Held", nil, false},
		{"1587", "", "Doors closed", nil, true},
	}
	if len(b.Departures) != len(want) {
		t.Fatalf("got %d departures, want %d: %+v", len(b.Departures), len(want), b.Departures)
	}
	for i, w := range want {
		d := b.Departures[i]
		if d.Train != w.train || d.Destination != w.dest || d.Status != w.status || d.Canceled != w.canceled ||
			(d.Expected == nil) != (w.expected == nil) || (d.Expected != nil && !d.Expected.Equal(*w.expected)) {
			t.Errorf("departure %d = %+v, want %+v", i, d, w)
		}
	}
	if b.Departures[0].Color != "#00985F" {
		t.Errorf("got color %s, want the route's", b.Departures[0].Color)
	}
}

func ptr[T any](v T) *T { return &v }

// TestForestHillsAlerts checks the subway alert filter against headlines
// from the MTA's feed.
func TestForestHillsAlerts(t *testing.T) {
	tr, err := newTrains(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filter := tr.subway.alerts
	filter.Stops = append(filter.Stops, "D15", "F12") // as if from Stations.csv
	south, north := uint32(1), uint32(0)

	tests := []struct {
		name  string
		alert transit.Alert
		want  bool
	}{
		{"Brooklyn delays", transit.Alert{Type: "Delays",
			Text:     "Coney Island-bound [F] trains are running with delays while we perform track maintenance along the line in Brooklyn.",
			Entities: []transit.Entity{{RouteID: "F"}}}, false},
		{"our direction", transit.Alert{Type: "Delays",
			Text:     "Coney Island-bound [F] trains are running with delays after we addressed a signal problem.",
			Entities: []transit.Entity{{RouteID: "F"}}}, true},
		{"Manhattan-bound", transit.Alert{Type: "Delays",
			Text:     "Manhattan-bound [E][F] trains are running with delays while we address a signal problem at Jackson Hts-Roosevelt Av.",
			Entities: []transit.Entity{{RouteID: "E"}, {RouteID: "F"}}}, true},
		{"Jamaica-bound", transit.Alert{Type: "Delays",
			Text:     "Jamaica-bound [E] trains are running with delays.",
			Entities: []transit.Entity{{RouteID: "E"}}}, false},
		{"Jamaica Center-bound", transit.Alert{Type: "Delays",
			Text:     "Jamaica Center-bound [E] trains are running with delays.",
			Entities: []transit.Entity{{RouteID: "E"}}}, false},
		{"uptown", transit.Alert{Type: "Delays",
			Text:     "Uptown [E] trains are running with delays.",
			Entities: []transit.Entity{{RouteID: "E"}}}, false},
		{"other route", transit.Alert{Type: "Delays",
			Text:     "Uptown [A][C][D] trains are running with delays while we perform planned track maintenance along the line in Manhattan.",
			Entities: []transit.Entity{{RouteID: "A"}, {RouteID: "C"}, {RouteID: "D"}}}, false},
		{"boarding change", transit.Alert{Type: "Boarding Change",
			Text:     "In Manhattan, all downtown [F] at 47-50 Sts-Rockefeller Ctr and 42 St-Bryant Park board from the express track",
			Entities: []transit.Entity{{RouteID: "F", StopID: "D15", DirectionID: &south}}}, false},
		{"entrance closed", transit.Alert{Type: "Station Notice",
			Text:     "Enter 5 Av/53 St [E][F] Station at 5 Av only Madison Av entrance will be exit only",
			Entities: []transit.Entity{{RouteID: "E", StopID: "F12"}}}, false},
		{"skips stop in Manhattan", transit.Alert{Type: "Planned - Stops Skipped",
			Text:     "Downtown [F] trains skip 47-50 Sts-Rockefeller Ctr",
			Entities: []transit.Entity{{RouteID: "F", StopID: "D15", DirectionID: &south}}}, true},
		{"skips stop, other direction", transit.Alert{Type: "Planned - Stops Skipped",
			Text:     "Uptown [F] trains skip 47-50 Sts-Rockefeller Ctr",
			Entities: []transit.Entity{{RouteID: "F", StopID: "D15", DirectionID: &north}}}, false},
		{"east of Forest Hills", transit.Alert{Type: "Planned - Stops Skipped",
			Text:     "Manhattan-bound [E][F] trains skip 75 Av",
			Entities: []transit.Entity{{RouteID: "E", StopID: "F07", DirectionID: &south}}}, false},
		{"Forest Hills", transit.Alert{Type: "Planned - Express to Local",
			Text:     "Manhattan-bound [E][F] trains run local from Forest Hills-71 Av to Queens Plaza",
			Entities: []transit.Entity{{RouteID: "E", StopID: "G08", DirectionID: &south}, {RouteID: "E", StopID: "G09", DirectionID: &south}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.alert.Periods = []transit.Period{{}}
			if got := filter.Match(tt.alert, now); got != tt.want {
				t.Errorf("Match = %v, want %v", got, tt.want)
			}
		})
	}
}
