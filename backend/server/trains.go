package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"
	_ "time/tzdata" // for the LIRR timetable, wherever this runs

	"github.com/jeinarsson/wall/basemap"
	"github.com/jeinarsson/wall/transit"
)

// The trains shown on the wall: from Forest Hills to Manhattan. Everything
// specific to where the wall hangs is set up here.

const (
	// Static data, for names, colors and timetables.
	staticSubway   = "https://rrgtfsfeeds.s3.amazonaws.com/gtfs_subway.zip"
	staticLIRR     = "https://rrgtfsfeeds.s3.amazonaws.com/gtfslirr.zip"
	subwayStations = "http://web.mta.info/developers/data/nyct/subway/Stations.csv"

	// Realtime trips and alerts.
	feedACE      = "https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs-ace"
	feedBDFM     = "https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs-bdfm"
	feedLIRR     = "https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/lirr%2Fgtfs-lirr"
	subwayAlerts = "https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/camsys%2Fsubway-alerts.json"
	lirrAlerts   = "https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/camsys%2Flirr-alerts.json"

	// GPS positions of LIRR trains, from the TrainTime app's backend: all
	// of them, and one.
	lirrLocations = "https://backend-unified.mylirr.org/locations?geometry=NONE&railroad=LIRR"
	lirrLocation  = "https://backend-unified.mylirr.org/locations/%s?geometry=NONE"
	// The trains due at a station, as on its board in the app, by the
	// station's GTFS stop_code.
	lirrArrivals = "https://backend-unified.mylirr.org/arrivals/%s"

	// Streets, parks and water for the map, from OpenStreetMap: the main
	// Overpass API instance, then a mirror. Others are listed at
	// https://wiki.openstreetmap.org/wiki/Overpass_API#Public_Overpass_API_instances
	overpass       = "https://overpass-api.de/api/interpreter?data="
	overpassMirror = "https://overpass.private.coffee/api/interpreter?data="
)

// minorAlerts are the alert types that don't say anything about whether
// the trains are running, such as station entrances being closed.
var minorAlerts = []string{"Station Notice", "Special Notice", "Boarding Change", "Extra Service", "Special Schedule"}

// severeAlerts matches the alert types shown in red rather than amber.
var severeAlerts = regexp.MustCompile(`Suspend|Cancel|Severe`)

// How often the trains on the LIRR sign are refreshed. The TrainTime
// board lags the trains by 15-45 s.
const boardInterval = 13 * time.Second

// How often the trains on the map are refreshed: the subway feeds, which
// are updated every 7-15 s, and the GPS fixes of the LIRR trains on the
// map, which come every few seconds. Not divisors of a minute, so that the
// polls don't keep hitting the feeds at the same point in their own update
// cycles.
const (
	positionInterval = 17 * time.Second
	gpsInterval      = 7 * time.Second
)

type poller interface {
	Poll(ctx context.Context, interval time.Duration)
}

type trains struct {
	subway    *subwaySign
	lirr      *lirrSign
	trainMap  *trainMap
	static    []poller // refreshed daily
	basemaps  []poller // refreshed monthly
	live      []poller // refreshed every minute
	positions []poller // refreshed every positionInterval, for the map
	gps       []poller // refreshed every gpsInterval, for the map
	boards    []poller // refreshed every boardInterval, for the signs
}

