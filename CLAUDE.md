# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Aviation weather dashboard for a configurable list of NW-German airfields (default **EDWN**, Nordhorn-Lingen). Go backend fetches + processes forecast data, vanilla-JS frontend renders 4 synchronized Chart.js charts with aviation symbols (wind barbs, cloud symbols, weather icons).

## Build & Run

```bash
make dev                      # FLUGWETTER_DEV=1 go run . — frontend served from disk
go run .                      # frontend served from the embedded copy
make test                     # gofmt, go vet, go test, node --test
make hooks                    # install the pre-commit hook (runs the same checks)
# http://localhost:8080

make build                    # OCI image, tagged :<commit> and :latest
make run                      # podman run, with the healthcheck attached
make push
make deploy                   # build, push, restart on the server — see the deploy skill
```

Deploying has its own skill, `.claude/skills/deploy/SKILL.md`: the useful part is the checks either side of `make deploy`, and the fact that **the app runs on host port 8082** — 8080 on that host is an unrelated vhost that answers 200, so verifying against it looks like a successful check of a deployment that never happened.

Env: `OPENAIP_API_KEY` (openAIP overlay on the map picker), `FLUGWETTER_AIRPORTS_FILE` (replaces the embedded airport list), `FLUGWETTER_LOG_LEVEL` (`debug` traces every VFR scoring decision), `FLUGWETTER_DEV` (serve the frontend from disk).

**The binary is self-contained** and runs from any directory: the frontend, the airport list and the timezone database are all compiled in. There is no build step for the frontend — under `make dev` a reload picks up JS/CSS edits; without it the embedded copy is served and a rebuild is needed.

## Layout

```
main.go                     entrypoint; -healthcheck probes a running instance
internal/server/            HTTP surface, weather processing, caches, VFR scoring
internal/web/               embeds and serves the frontend
internal/web/frontend/      index.html, styles.css, js/, icons/, vendor/
internal/web/jstest/        frontend tests, deliberately outside the embedded tree
```

`internal/web` is its own package because a `go:embed` pattern cannot leave its own package directory — the assets must live under whichever package embeds them. The JS tests sit outside `frontend/` for the same reason: `//go:embed all:frontend` takes everything, and it was compiling the test files into the binary and serving them.

## Architecture

**`internal/server/server.go`** — stdlib `http.ServeMux` (`/`, `/api/config`, `/api/weather`, `/api/status`, tile proxy, `/static/*`), logging/gzip/security middleware, and all wire-format structs (`ProcessedWeatherData` and children). Adding a field to the API means editing these structs plus the producer in `weather.go` and the consumer in `js/api.js`.

**`internal/server/airports.go` + `airports.json`** — the selectable airfields, embedded via `go:embed`. `Airport` carries true runway headings (both ends) and `crosswindComponent`. Display order is computed at startup (pinned entry first, then latitude descending), so file order is irrelevant. A malformed list is fatal at startup. See `design-airport-selection.md` for the decisions.

- `opening_hours` is **free text, copied verbatim from the AIP's `TIME` block** — UTC, with `SS+30`, `SR-30`, `ECET`, `LDG`, `O/T PPR` and the bracketed-summer notation intact. Nothing parses it. Parsing it into a schedule meant discarding exactly the parts that matter to draw a rectangle asserting an hour is open; the published line says what the AIP says and lets the reader judge it. `opening_hours_source` carries the AD page and its date because the AIP moves on a 28-day AIRAC cycle and this file does not.
- **Adding an airfield has its own skill**: `.claude/skills/add-airport/SKILL.md`. It exists because two things about the research fail silently — runway headings must be TRUE, from OSM geometry rather than the magnetic designators, and the AIP's aerodrome pages are base64 PNGs that no scraper can read.

**`internal/server/tiles.go`** — openAIP raster tile proxy, keeping `OPENAIP_API_KEY` server-side and caching tiles (2000 entries, 24h) because openAIP rate-limits and asks clients to cache. Without the key the route is not registered and the map falls back to OSM alone.

