package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/jeinarsson/wall/gcal"
	"github.com/jeinarsson/wall/transit"
)

const (
	// Static GTFS, for stop names and route colors.
	staticSubway = "https://rrgtfsfeeds.s3.amazonaws.com/gtfs_subway.zip"
	staticLIRR   = "https://rrgtfsfeeds.s3.amazonaws.com/gtfslirr.zip"

	// Realtime GTFS.
	feedACE  = "https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs-ace"
	feedBDFM = "https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs-bdfm"
	feedLIRR = "https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/lirr%2Fgtfs-lirr"
)

type calendar struct {
	id           string
	color        string
	excludeRegex *regexp.Regexp
	includeRegex *regexp.Regexp
}

// board is a list of departures shown on the wall.
type board struct {
	name            string
	count           int // how many departures to show
	showSchedule    bool
	showDestination bool
	feeds           []*transit.Feed
	filter          transit.Filter
}

type server struct {
	gcalClient *gcal.Client
	calendars  []*calendar
	statics    []*transit.StaticFeed
	feeds      []*transit.Feed
	boards     []*board
}

func NewServer(ctx context.Context, keyPath, cacheDir string) (*server, error) {
	c, err := gcal.NewClient(ctx, keyPath)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}
	subway := transit.NewStaticFeed(staticSubway, filepath.Join(cacheDir, "gtfs_subway.zip"))
	lirr := transit.NewStaticFeed(staticLIRR, filepath.Join(cacheDir, "gtfs_lirr.zip"))
	ace := transit.NewFeed(feedACE, subway)
	bdfm := transit.NewFeed(feedBDFM, subway)
	lirrFeed := transit.NewFeed(feedLIRR, lirr)
	westbound := uint32(1)

	return &server{
		gcalClient: c,
		statics:    []*transit.StaticFeed{subway, lirr},
		feeds:      []*transit.Feed{ace, bdfm, lirrFeed},
		boards: []*board{
			{
				name:            "subway",
				count:           3,
				showSchedule:    true,
				showDestination: true,
				feeds:           []*transit.Feed{ace, bdfm},
				// Forest Hills-71 Av. On Queens Blvd "S" is towards Manhattan.
				filter: transit.Filter{StopID: "G08S", Routes: []string{"E", "F"}},
			},
			{
				name:            "lirr",
				count:           2,
				showSchedule:    true,
				showDestination: true,
				feeds:           []*transit.Feed{lirrFeed},
				// Forest Hills, towards Penn Station / Grand Central.
				filter: transit.Filter{StopID: "55", DirectionID: &westbound},
			},
		},
		calendars: []*calendar{
			{
				id:           "9me0hsfh7oc365aqf39i9j6j38@group.calendar.google.com",
				color:        "#d85675",
				excludeRegex: nil,
				includeRegex: nil,
			},
			{
				id:           "b7cjs8ctbcthr18qcudua43k2o@group.calendar.google.com", // Birthdays
				color:        "rgb(216, 27, 96)",
				excludeRegex: nil,
				includeRegex: nil,
			},
			{
				id:           "792b9cd027ad35454d995f575d539c29d7f46a6b94b4903655193c3287770819@group.calendar.google.com",
				color:        "rgb(149, 115, 103)",
				excludeRegex: regexp.MustCompile("Virtual"),
				includeRegex: nil,
			},
			{
				id:           "en.usa#holiday@group.v.calendar.google.com",
				color:        "rgb(72, 145, 96)",
				excludeRegex: nil,
				includeRegex: nil,
			},
			{
				id:           "olmercyca@gmail.com",
				color:        "rgb(130, 139, 194)",
				excludeRegex: nil,
				includeRegex: nil,
			},
			{
				id:           "7deb7cecd8491b54ca0717a23a6d9ee1208f4d466b2967be89d4031c99ccea09@group.calendar.google.com", // WSTC Jonas
				color:        "rgb(231, 186, 81)",
				excludeRegex: nil,
				includeRegex: nil,
			},
			{
				id:           "e66e8bc9e4f80d9e2d9332ce988c9972efbe845f20324ffb7c6fc4368b0d4193@group.calendar.google.com", // WSTC Tora
				color:        "rgb(216, 190, 94)",
				excludeRegex: nil,
				includeRegex: nil,
			},
			{
				id:           "5c4b566891ba1cfcc5019ee8c44436cb62e65b754468147e28127ee1975aabf1@group.calendar.google.com", // WSTC Tora 2
				color:        "rgb(188, 194, 86)",
				excludeRegex: nil,
				includeRegex: nil,
			},
		},
	}, nil
}

func main() {
	keyPath := flag.String("key", "../wall-gcloud.key", "path to Google Cloud service account key")
	staticDir := flag.String("static", "", "directory with the built frontend to serve on /")
	cacheDir := flag.String("cache", "cache", "directory for downloaded data")
	addr := flag.String("addr", ":9000", "address to listen on")
	flag.Parse()

	ctx := context.Background()
	s, err := NewServer(ctx, *keyPath, *cacheDir)
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range s.statics {
		go f.Poll(ctx, 24*time.Hour)
	}
	for _, f := range s.feeds {
		go f.Poll(ctx, time.Minute)
	}

	http.HandleFunc("/api/calendarEvents", s.getCalendarEvents)
	http.HandleFunc("/api/departures", s.getDepartures)
	if *staticDir != "" {
		http.Handle("/", spaHandler(*staticDir))
	}
	log.Printf("listening on %s", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal(err)
	}
}

// spaHandler serves files from dir, falling back to index.html so that
// client-side routes like /top and /bottom work.
func spaHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if _, err := os.Stat(filepath.Join(dir, filepath.Clean(r.URL.Path))); err != nil {
				http.ServeFile(w, r, filepath.Join(dir, "index.html"))
				return
			}
		}
		files.ServeHTTP(w, r)
	})
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
		startOfWeek := time.Now()
		for startOfWeek.Weekday() != time.Monday {
			startOfWeek = startOfWeek.AddDate(0, 0, -1)
		}
		startOfWeek = startOfWeek.AddDate(0, 0, -1)

		events, err := s.gcalClient.Events(ctx, cal.id, startOfWeek, startOfWeek.AddDate(0, 0, 37))
		if err != nil {
			log.Printf("fetching events for calendar %s: %v", cal.id, err)
			continue
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonData)
}

func (s *server) getDepartures(w http.ResponseWriter, r *http.Request) {
	type departure struct {
		Route       string // short name, empty for LIRR branches
		RouteName   string
		Color       string
		TextColor   string
		Destination string
		Time        time.Time // expected
		Scheduled   time.Time
		Delay       int // seconds
	}
	type boardJSON struct {
		Name            string
		Count           int
		ShowSchedule    bool
		ShowDestination bool
		Departures      []departure
	}
	jsonData := []boardJSON{}

	now := time.Now()
	for _, b := range s.boards {
		bj := boardJSON{
			Name:            b.name,
			Count:           b.count,
			ShowSchedule:    b.showSchedule,
			ShowDestination: b.showDestination,
			Departures:      []departure{},
		}
		for _, d := range transit.Departures(b.feeds, b.filter, now) {
			bj.Departures = append(bj.Departures, departure{
				Route:       d.Route.ShortName,
				RouteName:   d.Route.LongName,
				Color:       "#" + d.Route.Color,
				TextColor:   "#" + d.Route.TextColor,
				Destination: d.Destination,
				Time:        d.Time,
				Scheduled:   d.Scheduled(),
				Delay:       int(d.Delay.Seconds()),
			})
		}
		jsonData = append(jsonData, bj)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jsonData)
}
