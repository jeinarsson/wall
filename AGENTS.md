# wall

A family wall display: two stacked screens driven by one small Linux box
running a Go server and two Chromium kiosk windows.

- **Top screen** (`/top`): a large clock over slowly drifting contour lines,
  with train signs in the bottom-left corner: Manhattan-bound E/F subway from
  Forest Hills-71 Av and westbound LIRR from Forest Hills. To their right, a
  map of the tracks with those trains moving along them.
- **Bottom screen** (`/bottom`): a rolling month calendar of several Google
  Calendars.

## Layout

```
backend/            Go workspace (go.work)
  server/           main package: HTTP server, calendar config, train signs
    server.go       flags, routes, calendars, /api/calendarEvents
    trains.go       everything specific to where the wall hangs: feeds, stops, filters
    subway.go       subwaySign: countdown per route + filtered alerts -> /api/subway
    lirr.go         lirrSign: the TrainTime board (or timetable) -> /api/lirr
    trainmap.go     trainMap: map, tracks, stations -> /api/map; trains -> /api/map/trains
    lirrpositions.go  polls the GPS of the few LIRR trains on the map
  gcal/             thin Google Calendar client (service account)
  transit/          generic GTFS / GTFS-Realtime plumbing for the MTA feeds,
                    geometry (geo.go) and how trains move (motion.go)
  basemap/          OpenStreetMap roads, parks and water via the Overpass API
frontend/           React + TypeScript + Vite
  src/Top.tsx       clock screen; places <Subway>, <Lirr> and <TrainMap>
  src/Bottom.tsx    calendar screen (Month.tsx, CustomRbc/ for react-big-calendar)
  src/Subway.tsx, Lirr.tsx, Sign.tsx, transit.ts, transit.css   train signs
  src/TrainMap.tsx  the map, drawn as SVG
  src/theme.css     shared palette and fonts
scripts/deploy.sh   git pull, build frontend and backend into bin/wall
scripts/run.sh      run server (with restart), wifi watchdog, both kiosk windows
```

The server serves the built frontend from `frontend/dist` and falls back to
`index.html` for client-side routes.

## Running locally

```sh
(cd frontend && npm ci && npm run build)
cd backend/server && go run . -key ../wall-gcloud.key -static ../../frontend/dist -cache /tmp/wall-cache
# open http://localhost:9000/top
```

For frontend work, run the server as above and `npm run dev` in `frontend/`;
Vite proxies `/api` to `localhost:9000`.

Secrets and generated files are not in git: `backend/wall-gcloud.key`
(Google service account key), `bin/`, `cache/`, `frontend/dist`.

## Tests and checks

```sh
cd backend && go vet ./server ./transit ./basemap && go test ./server ./transit ./basemap
cd frontend && npm run build && npx eslint src/
```

`Month.tsx` has a pre-existing lint error (`@ts-ignore`).

## Train signs

Data comes from the MTA's public feeds; no API key is needed.

- **Static data** (GTFS zips, the subway `Stations.csv` with boroughs) is
  downloaded into the cache dir (`-cache`, default `cache/`) by
  `transit.File`, loaded at startup and re-checked daily with
  `If-Modified-Since`. The first start downloads ~8 MB, which takes minutes
  on the kiosk's wifi; signs stay empty until it's done.
- **Realtime trips and alerts** are polled every minute by `transit.Live`
  and dropped if older than 10 minutes. The frontend polls the server every
  10 s and computes countdowns itself, which stay at 0 while a train is
  late. Trains stay on the signs until they leave; the backend decides
  when that is, the frontend doesn't drop them by their times.

Design rule: keep the core dumb and explicit, and convenience on the
outside. `transit` knows nothing about Forest Hills; its functions take
explicit stops, routes, directions and filters (`StopTimes`,
`Timetable.Departures`, `AlertFilter`). The signs turn explicit config into
JSON via pure `board` functions, which is what the tests exercise. All
location-specific choices live in `server/trains.go` and the props in
`Top.tsx`, so adapting the wall to another station should only touch those.

MTA data quirks worth knowing:

- On Queens Blvd, platform `G08S` is Manhattan-bound and `G08N` is
  Jamaica-bound. Subway trip updates don't set `direction_id`; the
  direction is the N/S suffix of the stop id.
- E and F are in different feeds (`gtfs-ace`, `gtfs-bdfm`).
- Subway alerts name stations without N/S and use `direction_id` 1 for
  south. Live incidents ("Delays") often have no stops or direction, only
  text, hence the text rules in `AlertFilter`.
