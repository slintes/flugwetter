# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Aviation weather dashboard for a configurable list of NW-German airfields (default **EDWN**, Nordhorn-Lingen). Go backend fetches + processes forecast data, vanilla-JS frontend renders 4 synchronized Chart.js charts with aviation symbols (wind barbs, cloud symbols, weather icons).

## Build & Run

```bash
cd backend && go run .        # must run from backend/ — see gotcha below
# http://localhost:8080

go vet ./...                  # run before declaring Go work done
go build -o flugwetter .

make build                    # podman build quay.io/slintes/flugwetter
make run                      # podman run -p 8080:8080
make push
```

Optional env: `OPENAIP_API_KEY` (enables the openAIP overlay on the map picker), `FLUGWETTER_AIRPORTS_FILE` (replaces the embedded airport list).

`go test ./...` covers VFR scoring, sunrise parsing, airport config and the tile proxy. No frontend build step — JS/CSS served as-is; browser reload picks up changes, backend restart only needed for Go changes.

**Gotcha:** `main.go` serves `../frontend/index.html` and `../frontend/` via relative paths, so the server only works when the CWD is `backend/`. The Dockerfile mirrors this (`WORKDIR /app/backend`).

## Architecture

**`backend/main.go`** — mux router (`/`, `/api/config`, `/api/weather`, tile proxy, `/static/*` → `../frontend/`), logging middleware, and all wire-format structs (`ProcessedWeatherData` and children). Adding a field to the API means editing these structs plus the producer in `weather.go` and the consumer in `app.js`.

**`backend/airports.go` + `backend/airports.json`** — the selectable airfields, embedded via `go:embed`. `Airport` carries true runway headings (both ends) and `crosswindComponent`. Display order is computed at startup (pinned entry first, then latitude descending), so file order is irrelevant. A malformed list is fatal at startup. See `design-airport-selection.md` for the decisions.

**`backend/tiles.go`** — openAIP raster tile proxy, keeping `OPENAIP_API_KEY` server-side and caching tiles (2000 entries, 24h) because openAIP rate-limits and asks clients to cache. Without the key the route is not registered and the map falls back to OSM alone.

**`backend/weather.go`** — everything else: API URL construction, fetching, caching, processing, VFR scoring. Everything is per-airport: `GetWeatherData`, `processWeatherData` and `calculateVFRProbability` all take an `Airport`, and `cache` holds one entry per identifier. Only the default airport is warmed at startup.

- Two upstream APIs: Open-Meteo (`models=icon_seamless`, `timezone=GMT`, `wind_speed_unit=kn`, hourly + 18 pressure levels) and sunrise-sunset.org (daylight/civil twilight).
- Two independent caches: `cache` (whole processed payload, 15 min TTL, `sync.RWMutex`, double-checked in `fetchAndCacheWeatherData`, warmed on startup in `main`) and `sunriseCache` (per lat/lon/date, unbounded, no TTL — daylight data is static per day).
- `processWeatherData` walks the hourly time series once and fans out into temperature / cloud / wind / VFR points. Pressure-level data is converted to altitude via geopotential height (`m * 3.28084`), so cloud and wind layer heights come from the model, not fixed altitudes.
- `getCloudBase` = lowest layer with coverage ≥ 40%, returned **as a flight level** (feet/100). Downstream code, including `calculateVFRProbability`, treats it as FL — easy to misread as feet.
- `calculateVFRProbability` starts at 100 and subtracts penalties (cloud base, visibility, total wind, crosswind + crosswind gusts, precipitation amount+probability, high temperature); returns `0` outside civil twilight, when cloud base < 1000ft, when visibility < 5km or when crosswind ≥ 20kt, and `-1` when visibility data is missing (frontend treats -1 as "no data", not "bad weather"). Every branch calls `debugProb` — turn on `DEBUG` in `main.go` to trace a score. All scoring lives in this one function by design; keep new rules inline rather than extracting helpers.
- `Airport.crosswindComponent` (in `airports.go`) computes `|speed × sin(dir − runwayHeading)|`, taking the **minimum** over `EDWN.RunwayHeadings` (`{50, 230}`, true degrees) so a multi-runway airport reports the crosswind after picking the best runway. Runway headings are treated as true; the ~2-3°E magnetic variation at EDWN is ignored (< 1kt error). Since crosswind and total wind are both penalized, the total-wind multipliers are deliberately mild (`>10kt: −1×`, `>15kt: −1.5×`, `>20kt: −2×` speed).
- Weather codes are emitted as strings with a `-night` suffix outside sunrise..sunset (e.g. `"3-night"`), which is what the icon lookup keys on.

**`frontend/app.js`** (~1400 lines, no modules) — four globals `vfrChart` / `temperatureChart` / `cloudChart` / `windChart`, an init function per chart, `updateCharts(data)` for the API response, and hand-rolled pan/zoom.

- All aviation rendering lives in custom Chart.js plugins registered inline: `vfrText` (colored probability + weather icon), `cloudSymbols` (transparency = coverage %), `windBarbs` (calm circle / half barb 5kt / full barb 10kt / pennant 50kt, pointing FROM the wind), `midnightDateLabels`, plus `cloudGridLines` (2000ft) and `windGridLines` (10kt) reference lines. Chart.js draws no symbols itself — changing symbol behaviour means editing plugin `afterDatasetsDraw` hooks, not dataset config.
- Wind chart datasets are positional — `[0]` wind layers (barbs), `[1]` speed 10m, `[2]` gusts 10m, `[3]` crosswind 10m, `[4]` crosswind gusts 10m — and `updateCharts` assigns to those indices directly. Inserting a dataset means renumbering the assignments. Speed/gusts are `#e17055`, crosswind `#ff8c00`; gust variants are the dashed member of each pair.
- Pan/zoom is **not** chartjs-plugin-zoom: `addManualPanZoom` attaches raw mouse/wheel/touch listeners, and `syncAllCharts`/`syncManualPan` push the resulting time range to the other three charts. Any new chart must be added to the sync list or it will drift.
- Chart.js, the date-fns adapter and Leaflet load from jsDelivr CDN in `index.html`, version-pinned; there is no local vendoring or offline fallback.
- Airport selection (`initAirportPicker` and below) is independent of the charts: it fetches `/api/config`, fills the dropdown, and drives a lazily-created Leaflet modal whose markers are the configured airfields only. Selection resolves `?airport=` → localStorage → backend default, and `loadWeatherData` appends the current identifier to the query.

**`frontend/weather-icons.js`** — WMO code → SVG filename map (including `-night` variants), files in `frontend/icons/` (Visual Crossing 2nd Set Color, LGPL). Unknown codes fall back to `notavailable.svg`.

## Units & conventions

Heights in feet, wind in knots, visibility converted to km, cloud base in flight levels. Y-axes: cloud chart 100–24,000 ft log, wind chart 600–12,000 ft log.

## Docs

`design-airport-selection.md` is current and records why airport selection works the way it does.

`specs.md`, `README.md` and `frontend/README.md` are partly stale — they still describe a separate "surface wind" chart and omit the VFR chart. Trust the code; update these docs when touching the areas they describe.
