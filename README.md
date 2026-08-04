# Flugwetter ✈️

Aviation weather dashboard for a configurable list of NW-German airfields, built around one
question: **can I fly from this airfield in the next few days?**

A Go backend fetches and scores forecast data; a vanilla-JS frontend draws four synchronised
charts with real aviation symbols — wind barbs, cloud symbols, weather icons.

## The charts

All four share one time axis and pan and zoom together.

| Chart | Shows |
|---|---|
| **VFR probability** | A 0–100 score per hour with a weather icon. The headline. |
| **Temperature** | Temperature, dew point, and precipitation with probability-scaled bars. |
| **Clouds & visibility** | Cloud layers by height with coverage, cloud base as a flight level, visibility in km. |
| **Wind** | Wind barbs by altitude, plus 10m speed, gusts and the crosswind component. |

The VFR score starts at 100 and subtracts penalties for cloud base, visibility, total wind,
crosswind and its gusts, precipitation, and heat. It returns 0 outside civil twilight, below
a 1000ft ceiling, or under 5km visibility, and `-1` — rendered as "no data", not as bad
weather — when an hour cannot be scored at all. Every rule lives in one function on purpose;
see `internal/server/weather.go`.

Crosswind is computed against **true** runway headings taken from OpenStreetMap geometry
rather than the published magnetic designators, and a multi-runway field is scored on its
best runway.

## Running it

```bash
make dev            # frontend served from disk: edit CSS/JS and reload
go run .            # frontend served from the embedded copy
make test           # Go and frontend suites
make hooks          # install the pre-commit hook (recommended, once)
```

Then <http://localhost:8080>.

The binary is self-contained — the frontend, the airport list and the timezone database are
all compiled in — so it runs from any directory.

### Configuration

| Variable | Effect |
|---|---|
| `OPENAIP_API_KEY` | Enables the openAIP airspace overlay on the map picker. Without it the map falls back to OpenStreetMap alone. |
| `FLUGWETTER_AIRPORTS_FILE` | Replaces the built-in airport list. |
| `FLUGWETTER_LOG_LEVEL` | `debug` \| `info` \| `warn` \| `error`. `debug` traces every VFR scoring decision. |
| `FLUGWETTER_DEV` | Serve the frontend from disk instead of the embedded copy. |

## Layout

```
main.go                     entrypoint; -healthcheck probes a running instance
internal/server/            HTTP surface, weather processing, caches, VFR scoring
internal/web/               embeds and serves the frontend
internal/web/frontend/      the assets: index.html, styles.css, js/, icons/, vendor/
internal/web/jstest/        frontend tests, kept out of the embedded tree
```

`internal/web` is a separate package because a `go:embed` pattern cannot leave its own
package directory, so the assets have to live under whichever package embeds them. The JS
tests sit outside `frontend/` for the same reason: that directory is embedded wholesale.

## Data sources

- **[Open-Meteo](https://open-meteo.com/)** — hourly forecast, `icon_seamless`, 18 pressure
  levels. Cached 15 minutes per airport; when it is unreachable the last good payload is
  served, flagged `stale`, and the page says how old it is.
- **[sunrise-sunset.org](https://sunrise-sunset.org/)** — daylight and civil twilight, one
  lookup per date.
- **[OpenStreetMap](https://www.openstreetmap.org/)** and optionally
  **[openAIP](https://www.openaip.net/)** — map picker tiles. openAIP is proxied so the API
  key never reaches the browser.

Browser libraries are vendored rather than loaded from a CDN, so the dashboard itself makes
no third-party requests at all.

## Deployment

```bash
make build          # OCI image, tagged :<commit> and :latest
make push
make restart        # replays the pod on the server
make deploy         # all three, in order
```

The image is `scratch` plus a CA bundle: two files, ~10MB, running as uid 65532.
`/api/config` reports the commit it was built from, so what is deployed can be checked
rather than inferred from a tag that always says `latest`. Rolling back is re-tagging an
older commit as `latest`.

The healthcheck is attached at run time rather than baked into the image — an image
`HEALTHCHECK` is a Docker-schema field with no OCI equivalent, and podman drops it:

```bash
podman run --health-cmd '["/flugwetter","-healthcheck"]' ...
```

JSON array form matters. A bare string is run through `/bin/sh`, which a `scratch` image
does not have, so every probe would fail while the server ran perfectly.

## Documentation

- **`CLAUDE.md`** — architecture and the non-obvious constraints that are easy to break.
  Worth reading before changing the charts.
- **`design-airport-selection.md`** — why airport selection works the way it does.
- **`internal/web/frontend/vendor/README.md`** — vendored library versions and how to update.

## Licence

MIT — see `LICENSE`. Weather icons are Visual Crossing's 2nd Set (Color), LGPL; see
`internal/web/frontend/icons/LICENSE`.
