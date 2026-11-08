package transit

import (
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Trip is a row from a GTFS trips.txt.
type Trip struct {
	ID          string
	RouteID     string
	Headsign    string // "Penn Station"
	ShortName   string // the LIRR train number
	DirectionID uint32
}

// ScheduledStop is a trip's scheduled departure from a stop.
type ScheduledStop struct {
	Trip Trip
	Time time.Time
}

// Timetable is the scheduled departures from one stop.
type Timetable struct {
	loc      *time.Location
	services map[string]map[string]bool // date (YYYYMMDD) -> service ids
	stops    []timetableStop
}

type timetableStop struct {
	trip    Trip
	service string
	// Since noon minus 12h on the service date; can be more than 24h
	// for trips that run past midnight.
	offset time.Duration
}

// LoadTimetable reads the departures from stopID out of a static GTFS
// feed whose times are in loc.
//
// Service dates are read from calendar_dates.txt only; calendar.txt is
// not supported (the LIRR feed doesn't use it).
func LoadTimetable(gtfs fs.FS, stopID string, loc *time.Location) (*Timetable, error) {
	t := &Timetable{loc: loc, services: map[string]map[string]bool{}}

	err := readFile(gtfs, "calendar_dates.txt", func(row Row) {
		if row("exception_type") != "1" {
			return
		}
		date := row("date")
		if t.services[date] == nil {
			t.services[date] = map[string]bool{}
		}
		t.services[date][row("service_id")] = true
	})
	if err != nil {
		return nil, err
	}

	type stopTime struct {
		trip   string
		offset time.Duration
	}
	var stopTimes []stopTime
	var parseErr error
	err = readFile(gtfs, "stop_times.txt", func(row Row) {
		if row("stop_id") != stopID || parseErr != nil {
			return
		}
		d, err := parseTime(row("departure_time"))
		if err != nil {
			parseErr = err
			return
		}
		stopTimes = append(stopTimes, stopTime{row("trip_id"), d})
	})
	if err == nil {
		err = parseErr
	}
	if err != nil {
		return nil, err
	}

	trips := map[string]Trip{}
	services := map[string]string{}
	err = readFile(gtfs, "trips.txt", func(row Row) {
		dir, _ := strconv.ParseUint(row("direction_id"), 10, 32)
		id := row("trip_id")
		trips[id] = Trip{
			ID:          id,
			RouteID:     row("route_id"),
			Headsign:    row("trip_headsign"),
			ShortName:   row("trip_short_name"),
			DirectionID: uint32(dir),
		}
		services[id] = row("service_id")
	})
	if err != nil {
		return nil, err
	}

	for _, st := range stopTimes {
		t.stops = append(t.stops, timetableStop{trips[st.trip], services[st.trip], st.offset})
	}
	return t, nil
}

// parseTime parses a GTFS time, "HH:MM:SS", where HH may be 24 or more.
func parseTime(s string) (time.Duration, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("bad time %q", s)
	}
	var d time.Duration
	for i, unit := range []time.Duration{time.Hour, time.Minute, time.Second} {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return 0, fmt.Errorf("bad time %q", s)
		}
		d += time.Duration(n) * unit
	}
	return d, nil
}

// Departures returns the scheduled departures in [from, to), ordered by
// time.
func (t *Timetable) Departures(from, to time.Time) []ScheduledStop {
	var deps []ScheduledStop
	// Trips from the previous service date can run past midnight.
	day := from.In(t.loc).AddDate(0, 0, -1)
	for !day.After(to) {
		// GTFS times count from noon minus 12h, which is midnight except
		// on days when daylight saving time changes.
		noon := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, t.loc)
		base := noon.Add(-12 * time.Hour)
		services := t.services[noon.Format("20060102")]
		for _, s := range t.stops {
			if !services[s.service] {
				continue
			}
			tm := base.Add(s.offset)
			if !tm.Before(from) && tm.Before(to) {
				deps = append(deps, ScheduledStop{s.trip, tm})
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].Time.Before(deps[j].Time) })
	return deps
}
