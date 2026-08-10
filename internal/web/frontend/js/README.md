# Frontend modules

Native ES modules. No bundler, no build step, no dependencies — `index.html` loads
`main.js` as `type="module"` and the browser does the rest.

| Module | Responsibility |
|---|---|
| `main.js` | Bootstrap. The only file that wires anything together. |
| `api.js` | Fetching the forecast, mapping the payload into chart points, auto-refresh. |
| `charts.js` | The shared time-axis config, the four chart constructors, the `charts` registry. |
| `plugins.js` | Every custom Chart.js plugin, plus `drawWindBarb` and the `timeNearest` interaction mode. |
| `panzoom.js` | Hand-rolled pan/zoom, cross-chart sync, the initial range. |
| `airports.js` | The picker, the shareable `?airport=` URL, the Leaflet map. |
| `viewport.js` | Breakpoints, axis widths, VFR metrics, display density. |
| `barbs.js` | Wind barb arithmetic, separated so it is testable without a canvas. |
| `time.js` | `toEpochMs` — the UTC parsing every series depends on. |
| `status.js` | Whether a new model run means the forecast on screen is out of date. |
| `bands.js` | The shaded bands behind the charts — night, ED-R activity, daytime — and clipping them to what is on screen. |
| `restrictions.js` | The airspace use plan: when restricted areas are active, for the charts and the map. |
| `vfr-penalties.js` | Formats the VFR score's breakdown for the tooltip: what the hour lost, and to what. |
| `weather-icons.js` | WMO code → icon filename, including the `-night` variants. |

## Two dependency rules

**`airports.js` must not import `api.js`.** Selecting an airport triggers a reload and the
loader reads the picker's state; having each import the other would make them mutually
dependent. `main.js` passes the reload in instead.

**`viewport.js`, `barbs.js`, `time.js`, `vfr-penalties.js`, `status.js`, `bands.js` and
`restrictions.js` must not import Chart.js or touch the DOM at load time.** That is what lets `internal/web/jstest/` run them under `node --test`. The
`responsiveAxes` plugin lives in `plugins.js` rather than `viewport.js` for exactly this
reason.

## What is easy to break

`CLAUDE.md` in the repository root documents the load-bearing constraints in full — shared
plot areas, `offset: false` on the time scale, the touch axis lock, the initial-zoom
calculation. They are not obvious from the code, and several were bugs first.

## Tests

```bash
node --test 'internal/web/jstest/*.test.js'
```

They live outside this directory because `//go:embed all:frontend` compiles everything here
into the binary — test files included, until they were moved.
