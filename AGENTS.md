# AGENTS.md

This file provides guidance to coding agents when working with code in this repository.
Claude Code reads it via the `@AGENTS.md` import in `CLAUDE.md`.

## Project

Aviation weather dashboard for a configurable list of NW-German airfields (default **EDWN**, Nordhorn-Lingen). Go backend fetches + processes forecast data, vanilla-JS frontend renders 4 synchronized Chart.js charts with aviation symbols (wind barbs, cloud symbols, weather icons).

## Build & Run

```bash
make dev                      # FLUGWETTER_DEV=1 go run . — frontend served from disk
go run .                      # frontend served from the embedded copy
make test                     # gofmt, go vet, go test, node --test
make hooks                    # install the pre-commit hook (runs the same checks)
# http://localhost:8080

make deploy                   # build, push, restart on the server — see the deploy skill
```

Deploying has its own skill, `.claude/skills/deploy/SKILL.md`: the useful part is the checks either side of `make deploy`, and the fact that **the app runs on host port 8082** — 8080 on that host is an unrelated vhost that answers 200, so verifying against it looks like a successful check of a deployment that never happened.

Env: `OPENAIP_API_KEY` (openAIP overlay on the map picker), `FLUGWETTER_AIRPORTS_FILE` (replaces the embedded airport list), `FLUGWETTER_LOG_LEVEL` (`debug` traces every VFR scoring decision), `FLUGWETTER_DEV` (serve the frontend from disk).

**The binary is self-contained** and runs from any directory: the frontend, the airport list and the timezone database are all compiled in. There is no build step for the frontend — under `make dev` a reload picks up JS/CSS edits; without it the embedded copy is served and a rebuild is needed.

## Layout

```
main.go                     entrypoint; -healthcheck probes a running instance
internal/server/            HTTP surface, weather processing, caches, VFR scoring
internal/web/                embeds and serves the frontend
internal/web/frontend/       index.html, styles.css, js/, icons/, vendor/
internal/web/jstest/         frontend tests, deliberately outside the embedded tree
```

`internal/web` is its own package because a `go:embed` pattern cannot leave its own package directory — the assets must live under whichever package embeds them. The JS tests sit outside `frontend/` for the same reason: `//go:embed all:frontend` takes everything, and it was compiling the test files into the binary and serving them.

## Architecture

See `internal/server/AGENTS.md` for the backend (server.go, airports.go, tiles.go, weather.go, modelruns.go, restrictions.go, vfr.go) and `internal/web/frontend/AGENTS.md` for the frontend (the JS modules, styles.css, weather-icons.js) — both load automatically for a session working under those directories (Claude Code via the same `@AGENTS.md` import pattern, in each directory's own `CLAUDE.md`).

## Units & conventions

Heights in feet, wind in knots, visibility converted to km, cloud base in flight levels. Y-axes: cloud chart 200–12,000 ft log, wind chart 20–10,000 ft log.

## Verification

`go test ./...` covers the scoring, the Open-Meteo decode path (a golden fixture in `internal/server/testdata/`, with a reflection test asserting every hourly field still binds — a renamed upstream field otherwise becomes a silent zero), the AUP parser (a trimmed real briefing in the same directory, carrying the duplicate-FIR listing, the malformed timestamp and the area with no windows on purpose), the handlers, the caches and the tile proxy. `node --test 'internal/web/jstest/*.test.js'` covers the pure frontend logic. `make test` runs both; `make hooks` installs a pre-commit hook that does the same.

The scoring tests split in two on purpose. `TestFactorEvaluate_HitsItsAnchors` and `_BetweenAnchorsTakesTheUpperSeverity` walk `vfrLimits` itself, so a retune needs no test edits at all; `TestScoreVFR_Calibration` and `_NoGos` pin chosen values by hand and are meant to fail when the table moves. Keep the hand-written set small — one mid-band case per factor is enough — or a retune turns into a test rewrite.

Chart behaviour cannot be tested that way. Changes to the charts should be checked in a browser: all four plot areas aligned, pan and zoom syncing across all four, the map modal, and no CSP violations in the console.

## Docs

`design-airport-selection.md` records why airport selection works the way it does, `design-vfr-scoring.md` why the score is a table of curves. `README.md` and `internal/web/frontend/js/README.md` are current.