**`internal/server/weather.go`** — everything else: API URL construction, fetching, caching, processing. Everything is per-airport: `GetWeatherData` and `processWeatherData` take an `Airport`, and `cache` holds one entry per identifier. Only the default airport is warmed at startup.

- Two upstream APIs: Open-Meteo (`models=icon_seamless`, `timezone=GMT`, `wind_speed_unit=kn`, hourly + 18 pressure levels) and sunrise-sunset.org (daylight/civil twilight).
- Two independent caches: `cache` (whole processed payload, `sync.RWMutex`, double-checked in `fetchAndCacheWeatherData`, warmed on startup) and `sunriseCache` (per lat/lon/date, unbounded, no TTL — daylight data is static per day).
- **What refreshes the forecast is a new model run, not a timer** — see `modelruns.go`. `cacheDuration` is now a 1 h **backstop** for when run detection is unavailable, not the schedule; changing it changes how long a forecast may quietly age in that failure mode, nothing else.
- `processWeatherData` walks the hourly time series once and fans out into temperature / cloud / wind / VFR points. Pressure-level data is converted to altitude via geopotential height (`m * 3.28084`), so cloud and wind layer heights come from the model, not fixed altitudes.
- `getCloudBase` = lowest layer with coverage ≥ 40%, returned **as a flight level** (feet/100). Downstream code, including the scoring, treats it as FL — easy to misread as feet.
- Scoring lives in `internal/server/vfr.go`, not here; `processWeatherData` only assembles a `conditions` from the hour and calls `scoreVFR`. Parsing the timestamp is the caller's job, and an unparseable one leaves the hour at `-1` rather than scoring it against a zero time.
- `Airport.crosswindComponent` (in `airports.go`) computes `|speed × sin(dir − runwayHeading)|`, taking the **minimum** over that airport's `runway_headings` so a multi-runway field reports the crosswind after picking the best runway. Those headings are **true**, taken from OpenStreetMap runway geometry rather than converted from the published (magnetic, 10°-rounded) designators — EDWN's 05/23 is true 55/235, not 50/230. Since crosswind and total wind are both penalized, the total-wind curve is deliberately the gentler of the two.

**`internal/server/vfr.go`** — all VFR scoring. `vfrLimits` is a table of factors (cloud base, visibility, wind, crosswind, crosswind gust spread, precipitation amount + probability, temperature, daylight); each carries an extractor and a curve of anchors, and **every limit and penalty in the application is in that one table**. Retuning the score means editing numbers there and nothing else. See `design-vfr-scoring.md`.

**Nothing outside `vfrLimits` should restate a threshold or a cost** — including this file. Those numbers are retuned regularly; anything that copies them is wrong by the next commit. Read them off the table.

- A curve is a list of `anchor{severity, at, cost}` running from `perfect` to worst, and the cost **ramps linearly between anchors** rather than stepping. That is the whole point: the stepped ladder this replaced charged a flat 5 points the moment a gust spread crossed a line, so a hair over the threshold scored the same as far past it.
- Whether a factor gets worse as its value rises or falls is **inferred** from the direction its thresholds run in, so the table cannot disagree with itself. `init()` validates every curve — monotone thresholds, increasing costs, `perfect` first and free, `noGo` last — and panics on a malformed one, the same way a malformed `airports.json` is fatal.
- A `noGo` anchor forces the whole hour to 0 and short-circuits the rest of the scoring. Which factors carry one is a property of the table, and the order of the table decides which reason a doomed hour reports.
- An anchor names the band *ending* at it, so a value is "difficult" from the moment it leaves the good anchor. A `noGo` names a wall rather than a band, so the last approach to it keeps the previous band's name.
- `scoreVFR` returns `100 - Σcost` clamped at 0, plus the `[]VfrPenalty` breakdown that explains it — worst first, only factors that cost something, and for a no-go hour exactly one entry: the reason. Costs are rounded per factor, not at the end, so the numbers in the tooltip add up to the score on screen. `-1` still means "not scored at all" (no daylight window), which the frontend renders as "no data" rather than bad weather.
- A factor with no `noGo` clamps at its last anchor instead of extrapolating, rather than escalating without bound the way the old ladder did.
- A factor may be **scaled** by a second quantity (`scaledBy`): its cost is multiplied by a weight that is itself an interpolated curve, so the shape is data and not a formula. Precipitation is the case it exists for — amount and probability are not independent, and no penalty *added* to the amount can express that. Three rules: missing scaling data means weight 1, never a discount; a scaled factor may not also carry a `noGo`, and `validate()` rejects one that does; and `evaluate` returns the cost unrounded so `scoreVFR` can scale and round exactly once.
- Weather codes are emitted as strings with a `-night` suffix outside sunrise..sunset (e.g. `"3-night"`), which is what the icon lookup keys on.

