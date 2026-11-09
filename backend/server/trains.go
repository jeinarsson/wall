package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"time"
	_ "time/tzdata" // for the LIRR timetable, wherever this runs

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

	// GPS positions of LIRR trains, from the TrainTime app's backend.
	lirrLocations = "https://backend-unified.mylirr.org/locations?geometry=NONE&railroad=LIRR"
)

// minorAlerts are the alert types that don't say anything about whether
// the trains are running, such as station entrances being closed.
var minorAlerts = []string{"Station Notice", "Special Notice", "Boarding Change", "Extra Service", "Special Schedule"}

// severeAlerts matches the alert types shown in red rather than amber.
var severeAlerts = regexp.MustCompile(`Suspend|Cancel|Severe`)

// positionInterval is how often the trains on the map are refreshed. Not
// a divisor of a minute, so that the polls don't keep hitting the feeds at
// the same point in their own update cycles.
const positionInterval = 17 * time.Second

type poller interface {
	Poll(ctx context.Context, interval time.Duration)
}

type trains struct {
	subway    *subwaySign
	lirr      *lirrSign
	trainMap  *trainMap
	static    []poller // refreshed daily
	live      []poller // refreshed every minute
	positions []poller // refreshed every positionInterval, for the map
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
	const forestHillsLIRR = "55"

	subwayStatic := transit.NewFile(staticSubway, cached("gtfs_subway.zip"), transit.FromZip(loadSubwayStatic([]string{"E", "F"})))
	stations := transit.NewFile(subwayStations, cached("stations.csv"), transit.FromCSV(transit.LoadStations))
	lirrStatic := transit.NewFile(staticLIRR, cached("gtfs_lirr.zip"), transit.FromZip(loadLIRRStatic(forestHillsLIRR, nyc)))
	ace := transit.NewTripFeed(feedACE)
	bdfm := transit.NewTripFeed(feedBDFM)
	lirrTrips := transit.NewTripFeed(feedLIRR)
	subwayAlertFeed := transit.NewAlertFeed(subwayAlerts)
	lirrAlertFeed := transit.NewAlertFeed(lirrAlerts)
	lirrTrains := transit.NewLocationFeed(lirrLocations)

	south, west := uint32(1), uint32(1)

	return &trains{
		static:    []poller{subwayStatic, stations, lirrStatic},
		live:      []poller{lirrTrips, subwayAlertFeed, lirrAlertFeed},
		positions: []poller{ace, bdfm, lirrTrains},

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
			directionID: west,
			ahead:       6 * time.Hour,
			alerts: transit.AlertFilter{
				ExcludeTypes: minorAlerts,
				Stops:        []string{forestHillsLIRR},
				DirectionID:  &west,
			},
			cancelAlertType: "Cancellations",

			trips:     lirrTrips,
			alertFeed: lirrAlertFeed,
			static:    lirrStatic,
		},

		// The same trains on their way to Forest Hills, from a bit west
		// of it to a few stops beyond Jamaica. The frontend shows part
		// of this area.
		trainMap: &trainMap{
			area:           transit.BBox{West: -73.880, South: 40.660, East: -73.735, North: 40.735},
			subwayRoutes:   []string{"E", "F"},
			subwayPlatform: "G08S",
			lirrStation:    forestHillsLIRR,
			lirrDirection:  "W",

			subwayTrips:  []*transit.TripFeed{ace, bdfm},
			subwayStatic: subwayStatic,
			lirrTrains:   lirrTrains,
			lirrStatic:   lirrStatic,
		},
	}, nil
}

func (t *trains) poll(ctx context.Context) {
	for _, p := range t.static {
		go p.Poll(ctx, 24*time.Hour)
	}
	for _, p := range t.live {
		go p.Poll(ctx, time.Minute)
	}
	for _, p := range t.positions {
		go p.Poll(ctx, positionInterval)
	}
}
