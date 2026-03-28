# Flugwetter - Aviation Weather Application

## Overview
Aviation weather dashboard for flight planning. Displays weather forecasts as interactive, synchronized Chart.js charts with aviation-specific symbols (wind barbs, cloud symbols). Currently hardcoded to **EDDG** (Muenster/Osnabrueck Airport).

## Architecture
- **Backend**: Go 1.24+ HTTP server (`backend/`) — fetches from Open-Meteo API and sunrise-sunset.org, processes/caches data, serves JSON API + static files
- **Frontend**: Vanilla JS/HTML/CSS (`frontend/`) — 4 synchronized Chart.js charts (VFR probability, temperature/precipitation, clouds, wind) with custom plugins
- **No frontend build step** — files served directly by Go backend

## Key Files
- `backend/main.go` — HTTP router (gorilla/mux), data structures, handlers (`/api/weather`, `/`, `/static/*`)
- `backend/weather.go` — API client, 15-min cache with mutex, data processing, VFR probability calculation
- `frontend/app.js` — Chart initialization, custom plugins (wind barbs, cloud symbols, VFR overlay), pan/zoom sync
- `frontend/weather-icons.js` — WMO weather code to icon mapping
- `frontend/styles.css` — Responsive CSS Grid layout (2x2 desktop, single column mobile)
- `specs.md` — Detailed technical specification

## Build & Run

```bash
# Development
cd backend && go run .
# Server at http://localhost:8080

# Container build (uses Podman)
make build    # builds quay.io/slintes/flugwetter
make run      # runs on port 8080
make push     # pushes to registry
```

## Testing
No test suite exists yet. When adding tests:
- Backend: `cd backend && go test ./...`
- Run `go vet` before declaring done

## Key Technical Details
- **Units**: Heights in feet, wind in knots (aviation standard)
- **Cache**: 15-minute TTL, thread-safe (sync.RWMutex), pre-cached on startup
- **Charts**: Logarithmic Y-axis for cloud (100-24,000ft) and wind (600-12,000ft) charts
- **VFR calculation**: Based on cloud base, visibility (>=5km), wind speed, precipitation, and civil twilight
- **API**: Open-Meteo ICON Seamless model, 18 pressure levels for clouds, 7 for wind
- **Container**: Multi-stage Docker build, non-root user, alpine-based
- **Debug**: `const DEBUG = false` in `main.go` toggles verbose logging
