# Design: airport selection

Status: implemented (2026-08-04). Replaces the hardcoded `EDWN` in `backend/weather.go`.

Goal: the dashboard serves a configurable list of airfields instead of one. Two ways to
pick one — a dropdown, and a button that opens an openAIP map of north-west Germany with
the configured fields as clickable markers.

## Decisions

### D1 — Airport list lives in `backend/airports.json`, embedded via `go:embed`

Alternatives were a Go slice literal (recompile to change) and a mandatory external file
(one more deployment artefact, breaks `go run .`).

Embedding gives a working default with no setup; the env var `FLUGWETTER_AIRPORTS_FILE`
points at an external JSON file that replaces the embedded one wholesale, so the container
image can be re-pointed without a rebuild. The file is validated at startup and the process
refuses to start on a bad list — a silently-empty airport list would render as a working UI
with no data.

Schema, one entry per airfield:

```json
{
  "identifier": "EDWN",
  "name": "Nordhorn-Lingen",
  "latitude": 52.4575,
  "longitude": 7.185,
  "runways": ["05/23"],
  "runway_headings": [50, 230],
  "pinned": true
}
```

- `identifier` — ICAO code where one exists, otherwise the openAIP/AIP short code. It is the
  API parameter and the localStorage value, so it must be stable and unique.
- `runway_headings` — **true** headings, both ends of every runway, in degrees, taken from
  OpenStreetMap runway *geometry* (see D8). `runways` carries the published designators for
  display only.
- `pinned` — sorts to the top of the dropdown regardless of latitude, and is the default
  airport. Exactly one entry is expected to carry it.

Latitude/longitude are JSON numbers, not the strings the old `Airport` struct used. The two
places that need strings (the Open-Meteo URL and the sunrise-sunset cache key) format them
via `LatString()` / `LonString()` at 4 decimals — that also keeps the sunrise cache key
stable, which a raw `%v` on a float would not.

### D2 — Ordering is computed, not taken from file order

The backend sorts: pinned first, then by latitude descending (north → south). Adding an
airfield to the JSON therefore means appending it anywhere in the file; it lands in the
right place on its own. EDWN appears exactly once — pinned at the top, not repeated in its
geographic position between Damme and EDDG.

### D3 — One config endpoint, not several

`GET /api/config` returns everything the frontend needs to boot:

```json
{
  "airports": [ ... in display order ... ],
  "default_airport": "EDWN",
  "openaip_overlay": true
}
```

`GET /api/weather?airport=EDWN` returns the existing `ProcessedWeatherData` shape,
unchanged. A missing `airport` parameter falls back to the default so old bookmarks and the
bare `/api/weather` still work; an unknown identifier is a 400 rather than a silent fallback,
because silently serving the wrong airfield's weather is a flight-safety problem the user
cannot see.

The map's viewport is *not* configured: the frontend fits the map to the bounding box of the
airports it received. A changed list moves the map by itself.

### D4 — Map is Leaflet + OSM base + openAIP overlay, with our own markers

openAIP's own web map cannot be embedded and clicked through — an iframe gives no access to
the click target. So the map is built here:

- base layer: OpenStreetMap raster tiles (attribution required, kept in the map corner),
- overlay: openAIP raster tiles, `https://api.tiles.openaip.net/api/data/openaip/{z}/{x}/{y}.png`,
  which draws airspaces, airfields, navaids and reporting points,
- markers: **only** the configured airfields, because those are the only ones the backend can
  serve weather for. Clicking anything else on the openAIP layer does nothing by design; the
  overlay is context, the markers are the control.

Leaflet 1.9.4 from jsDelivr, version-pinned like the existing Chart.js includes. No local
vendoring, consistent with the rest of the frontend.

### D5 — openAIP tiles are proxied through the backend

The openAIP Tiles API needs a free API key (`accounts.openaip.net` → API Clients). Putting it
in the page source would publish it. The backend therefore proxies:

`GET /api/tiles/openaip/{z}/{x}/{y}.png` → upstream with `apiKey` attached from the
`OPENAIP_API_KEY` env var.

openAIP rate-limits the tile service and explicitly asks clients to cache, so the proxy keeps
a bounded in-memory tile cache (2000 tiles, 24h TTL — aeronautical tiles change on AIRAC
cycles, not hourly). Tiles are small; the cap bounds the map at a few tens of MB.

