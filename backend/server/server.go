package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/jeinarsson/wall/gcal"
)

type server struct {
	gcalClient *gcal.Client
	calId      string
}

func NewServer(ctx context.Context) (*server, error) {
	c, err := gcal.NewClient(ctx, "../wall-gcloud.key")
	if err != nil {
		return nil, err
	}
	return &server{
		gcalClient: c,
		calId:      "9me0hsfh7oc365aqf39i9j6j38@group.calendar.google.com",
	}, nil
}

func main() {
	ctx := context.Background()
	s, err := NewServer(ctx)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/calendarEvents", s.getCalendarEvents)
	if err := http.ListenAndServe(":9000", nil); err != nil {
		log.Fatal(err)
	}
}

func (s *server) getCalendarEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	events, err := s.gcalClient.Events(ctx, s.calId, time.Now(), time.Now().AddDate(0, 0, 30))
	if err != nil {
		log.Fatal(err)
	}

	type event struct {
		Summary string
		Start   string
		End     string
		AllDay  bool
	}
	jsonData := []event{}
	for _, item := range events {
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
		})
	}
	json.NewEncoder(w).Encode(jsonData)
	w.Header().Add("Content-Type", "application/json")
}
