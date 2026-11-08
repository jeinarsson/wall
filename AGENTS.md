# wall

A family wall display: two stacked screens driven by one small Linux box
running a Go server and two Chromium kiosk windows.

- **Top screen** (`/top`): a large clock over slowly drifting contour lines,
  with train signs in the bottom-left corner: Manhattan-bound E/F subway from
  Forest Hills-71 Av and westbound LIRR from Forest Hills.
- **Bottom screen** (`/bottom`): a rolling month calendar of several Google
  Calendars.

## Layout

```
backend/            Go workspace (go.work)
  server/           main package: HTTP server, calendar config, train signs
    server.go       flags, routes, calendars, /api/calendarEvents
    trains.go       everything specific to where the wall hangs: feeds, stops, filters
    subway.go       subwaySign: countdown per route + filtered alerts -> /api/subway
    lirr.go         lirrSign: timetable merged with live data -> /api/lirr
  gcal/             thin Google Calendar client (service account)
  transit/          generic GTFS / GTFS-Realtime plumbing for the MTA feeds
frontend/           React + TypeScript + Vite
  src/Top.tsx       clock screen; places <Subway> and <Lirr>
  src/Bottom.tsx    calendar screen (Month.tsx, CustomRbc/ for react-big-calendar)
  src/Subway.tsx, Lirr.tsx, Sign.tsx, transit.ts, transit.css   train signs
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
cd backend && go vet ./server ./transit && go test ./server ./transit
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
  30 s and computes countdowns itself.

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
- The LIRR feed drops canceled trains instead of marking them, so the LIRR
  sign is built from the static timetable (`stop_times.txt`), with live
  predictions and "Cancellations" alerts (which name trip ids) on top. LIRR
  `direction_id` 1 is westbound. GTFS times can exceed 24:00 and belong to
  the previous service date.
- Feeds keep trips for a while after they leave; always filter out past
  times.

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
