package transit

import (
	"testing"
	"testing/fstest"
	"time"
)

func TestTimetable(t *testing.T) {
	gtfs := fstest.MapFS{
		"calendar_dates.txt": {Data: []byte(`"service_id","date","exception_type"
"sat","20261107","1"
"sun","20261108","1"
"removed","20261108","2"
`)},
		"trips.txt": {Data: []byte(`"route_id","service_id","trip_id","trip_headsign","trip_short_name","direction_id"
"1","sat","late","Penn Station","1901","1"
"1","sun","morning","Grand Central","6115","1"
"1","sun","other-stop","Penn Station","6117","1"
"1","removed","removed","Penn Station","6119","1"
`)},
		"stop_times.txt": {Data: []byte(`"trip_id","arrival_time","departure_time","stop_id","stop_sequence"
"late","24:43:00","24:43:30","55","5"
"morning","07:43:00","07:43:00","55","17"
"other-stop","07:50:00","07:50:00","56","17"
"removed","08:00:00","08:00:00","55","17"
`)},
	}
	nyc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	tt, err := LoadTimetable(gtfs, "55", nyc)
	if err != nil {
		t.Fatal(err)
	}

	from := time.Date(2026, 11, 8, 0, 0, 0, 0, nyc)
	deps := tt.Departures(from, from.Add(24*time.Hour))
	if len(deps) != 2 {
		t.Fatalf("got %d departures, want 2: %v", len(deps), deps)
	}
	// Saturday's 24:43:30 is early on Sunday.
	if want := time.Date(2026, 11, 8, 0, 43, 30, 0, nyc); deps[0].Trip.ID != "late" || !deps[0].Time.Equal(want) {
		t.Errorf("got %s at %v, want late at %v", deps[0].Trip.ID, deps[0].Time, want)
	}
	if want := time.Date(2026, 11, 8, 7, 43, 0, 0, nyc); deps[1].Trip.ID != "morning" || !deps[1].Time.Equal(want) {
		t.Errorf("got %s at %v, want morning at %v", deps[1].Trip.ID, deps[1].Time, want)
	}
	if tr := deps[1].Trip; tr.Headsign != "Grand Central" || tr.ShortName != "6115" || tr.DirectionID != 1 || tr.RouteID != "1" {
		t.Errorf("got trip %+v", tr)
	}

	if deps := tt.Departures(from.Add(time.Hour), from.Add(7*time.Hour)); len(deps) != 0 {
		t.Errorf("got %v, want no departures between 1 and 7", deps)
	}
}

func TestParseTime(t *testing.T) {
	for s, want := range map[string]time.Duration{
		"07:43:00": 7*time.Hour + 43*time.Minute,
		"25:06:30": 25*time.Hour + 6*time.Minute + 30*time.Second,
	} {
		if got, err := parseTime(s); err != nil || got != want {
			t.Errorf("parseTime(%q) = %v, %v, want %v", s, got, err, want)
		}
	}
	if _, err := parseTime("7:43"); err == nil {
		t.Error("parseTime(7:43) succeeded")
	}
}