- The LIRR sign shows the station's board in the TrainTime app
  (`/arrivals/FHL`, see below; polled every 13 s): scheduled times, `otp`
  (seconds early, so negative when late), `stop_status` (`EN_ROUTE`,
  `ARRIVING`, `BERTHED`), held and canceled. It lists trains until they
  leave, 15-45 s late, and never says `DEPARTED`.
- Without the board, the LIRR sign falls back to the static timetable
  (`stop_times.txt`), with GTFS-RT predictions and "Cancellations" alerts
  (which name trip ids) on top, as the GTFS-RT feed drops canceled trains
  instead of marking them. Its predictions lag by a minute or more. LIRR
  `direction_id` 1 is westbound. GTFS times can exceed 24:00 and belong to
  the previous service date.
- Feeds keep trips for a while after they leave; always filter out past
  times.
- While a subway train stands at a platform, the feed lists that stop with
  the time of the train's latest report, a few seconds in the past. The
  stop drops off 30-45 s after the last report, about when it leaves.
- Subway trips are listed before they start, with vehicle timestamps in
  the future; a trip is under way once its timestamp has passed. Most trip
  ids end with their shape (`089900_E..S69R`), but some only with route and
  direction (`121550_F..S`).
- The subway vehicle status is always `STOPPED_AT`, meaning "at or last
  passed" that stop; its timestamp is the latest report, not the arrival.
  The report in which the stop changes is the arrival, and arrivals are the
  only reliable events (seen 5-18 s after they happen). Predictions are
  revised by up to a minute at every update. A stop drops off the trip
  about 30 s after a report, which says nothing about departure. The ACE
  feed is updated every 6-9 s, BDFM every ~15 s.
- LIRR GTFS-RT vehicle positions are snapped to stations and often stale.
  Real GPS (fixes every 2-6 s, with speed, heading and cars) comes from the
  TrainTime app's backend, `backend-unified.mylirr.org` with header
  `Accept-Version: 3.0`; it is undocumented. `/locations` lists all trains
  (~14 KB gzipped), `/locations/{train_id}` is one (~1 KB). Its stop codes
  ("FHL") are the GTFS `stop_code`.

## Train map

The map shows the trains that the signs list, while they are on their way
through the area around the station.

- **View and data**: `trainMap.view` in `server/trains.go` is the part of
  the map shown (center, width and height in meters, rotation); it also
  decides which trains and tracks are sent and the area of the basemap.
  Tracks and stations come from the GTFS shapes and stops; the basemap is
  fetched from the Overpass API by `basemap` once a month, cached under a
  name from the query (so changing the view fetches it anew; the first
  fetch takes a minute). `/api/map` has all of this and changes daily;
  `/api/map/trains` has the trains, and is polled every 5 s.
- **LIRR** trains are where their GPS says, then go on at their speed until
  their next stop, for at most 20 s (`transit.DeadReckon`). The list of
  trains is fetched every minute, and the trains on (or nearing) the map
  one by one every 7 s. Over 17 s this is off by 12 m median, 66 m p90.
- **Subway** trains stand at the stop they last arrived at, run to the next
  at an average speed, and wait there until the feed says they got there
  (`transit.SubwayModel`, with `transit.Arrivals` remembering arrivals).
  They never move backwards, and are at most a stop ahead of the data.
- Each train comes with a path and knots (time, meters along the path);
  the frontend moves it along them at 10 fps, the same way as
  `transit.PositionAt`, and glides to where new data puts it.

## Calendar

Calendars are configured in `server/server.go` (id, color, optional
include/exclude regex on the summary). `/api/calendarEvents` returns events
from the start of the week for 37 days; calendars that fail to load are
skipped.

## Deploying

The kiosk is reachable as `ssh wall@wall.local` (the corresponding key is
available in `~/.ssh/`); the repo is in `~/git/wall`. There, run
`scripts/deploy.sh`, then restart `scripts/run.sh`. Go is not on the `PATH`
of non-interactive SSH sessions there, so run `deploy.sh` from a login shell.

## Conventions

- Match the surrounding style: short doc comments on types and non-obvious
  functions, plain structs for config, no frameworks beyond what's there.
- UI is minimal and discreet: near-black background, muted greys, amber
  accent; color only for route colors, delays and alerts.
- Commit messages: imperative subject, a short body explaining why.
