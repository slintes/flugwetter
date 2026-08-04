# Flugwetter Backend 🔧

Go backend server for the Flugwetter aviation weather application.

## Features

- **HTTP Server**: REST API using `gorilla/mux` router
- **Weather API Integration**: Fetches data from Open-Meteo API
- **Multiple Airfields**: Configurable airport list, per-airport forecasts and crosswinds
- **Smart Caching**: 15-minute thread-safe caching system, one entry per airport
- **Data Processing**: Converts raw API data to aviation-friendly format
- **Unit Conversion**: Automatic height conversion (meters → feet)
- **Static Serving**: Serves frontend assets

## Quick Start

```bash
# Install dependencies
go mod tidy

# Run development server
go run .

# Build for production
go build -o flugwetter
```

## API Endpoints

- `GET /` - Serves main application page
- `GET /api/config` - Airport list (display order), default airport, openAIP overlay availability
- `GET /api/weather?airport=EDWN` - Returns processed weather data; omitting `airport` uses the
  default, an unknown identifier is a 400
- `GET /api/tiles/openaip/{z}/{x}/{y}.png` - openAIP tile proxy, only registered when
  `OPENAIP_API_KEY` is set
- `GET /static/*` - Serves static frontend assets

## Configuration

Airfields live in `backend/airports.json`, embedded into the binary via `go:embed`. Each entry:

```json
{
  "identifier": "EDWN",
  "name": "Nordhorn-Lingen",
  "latitude": 52.4575,
  "longitude": 7.185,
  "runways": ["05/23"],
  "runway_headings": [53, 233],
  "pinned": true
}
```

`runway_headings` are **true** headings for both ends of every runway (published designators
are magnetic and rounded); `runways` is the display label. `pinned` puts the entry at the top
of the dropdown and makes it the default — at most one entry may carry it. Display order is
computed at startup (pinned first, then north to south), so new entries can be appended
anywhere in the file.

### Environment variables

| Variable | Effect |
| --- | --- |
| `FLUGWETTER_AIRPORTS_FILE` | Path to a JSON file replacing the embedded airport list |
| `OPENAIP_API_KEY` | Enables the openAIP overlay on the map picker. Free key from accounts.openaip.net |

A malformed airport list is fatal at startup rather than served as an empty dropdown.

## Dependencies

- `github.com/gorilla/mux` - HTTP routing
- Standard Go libraries for HTTP, JSON, time handling

## Data Processing

1. **Fetch**: Open-Meteo API with comprehensive parameters
2. **Cache**: 15-minute intelligent caching
3. **Process**: Extract relevant data for each chart type
4. **Convert**: Heights to feet, speeds to knots
5. **Filter**: Altitude ranges for optimal chart display
6. **Serve**: JSON API for frontend consumption

---

*Part of the Flugwetter aviation weather dashboard*
