package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/jeinarsson/wall/gcal"
)

type calendar struct {
	id           string
	color        string
	excludeRegex *regexp.Regexp
	includeRegex *regexp.Regexp
}

type server struct {
	gcalClient *gcal.Client
	calendars  []*calendar
}

func NewServer(ctx context.Context) (*server, error) {
	c, err := gcal.NewClient(ctx, "../wall-gcloud.key")
	if err != nil {
		return nil, err
	}

	return &server{
		gcalClient: c,
		calendars: []*calendar{
			{
				id:           "9me0hsfh7oc365aqf39i9j6j38@group.calendar.google.com",
				color:        "#d85675",
				excludeRegex: nil,
				includeRegex: nil,
			},
			{
				id:           "b7cjs8ctbcthr18qcudua43k2o@group.calendar.google.com",
				color:        "rgb(216, 190, 94)",
				excludeRegex: nil,
				includeRegex: nil,
			},
			{
				id:           "soi6307ajh45fdn2j3i1f1lhe6j5fi7i@import.calendar.google.com",
				color:        "rgb(149, 115, 103)",
				excludeRegex: regexp.MustCompile("Virtual"),
				includeRegex: nil,
			},
		},
	}, nil
}

func main() {
	ctx := context.Background()
	s, err := NewServer(ctx)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/api/calendarEvents", s.getCalendarEvents)
	if err := http.ListenAndServe(":9000", nil); err != nil {
		log.Fatal(err)
	}
}

func (s *server) getCalendarEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	type event struct {
		Summary string
		Start   string
		End     string
		AllDay  bool
		Color   string
	}
	jsonData := []event{}

	for _, cal := range s.calendars {
		events, err := s.gcalClient.Events(ctx, cal.id, time.Now(), time.Now().AddDate(0, 0, 30))
		if err != nil {
			log.Fatal(err)
		}
		for _, item := range events {
			if cal.excludeRegex != nil {
				if cal.excludeRegex.MatchString(item.Summary) {
					continue
				}
			}
			if cal.includeRegex != nil {
				if !cal.includeRegex.MatchString(item.Summary) {
					continue
				}
			}

			allDay := false
			start := item.Start.DateTime
			if start == "" {
				start = item.Start.Date
				allDay = true
			}
			end := item.End.DateTime
			if end == "" {
				end = item.End.Date
				allDay = true
			}
			jsonData = append(jsonData, event{
				Summary: item.Summary,
				Start:   start,
				End:     end,
				AllDay:  allDay,
				Color:   cal.color,
			})
		}
	}
	json.NewEncoder(w).Encode(jsonData)
	w.Header().Add("Content-Type", "application/json")
}
