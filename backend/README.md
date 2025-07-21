# Flugwetter Backend 🔧

Go backend server for the Flugwetter aviation weather application.

## Features

- **HTTP Server**: REST API using `gorilla/mux` router
- **Weather API Integration**: Fetches data from Open-Meteo API
- **Smart Caching**: 15-minute thread-safe caching system
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
- `GET /api/weather` - Returns processed weather data
- `GET /static/*` - Serves static frontend assets

## Configuration

Weather data location (modify in `weather.go`):
```go
latitude := 52.13499946
longitude := 7.684830594
```

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