func newTrains(cacheDir string) (*trains, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}
	nyc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return nil, err
	}
	cached := func(name string) string { return filepath.Join(cacheDir, name) }
	const forestHillsLIRR, forestHillsCode = "55", "FHL"

	subwayStatic := transit.NewFile(staticSubway, cached("gtfs_subway.zip"), transit.FromZip(loadSubwayStatic([]string{"E", "F"})))
	stations := transit.NewFile(subwayStations, cached("stations.csv"), transit.FromFile(transit.LoadStations))
	lirrStatic := transit.NewFile(staticLIRR, cached("gtfs_lirr.zip"), transit.FromZip(loadLIRRStatic(forestHillsLIRR, nyc)))
	ace := transit.NewTripFeed(feedACE)
	bdfm := transit.NewTripFeed(feedBDFM)
	lirrTrips := transit.NewTripFeed(feedLIRR)
	subwayAlertFeed := transit.NewAlertFeed(subwayAlerts)
	lirrAlertFeed := transit.NewAlertFeed(lirrAlerts)
	lirrList := transit.NewLocationFeed(lirrLocations)
	lirrBoard := transit.NewArrivalFeed(fmt.Sprintf(lirrArrivals, forestHillsCode))

	// The map shows Forest Hills a third from the left and about halfway
	// up, and the tracks from Jamaica to Locust Manor on the Atlantic
	// Branch, where most trains come from, and some of the Main Line.
	// Turned so that the tracks run across the screen.
	view := mapView{
		Center: transit.LatLon{Lat: 40.71255, Lon: -73.82166},
		Width:  12_500, Height: 3_600,
		Rotate: 30,
	}
	mapArea := view.bounds(500)
	b := view.bounds(1000) // for the edges, which fade out
	query := basemap.Query(b.South, b.West, b.North, b.East)
	// Named by the query, so that it is fetched again if the view changes.
	// Once it is, those for other views are removed.
	hash := sha256.Sum256([]byte(query))
	basemapFile := cached(fmt.Sprintf("basemap-%x.json", hash[:4]))
	streets := transit.NewFile(overpass+url.QueryEscape(query), basemapFile,
		transit.FromFile(func(r io.Reader) (*basemap.Layers, error) {
			layers, err := basemap.Load(r, 2)
			if err != nil {
				return nil, err
			}
			if old, err := filepath.Glob(cached("basemap-*.json")); err == nil {
				for _, f := range old {
					if f != basemapFile {
						os.Remove(f)
					}
				}
			}
			return &layers, nil
		})).
		WithMirrors(overpassMirror + url.QueryEscape(query)).
		WithHeader(http.Header{"User-Agent": {"wall (github.com/jeinarsson/wall)"}})

	south, west := uint32(1), uint32(1)

	// The same trains as on the signs, on their way to Forest Hills.
	tm := &trainMap{
		view:           view,
		area:           mapArea,
		subwayRoutes:   []string{"E", "F"},
		subwayPlatform: "G08S",
		subwayModel:    transit.SubwayModel{MinSpeed: 5, MaxSpeed: 20, Lost: 5 * time.Minute},
		lirrStation:    forestHillsLIRR,
		lirrDirection:  "W",
		lirrAhead:      20 * time.Second,
		lirrLost:       3 * time.Minute,

		subwayTrips:  []*transit.TripFeed{ace, bdfm},
		subwayStatic: subwayStatic,
		lirrStatic:   lirrStatic,
		basemap:      streets,
	}
	// Follow the LIRR trains on the map, and those that will be on it in
	// a few minutes, until they have left the station and the map.
	watchArea := view.bounds(5000)
	lirrTrains := &lirrPositions{
		list: lirrList,
		url:  func(id string) string { return fmt.Sprintf(lirrLocation, url.PathEscape(id)) },
		watch: func(t transit.TrainLocation) bool {
			static, ok := lirrStatic.Get()
			return ok && tm.watchLIRR(static, watchArea, t)
		},
	}
	tm.lirrTrains = lirrTrains

	return &trains{
		static:    []poller{subwayStatic, stations, lirrStatic},
		basemaps:  []poller{streets},
		live:      []poller{lirrTrips, subwayAlertFeed, lirrAlertFeed, lirrList},
		positions: []poller{ace, bdfm},
		gps:       []poller{lirrTrains},
		boards:    []poller{lirrBoard},

		// Manhattan-bound E and F from Forest Hills-71 Av. On Queens Blvd
		// "S" is towards Manhattan, and "N" towards Jamaica.
		subway: &subwaySign{
			platform: "G08S",
			routes:   []string{"E", "F"},
			alerts: transit.AlertFilter{
				ExcludeTypes: minorAlerts,
				Routes:       []string{"E", "F"},
				RouteWide:    true,
				DirectionID:  &south,
				// The ride in Queens; alertBoroughs adds Manhattan.
				Stops: []string{
					"G08",                      // Forest Hills-71 Av
					"G09", "G10", "G11", "G12", // 67 Av .. Grand Av-Newtown
					"G13", "G14", "G15", "G16", // Elmhurst Av .. Northern Blvd
					"G18", "G19", "G20", "G21", // 46 St .. Queens Plaza
					"F09", // Court Sq-23 St
					"B04", // 21 St-Queensbridge, for the F via 63 St
				},
				ExcludeText: regexp.MustCompile(`(?i)(jamaica|queens|179 st)[\w\s-]*-bound|\b(uptown|northbound)\b|\bin (brooklyn|the bronx)\b`),
				IncludeText: regexp.MustCompile(`(?i)(manhattan|world trade center)-bound|\b(downtown|southbound)\b|\bin (manhattan|queens)\b`),
			},
			alertBoroughs: []string{"M"},

			trips:     []*transit.TripFeed{ace, bdfm},
			alertFeed: subwayAlertFeed,
			static:    subwayStatic,
			stations:  stations,
		},

		// Westbound LIRR from Forest Hills, to Penn Station or Grand
		// Central.
		lirr: &lirrSign{
			stop:        forestHillsLIRR,
			direction:   "W",
			directionID: west,
			ahead:       6 * time.Hour,
			alerts: transit.AlertFilter{
				ExcludeTypes: minorAlerts,
				Stops:        []string{forestHillsLIRR},
				DirectionID:  &west,
			},
			cancelAlertType: "Cancellations",

			arrivals:  lirrBoard,
			trips:     lirrTrips,
			alertFeed: lirrAlertFeed,
			static:    lirrStatic,
		},

		trainMap: tm,
	}, nil
}

func (t *trains) poll(ctx context.Context) {
	for _, p := range t.static {
		go p.Poll(ctx, 24*time.Hour)
	}
	for _, p := range t.live {
		go p.Poll(ctx, time.Minute)
	}
	for _, p := range t.basemaps {
		go p.Poll(ctx, 30*24*time.Hour)
	}
	for _, p := range t.positions {
		go p.Poll(ctx, positionInterval)
	}
	for _, p := range t.gps {
		go p.Poll(ctx, gpsInterval)
	}
	for _, p := range t.boards {
		go p.Poll(ctx, boardInterval)
	}
}