Without `OPENAIP_API_KEY` the proxy is not registered, `openaip_overlay` is `false`, and the
map falls back to OSM alone. The picker still works — only the aeronautical context is
missing. This is the out-of-the-box state; it is a deliberate degradation, not a failure.

openAIP data is CC BY-NC 4.0 and requires attribution, which the map carries. Non-commercial
use only — fine for this project, worth knowing before anyone hosts it commercially.

### D6 — Per-airport caches

`cache` becomes a `map[identifier]*entry`, each entry keeping its own payload and timestamp
under one mutex, with the same 15-minute TTL and the same double-checked fetch. The
sunrise-sunset cache was already keyed by lat/lon/date and needed no change.

Only the default airport is warmed at startup. Warming all thirteen would fire thirteen very
large Open-Meteo requests before the first user arrives, for airfields nobody may look at.
The first request for any other field pays one fetch.

### D7 — Selection lives in the URL and localStorage

`?airport=EDLT` wins if present (shareable links), otherwise localStorage
(`flugwetter.airport`), otherwise the default. Selecting an airport updates all three plus
the header and `document.title`, without a page reload — the charts already have an update
path, so only the data is refetched.

An identifier in localStorage that is no longer in the config (airfield removed) falls back to
the default instead of erroring.

### D8 — Runway headings come from OSM geometry, not from the designators

The obvious route is designator → magnetic → true, applying ~3°E variation. It is worse than
it looks: designators are rounded to 10°, so the conversion inherits up to 5° of rounding
error before the variation is even added.

Instead the headings were computed from the endpoint coordinates of each
`aeroway=runway` way in OpenStreetMap (Overpass, great-circle bearing between the first and
last node). That bearing is *already true* — no variation term, no rounding. Spot-checking the
results against the published designators, every field agrees to within the expected few
degrees, which is what makes the geometry trustworthy in the first place.

This corrected EDWN, which the old hardcoded `Airport` had at 50/230: the paved 05/23 runs
true 55/235. The 5° difference is worth ~1.7kt of crosswind in a 20kt wind.

Where a field has a second runway in a different orientation (Wangerooge's grass 01/19,
Wilhelmshaven's 16/34) both are listed, so `crosswindComponent` can pick the better one — the
same "best runway" rule that already applied to the two ends of a single runway.

Airfield coordinates are the midpoint of the main runway, except EDWN which keeps the value it
has always used. All of them sit within ~150m of the published reference point, which is far
inside one grid cell of the weather model.

## Airfields

Sorted north → south, as returned by the API. EDWN is pinned to the top of the dropdown.

| Ident | Name | Lat | Lon | Runways |
| --- | --- | --- | --- | --- |
| EDWG | Wangerooge | 53.78256 | 7.91957 | 09/27, 01/19 |
| EDWY | Norderney | 53.70691 | 7.23006 | 08/26 |
| EDWJ | Juist | 53.68127 | 7.05651 | 07/25 |
| EDWI | Wilhelmshaven-Mariensiel | 53.50216 | 8.05224 | 02/20, 16/34 |
| EDWE | Emden | 53.39125 | 7.22732 | 07/25 |
| EDWF | Leer-Papenburg | 53.27187 | 7.44200 | 08/26 |
| EDWC | Damme | 52.48757 | 8.18510 | 10/28 |
| EDWN | Nordhorn-Lingen | 52.45750 | 7.18500 | 05/23 |
| EDDG | Münster/Osnabrück | 52.13439 | 7.68366 | 07/25 |
| EDLS | Stadtlohn-Vreden | 51.99558 | 6.84180 | 11/29 |
| EDLT | Münster-Telgte | 51.94451 | 7.77364 | 10/28 |
| EDLB | Borkenberge | 51.77980 | 7.28936 | 07/25 |
| EDLH | Hamm-Lippewiesen | 51.69071 | 7.81911 | 06/24 |

See `backend/airports.json` for the authoritative values including true headings.

## Consequences

- `calculateVFRProbability`, `processWeatherData` and the crosswind computation take an
  `Airport` argument; nothing about the scoring rules themselves changed.
- The Dockerfile needs no change — `airports.json` is embedded in the binary.
- `OPENAIP_API_KEY` is the only new operational knob, and is optional.
