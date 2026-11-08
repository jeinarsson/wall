package transit

import (
	"regexp"
	"testing"
	"time"
)

func TestParseAlerts(t *testing.T) {
	alerts, err := parseAlerts([]byte(`{"entity": [{
		"id": "lmm:alert:1",
		"alert": {
			"active_period": [{"start": 1000}],
			"informed_entity": [
				{"route_id": "F", "stop_id": "D15", "direction_id": 1,
				 "transit_realtime.mercury_entity_selector": {"sort_order": "MTASBWY:F:16"}},
				{"trip": {"trip_id": "GO202_26_6114"},
				 "transit_realtime.mercury_entity_selector": {"sort_order": "MTASBWY:F:26"}}
			],
			"header_text": {"translation": [
				{"text": "<p>html</p>", "language": "en-html"},
				{"text": "Downtown [F] trains skip 47-50 Sts", "language": "en"}
			]},
			"transit_realtime.mercury_alert": {"alert_type": "Planned - Stops Skipped"}
		}
	}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts, want 1", len(alerts))
	}
	a := alerts[0]
	if a.Type != "Planned - Stops Skipped" || a.Severity != 26 || a.Text != "Downtown [F] trains skip 47-50 Sts" {
		t.Errorf("got type %q, severity %d, text %q", a.Type, a.Severity, a.Text)
	}
	if !a.Periods[0].End.IsZero() || a.Periods[0].Start != time.Unix(1000, 0) {
		t.Errorf("got period %v", a.Periods[0])
	}
	e := a.Entities[0]
	if e.RouteID != "F" || e.StopID != "D15" || e.DirectionID == nil || *e.DirectionID != 1 {
		t.Errorf("got entity %+v", e)
	}
	if a.Entities[1].TripID != "GO202_26_6114" {
		t.Errorf("got entity %+v", a.Entities[1])
	}
}

func TestAlertFilter(t *testing.T) {
	now := time.Unix(10000, 0)
	active := []Period{{Start: time.Unix(0, 0)}}
	south, north := uint32(1), uint32(0)

	filter := AlertFilter{
		ExcludeTypes: []string{"Station Notice"},
		Routes:       []string{"E", "F"},
		Stops:        []string{"G08", "D15"},
		RouteWide:    true,
		DirectionID:  &south,
		ExcludeText:  regexp.MustCompile(`(?i)jamaica-bound|\bin brooklyn\b`),
		IncludeText:  regexp.MustCompile(`(?i)\bin manhattan\b`),
	}

	tests := []struct {
		name  string
		alert Alert
		want  bool
	}{
		{
			name:  "route wide",
			alert: Alert{Type: "Delays", Text: "[F] trains are delayed", Entities: []Entity{{RouteID: "F"}}},
			want:  true,
		},
		{
			name:  "other route",
			alert: Alert{Type: "Delays", Text: "[A] trains are delayed", Entities: []Entity{{RouteID: "A"}}},
			want:  false,
		},
		{
			name: "inactive",
			alert: Alert{Type: "Delays", Entities: []Entity{{RouteID: "F"}},
				Periods: []Period{{Start: time.Unix(0, 0), End: time.Unix(5000, 0)}}},
			want: false,
		},
		{
			name:  "excluded type",
			alert: Alert{Type: "Station Notice", Entities: []Entity{{RouteID: "F", StopID: "G08"}}},
			want:  false,
		},
		{
			name:  "our stop",
			alert: Alert{Type: "Planned - Stops Skipped", Entities: []Entity{{RouteID: "F", StopID: "D15", DirectionID: &south}}},
			want:  true,
		},
		{
			name:  "our stop, other direction",
			alert: Alert{Type: "Planned - Stops Skipped", Entities: []Entity{{RouteID: "F", StopID: "D15", DirectionID: &north}}},
			want:  false,
		},
		{
			name:  "other stop",
			alert: Alert{Type: "Planned - Stops Skipped", Entities: []Entity{{RouteID: "F", StopID: "F27"}}},
			want:  false,
		},
		{
			name:  "other direction by text",
			alert: Alert{Type: "Delays", Text: "Jamaica-bound [E] trains are delayed", Entities: []Entity{{RouteID: "E"}}},
			want:  false,
		},
		{
			name:  "elsewhere by text",
			alert: Alert{Type: "Delays", Text: "Coney Island-bound [F] trains are delayed in Brooklyn", Entities: []Entity{{RouteID: "F"}}},
			want:  false,
		},
		{
			name:  "elsewhere and here by text",
			alert: Alert{Type: "Delays", Text: "[F] trains are delayed in Brooklyn and in Manhattan", Entities: []Entity{{RouteID: "F"}}},
			want:  true,
		},
		{
			name:  "text ignored with direction data",
			alert: Alert{Type: "Delays", Text: "[F] trains are delayed in Brooklyn", Entities: []Entity{{RouteID: "F", DirectionID: &south}}},
			want:  true,
		},
		{
			name:  "trip",
			alert: Alert{Type: "Cancellations", Entities: []Entity{{RouteID: "F", TripID: "123"}}},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.alert.Periods == nil {
				tt.alert.Periods = active
			}
			if got := filter.Match(tt.alert, now); got != tt.want {
				t.Errorf("Match = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAlertFilterStopsOnly(t *testing.T) {
	now := time.Unix(10000, 0)
	west := uint32(1)
	filter := AlertFilter{Stops: []string{"55"}, DirectionID: &west}
	a := func(ents ...Entity) Alert {
		return Alert{Periods: []Period{{}}, Entities: ents}
	}
	if !filter.Match(a(Entity{RouteID: "12", StopID: "55", DirectionID: &west}), now) {
		t.Error("alert at stop didn't match")
	}
	if filter.Match(a(Entity{RouteID: "2"}), now) {
		t.Error("route wide alert matched without RouteWide")
	}
}

func TestTripsIn(t *testing.T) {
	now := time.Unix(10000, 0)
	alerts := []Alert{
		{Type: "Cancellations", Periods: []Period{{}}, Entities: []Entity{{RouteID: "1"}, {TripID: "a"}}},
		{Type: "Extra Service", Periods: []Period{{}}, Entities: []Entity{{TripID: "b"}}},
		{Type: "Cancellations", Periods: []Period{{End: time.Unix(1, 0)}}, Entities: []Entity{{TripID: "c"}}},
	}
	got := TripsIn(alerts, "Cancellations", now)
	if len(got) != 1 || !got["a"] {
		t.Errorf("got %v, want only a", got)
	}
}