**`internal/server/modelruns.go`** — when the weather models behind `icon_seamless` last ran, and the poller that watches for the next one. DWD runs ICON-D2 and ICON-EU every 3 h and ICON global every 6, so refetching the forecast on a 15-minute clock discarded eleven of every twelve responses. Open-Meteo publishes each model's run times at `api.open-meteo.com/data/<model>/static/meta.json` — ~600 bytes against ~64 KB for the forecast — and polling that instead is what turns "refresh on a timer" into "refresh when there is something new".

- `last_run_availability_time` is the **trigger** (when a run reached the API); `last_run_initialisation_time` is what the **UI shows** (the model's reference hour). They differ by an hour or more, so using one for the other is wrong in both directions.
- A new run invalidates the whole cache, not one airport: runs are global. Only the default airport is re-warmed, for the same reason it is the only one warmed at startup.
- These URLs are **not part of the documented v1 surface**. Everything here degrades rather than fails: a bad poll keeps the last known runs, leaves the cache alone, and lets the backstop TTL do what the clock used to. Two consecutive total failures raise `model_runs_degraded`, which the frontend shows — a fallback nobody can see is one that runs for months unnoticed.
- Comparisons are for **inequality**, not ordering. A run time that appears to move backwards means something upstream changed, and refetching is the safe answer.

**`internal/web/frontend/js/`** — thirteen native ES modules, no bundler and no build step. `main.js` bootstraps; `charts.js` owns the four instances behind a mutable `charts` registry (`charts.vfr`, `.temperature`, `.cloud`, `.wind`) that `api.js` and `panzoom.js` read; `plugins.js` holds every drawing plugin and is imported for its registration side effect before any chart is constructed.

- **Two dependency rules.** `airports.js` must not import `api.js` — selecting an airport triggers a reload and the loader reads the picker's state, so `main.js` passes the reload in rather than letting the two import each other. And `viewport.js`, `barbs.js`, `time.js` and `vfr-penalties.js` must not import Chart.js or touch the DOM at load time, because that is what lets `internal/web/jstest/` run them under `node --test`; the `responsiveAxes` plugin lives in `plugins.js` for exactly this reason.
- The VFR chart's tooltip is the score's breakdown: `vfr-penalties.js` formats the `penalties` array the backend sends per hour into one line per factor. Returning an array from a Chart.js `label` callback is what puts each on its own line.

- All aviation rendering lives in custom Chart.js plugins in `plugins.js`: `vfrText` (colored probability + weather icon), `cloudSymbols` (transparency = coverage %), `windBarbs` (calm circle / half barb 5kt / full barb 10kt / pennant 50kt, pointing FROM the wind), `midnightDateLabels`, plus `cloudGridLines` (2000ft) and `windGridLines` (10kt) reference lines. Chart.js draws no symbols itself — changing symbol behaviour means editing plugin `afterDatasetsDraw` hooks, not dataset config.
- Wind chart datasets are positional — `[0]` wind layers (barbs), `[1]` speed 10m, `[2]` gusts 10m, `[3]` crosswind 10m, `[4]` crosswind gusts 10m — and `updateCharts` in `api.js` assigns to those indices directly. Inserting a dataset means renumbering the assignments. Speed/gusts are `#e17055`, crosswind `#ff8c00`; gust variants are the dashed member of each pair.
- Pan/zoom is **not** chartjs-plugin-zoom: `addManualPanZoom` attaches raw mouse/wheel/touch listeners, and `syncAllCharts`/`syncManualPan` push the resulting time range to the other three charts. Any new chart must be added to the sync list or it will drift.
- Touch gestures are split by axis, and it takes both halves: `touch-action: pan-y` on the canvas (`styles.css`) gives vertical swipes back to the browser, and `addManualPanZoom` locks each gesture to one axis after `TOUCH_AXIS_LOCK_PX` so a diagonal swipe does not scroll the page and pan time at once. Without the CSS the charts swallow the only way to scroll a phone; without the lock, diagonals do both. `preventDefault` is guarded by `e.cancelable` — once the browser owns the scroll the event is not cancelable — and `touchcancel` must reset the same state as `touchend`, because a gesture the browser takes over never reaches `touchend`.
- Chart.js, the date-fns adapter and Leaflet are **vendored** in `frontend/vendor/` and served same-origin — not from a CDN. That is what makes the page work on a captive portal and what makes the strict CSP possible; see `vendor/README.md` for versions and how to update. Loading the dashboard makes no third-party request at all; the OpenStreetMap tiles are the only cross-origin traffic, and only once the map modal is opened.
- **`index.html` is a Go template**, rendered by `internal/web`. `{{asset "js/main.js"}}` stamps a content hash onto each URL and `{{.ImportMap}}` emits the generated import map. Hashing the entry point alone would not work: a module's own `import './api.js'` resolves against the module's URL and does not inherit the query string, so every inner import would be unversioned. The import map remaps the resolved URLs, which keeps hashes out of the module sources entirely.
- The inline import map carries a per-response CSP nonce, and it is the only inline script on the page — the time-range buttons use `data-hours` and `addEventListener` in `main.js`, not `onclick=`. If the nonce in the header and the one in the page ever disagree, the browser drops the import map and nothing loads, which is why `indexHandler` produces both together.
- **All four charts share one plot area, and it is load-bearing.** Chart.js sizes a y axis to its own tick labels, which put the four time axes up to 72px out of step. Every visible y scale therefore pins its width via `afterFit: pinAxisWidth('left'|'right')`, and the VFR chart — which has no visible axis — reserves the same space through `layout.padding`. A new chart must do the same or it will not line up.
- Equal plot areas are necessary but not sufficient: the shared `xAxisConfig` also pins `offset: false` on the time scale itself. The temperature chart is the only one with a bar dataset (precipitation), and Chart.js's `BarController.overrides` turns `offset` on for the index scale, which makes `TimeScale.initOffsets()` reserve half a slot at each end — the same timestamp then lands 26px further right and 5% closer together than on the other three. `grid.offset` is a different option and does not prevent this.
- Axis widths, the VFR icon/label size and the axis titles all switch at `NARROW_VIEWPORT` (600px): `82/81` → `54/52`, `36px/24px` → `20px/14px`, titles hidden. On a 360px phone the wide axes left a 157px plot, too narrow for even one hour. The switch is applied by the `responsiveAxes` plugin's `beforeLayout`, so a resize across the breakpoint re-pins everything with no resize handler; both widths must come from the same `axisWidths()` call or the charts drift apart again.
- The initial time range is computed, not fixed: `initialZoomHours()` divides the laid-out plot width by `vfrSlotWidth()` (icon box vs the widest probability label, measured with `measureText` in the plugin's own font) and subtracts the 3 history hours `resetChartZoom` adds. `applyInitialZoomOnce()` guards it — airport switches and the 15-minute refresh must not reset the user's zoom.
- The refresh is a 5-minute poll of `/api/status` plus a `visibilitychange` handler, and it reloads the forecast only when the model run differs from the one on screen. A background tab's timers are throttled and a sleeping phone's do not run at all, so the interval alone could leave a stale forecast on screen at exactly the moment it is looked at and trusted; the visibility check is now unconditional because asking costs ~120 bytes.
- The night shading is a `backgroundBands` plugin drawing on **`beforeDatasetsDraw`** — behind the data, not over it — with positions from `scales.x` so it pans and zooms with everything else. The intervals come from the backend bounded by civil twilight, the same boundary `scoreVFR` zeroes an hour against, so the grey band and the zero score cannot disagree. `bands.js` holds the intervals and the clipping and must stay free of the DOM.
- `status.js` holds the reload decision and must stay free of the DOM, like `viewport.js` and `time.js`. Note `isKnownRun`: Go encodes a zero `time.Time` as `0001-01-01T00:00:00Z`, which is a *value*, not an absence — read naively it differs from whatever is on screen and reloads the forecast on every single poll. When either run is unknown the decision falls back to the clock.
- The `#error` box carries two independent conditions, rendered by `renderErrorArea`: a failed load (with Retry) and degraded run detection (without — retrying achieves nothing against a backend poller).
- Airport selection (`initAirportPicker` and below) is independent of the charts: it fetches `/api/config`, fills the dropdown, and drives a lazily-created Leaflet modal whose markers are the configured airfields only. Selection resolves `?airport=` → localStorage → backend default, and `loadWeatherData` appends the current identifier to the query.
- The Leaflet map needs `center`/`zoom` at construction: a layer's `addTo()` reads the map's pixel origin, which does not exist until the view is set, and `fitBounds` afterwards is too late. Wheel zoom is deliberately damped (`wheelPxPerZoomLevel: 200`, `zoomSnap: 0.25`) — the default 60px/level is two whole levels per mouse notch. Marker detail is opened on `mouseover`, not `bindPopup`, because click both selects the airport and closes the modal.

**`internal/web/frontend/styles.css`** — dark slate shell (`#0f172a`) with the white chart cards left light on purpose; the charts are the one thing that must stay maximally legible. Controls share one steel accent (`#4a6e9b`). `.chart-container` is otherwise untouched by the theme apart from a shadow tuned for the dark background.

**`internal/web/frontend/js/weather-icons.js`** — WMO code → SVG filename map (including `-night` variants), files in `internal/web/frontend/icons/` (Visual Crossing 2nd Set Color, LGPL). Unknown codes fall back to `notavailable.svg`.

## Units & conventions

Heights in feet, wind in knots, visibility converted to km, cloud base in flight levels. Y-axes: cloud chart 200–12,000 ft log, wind chart 20–10,000 ft log.

## Verification

`go test ./...` covers the scoring, the Open-Meteo decode path (a golden fixture in `internal/server/testdata/`, with a reflection test asserting every hourly field still binds — a renamed upstream field otherwise becomes a silent zero), both HTTP handlers, the caches and the tile proxy. `node --test 'internal/web/jstest/*.test.js'` covers the pure frontend logic. `make test` runs both; `make hooks` installs a pre-commit hook that does the same.

The scoring tests split in two on purpose. `TestFactorEvaluate_HitsItsAnchors` and `_RampsBetweenAnchors` walk `vfrLimits` itself, so a retune needs no test edits at all; `TestScoreVFR_Calibration` and `_NoGos` pin chosen values by hand and are meant to fail when the table moves. Keep the hand-written set small — one mid-band case per factor is enough — or a retune turns into a test rewrite.

Chart behaviour cannot be tested that way. Changes to the charts should be checked in a browser: all four plot areas aligned, pan and zoom syncing across all four, the map modal, and no CSP violations in the console.

## Docs

`design-airport-selection.md` records why airport selection works the way it does, `design-vfr-scoring.md` why the score is a table of curves. `README.md` and `internal/web/frontend/js/README.md` are current.
