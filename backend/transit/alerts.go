package transit

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Alert is a service alert.
type Alert struct {
	ID       string
	Type     string // MTA alert type: "Delays", "Planned - Stops Skipped", ...
	Severity int    // MTA sort order; higher is more severe
	Text     string // headline, with routes written as "[E]"
	Periods  []Period
	Entities []Entity
}

// Period is when an alert is active. End is zero if open-ended.
type Period struct {
	Start, End time.Time
}

// Entity is something an alert applies to. Empty fields are unspecified:
// an entity with only a route applies to the whole route.
type Entity struct {
	RouteID     string
	StopID      string
	TripID      string
	DirectionID *uint32
}

func (a Alert) Active(now time.Time) bool {
	for _, p := range a.Periods {
		if !now.Before(p.Start) && (p.End.IsZero() || now.Before(p.End)) {
			return true
		}
	}
	return false
}

// parseAlerts reads the MTA's JSON alert feeds. These carry the MTA's own
// GTFS-Realtime extensions with the alert type and severity.
func parseAlerts(b []byte) ([]Alert, error) {
	type translated struct {
		Translation []struct {
			Text     string `json:"text"`
			Language string `json:"language"`
		} `json:"translation"`
	}
	var feed struct {
		Entity []struct {
			ID    string `json:"id"`
			Alert struct {
				ActivePeriod []struct {
					Start int64 `json:"start"`
					End   int64 `json:"end"`
				} `json:"active_period"`
				InformedEntity []struct {
					RouteID     string  `json:"route_id"`
					StopID      string  `json:"stop_id"`
					DirectionID *uint32 `json:"direction_id"`
					Trip        *struct {
						TripID string `json:"trip_id"`
					} `json:"trip"`
					Selector struct {
						SortOrder string `json:"sort_order"` // "MTASBWY:E:26"
					} `json:"transit_realtime.mercury_entity_selector"`
				} `json:"informed_entity"`
				HeaderText translated `json:"header_text"`
				Mercury    struct {
					AlertType string `json:"alert_type"`
				} `json:"transit_realtime.mercury_alert"`
			} `json:"alert"`
		} `json:"entity"`
	}
	if err := json.Unmarshal(b, &feed); err != nil {
		return nil, err
	}

	var alerts []Alert
	for _, e := range feed.Entity {
		a := Alert{ID: e.ID, Type: e.Alert.Mercury.AlertType}
		for _, t := range e.Alert.HeaderText.Translation {
			if t.Language == "en" || a.Text == "" {
				a.Text = t.Text
			}
		}
		for _, p := range e.Alert.ActivePeriod {
			var period Period
			if p.Start != 0 {
				period.Start = time.Unix(p.Start, 0)
			}
			if p.End != 0 {
				period.End = time.Unix(p.End, 0)
			}
			a.Periods = append(a.Periods, period)
		}
		for _, ie := range e.Alert.InformedEntity {
			ent := Entity{RouteID: ie.RouteID, StopID: ie.StopID, DirectionID: ie.DirectionID}
			if ie.Trip != nil {
				ent.TripID = ie.Trip.TripID
			}
			a.Entities = append(a.Entities, ent)
			so := ie.Selector.SortOrder
			if sev, err := strconv.Atoi(so[strings.LastIndex(so, ":")+1:]); err == nil && sev > a.Severity {
				a.Severity = sev
			}
		}
		alerts = append(alerts, a)
	}
	return alerts, nil
}

// AlertFilter selects alerts. An alert matches if it is active, its type
// isn't excluded and one of its entities matches every criterion below.
type AlertFilter struct {
	ExcludeTypes []string

	// Routes the entity must be on. Empty means any route.
	Routes []string
	// Stops the entity must be at. The NYC subway uses station ids here,
	// without the N/S of the platform.
	Stops []string
	// RouteWide also matches entities without a stop, which apply to
	// the whole route. Needs Routes.
	RouteWide bool
	// DirectionID the entity must have, if it has one.
	DirectionID *uint32

	// Alerts matched only by route often say where and which way in their
	// text instead. Such an alert is dropped if its text matches
	// ExcludeText, unless it also matches IncludeText.
	ExcludeText *regexp.Regexp
	IncludeText *regexp.Regexp
}

// Match reports whether a matches the filter at time now.
func (f AlertFilter) Match(a Alert, now time.Time) bool {
	if !a.Active(now) || slices.Contains(f.ExcludeTypes, a.Type) {
		return false
	}
	for _, e := range a.Entities {
		if len(f.Routes) > 0 && !slices.Contains(f.Routes, e.RouteID) {
			continue
		}
		if f.DirectionID != nil && e.DirectionID != nil && *e.DirectionID != *f.DirectionID {
			continue
		}
		if e.StopID != "" && slices.Contains(f.Stops, e.StopID) {
			return true
		}
		if e.StopID == "" && e.TripID == "" && f.RouteWide && len(f.Routes) > 0 {
			if e.DirectionID != nil || !f.excludedByText(a.Text) {
				return true
			}
		}
	}
	return false
}

func (f AlertFilter) excludedByText(text string) bool {
	if f.ExcludeText == nil || !f.ExcludeText.MatchString(text) {
		return false
	}
	return f.IncludeText == nil || !f.IncludeText.MatchString(text)
}

// TripsIn returns the ids of the trips named by alerts of the given type
// that are active at time now, such as the trains in "Cancellations".
func TripsIn(alerts []Alert, alertType string, now time.Time) map[string]bool {
	trips := map[string]bool{}
	for _, a := range alerts {
		if a.Type != alertType || !a.Active(now) {
			continue
		}
		for _, e := range a.Entities {
			if e.TripID != "" {
				trips[e.TripID] = true
			}
		}
	}
	return trips
}
