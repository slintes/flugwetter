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
- `Airport.crosswindComponent` (in `airports.go`) computes `|speed × sin(dir − runwayHeading)|`, taking the **minimum** over that airport's `runway_headings` so a multi-runway field reports the crosswind after picking the best runway. Those headings are **true**, taken from OpenStreetMap runway geometry rather than converted from the published (magnetic, 10°-rounded) designators — EDWN's 05/23 is true 55/235, not 50/230. Since crosswind and total wind are both penalized, the total-wind multipliers are deliberately mild (`>10kt: −1×`, `>15kt: −1.5×`, `>20kt: −2×` speed).
- Weather codes are emitted as strings with a `-night` suffix outside sunrise..sunset (e.g. `"3-night"`), which is what the icon lookup keys on.

**`frontend/app.js`** (~1900 lines, no modules) — four globals `vfrChart` / `temperatureChart` / `cloudChart` / `windChart`, an init function per chart, `updateCharts(data)` for the API response, and hand-rolled pan/zoom.

- All aviation rendering lives in custom Chart.js plugins registered inline: `vfrText` (colored probability + weather icon), `cloudSymbols` (transparency = coverage %), `windBarbs` (calm circle / half barb 5kt / full barb 10kt / pennant 50kt, pointing FROM the wind), `midnightDateLabels`, plus `cloudGridLines` (2000ft) and `windGridLines` (10kt) reference lines. Chart.js draws no symbols itself — changing symbol behaviour means editing plugin `afterDatasetsDraw` hooks, not dataset config.
- Wind chart datasets are positional — `[0]` wind layers (barbs), `[1]` speed 10m, `[2]` gusts 10m, `[3]` crosswind 10m, `[4]` crosswind gusts 10m — and `updateCharts` assigns to those indices directly. Inserting a dataset means renumbering the assignments. Speed/gusts are `#e17055`, crosswind `#ff8c00`; gust variants are the dashed member of each pair.
- Pan/zoom is **not** chartjs-plugin-zoom: `addManualPanZoom` attaches raw mouse/wheel/touch listeners, and `syncAllCharts`/`syncManualPan` push the resulting time range to the other three charts. Any new chart must be added to the sync list or it will drift.
- Touch gestures are split by axis, and it takes both halves: `touch-action: pan-y` on the canvas (styles.css) gives vertical swipes back to the browser, and `addManualPanZoom` locks each gesture to one axis after `TOUCH_AXIS_LOCK_PX` so a diagonal swipe does not scroll the page and pan time at once. Without the CSS the charts swallow the only way to scroll a phone; without the lock, diagonals do both. `preventDefault` is guarded by `e.cancelable` — once the browser owns the scroll the event is not cancelable — and `touchcancel` must reset the same state as `touchend`, because a gesture the browser takes over never reaches `touchend`.
- Chart.js, the date-fns adapter and Leaflet load from jsDelivr CDN in `index.html`, version-pinned; there is no local vendoring or offline fallback.
- **All four charts share one plot area, and it is load-bearing.** Chart.js sizes a y axis to its own tick labels, which put the four time axes up to 72px out of step. Every visible y scale therefore pins its width via `afterFit: pinAxisWidth('left'|'right')`, and the VFR chart — which has no visible axis — reserves the same space through `layout.padding`. A new chart must do the same or it will not line up.
- Equal plot areas are necessary but not sufficient: the shared `xAxisConfig` also pins `offset: false` on the time scale itself. The temperature chart is the only one with a bar dataset (precipitation), and Chart.js's `BarController.overrides` turns `offset` on for the index scale, which makes `TimeScale.initOffsets()` reserve half a slot at each end — the same timestamp then lands 26px further right and 5% closer together than on the other three. `grid.offset` is a different option and does not prevent this.
- Axis widths, the VFR icon/label size and the axis titles all switch at `NARROW_VIEWPORT` (600px): `82/81` → `54/52`, `36px/24px` → `20px/14px`, titles hidden. On a 360px phone the wide axes left a 157px plot, too narrow for even one hour. The switch is applied by the `responsiveAxes` plugin's `beforeLayout`, so a resize across the breakpoint re-pins everything with no resize handler; both widths must come from the same `axisWidths()` call or the charts drift apart again.
- The initial time range is computed, not fixed: `initialZoomHours()` divides the laid-out plot width by `vfrSlotWidth()` (icon box vs the widest probability label, measured with `measureText` in the plugin's own font) and subtracts the 3 history hours `resetChartZoom` adds. It runs once, from `updateCharts`, guarded by `initialZoomApplied` — airport switches and the 15-minute refresh must not reset the user's zoom.
- Airport selection (`initAirportPicker` and below) is independent of the charts: it fetches `/api/config`, fills the dropdown, and drives a lazily-created Leaflet modal whose markers are the configured airfields only. Selection resolves `?airport=` → localStorage → backend default, and `loadWeatherData` appends the current identifier to the query.
- The Leaflet map needs `center`/`zoom` at construction: a layer's `addTo()` reads the map's pixel origin, which does not exist until the view is set, and `fitBounds` afterwards is too late. Wheel zoom is deliberately damped (`wheelPxPerZoomLevel: 200`, `zoomSnap: 0.25`) — the default 60px/level is two whole levels per mouse notch. Marker detail is opened on `mouseover`, not `bindPopup`, because click both selects the airport and closes the modal.

**`frontend/styles.css`** — dark slate shell (`#0f172a`) with the white chart cards left light on purpose; the charts are the one thing that must stay maximally legible. Controls share one steel accent (`#4a6e9b`). `.chart-container` is otherwise untouched by the theme apart from a shadow tuned for the dark background.

**`frontend/weather-icons.js`** — WMO code → SVG filename map (including `-night` variants), files in `frontend/icons/` (Visual Crossing 2nd Set Color, LGPL). Unknown codes fall back to `notavailable.svg`.

## Units & conventions

Heights in feet, wind in knots, visibility converted to km, cloud base in flight levels. Y-axes: cloud chart 200–12,000 ft log, wind chart 20–10,000 ft log.

## Docs

`design-airport-selection.md` is current and records why airport selection works the way it does.

`specs.md`, `README.md` and `frontend/README.md` are partly stale — they still describe a separate "surface wind" chart and omit the VFR chart. Trust the code; update these docs when touching the areas they describe.
