# Flugwetter 🛩️

An aviation weather forecast application that displays detailed weather data with interactive charts.

## Features

- **Real-time Weather Data**: Fetches detailed weather forecasts from Open-Meteo API
- **Temperature Visualization**: Interactive line chart showing 2-meter temperature over time
- **Cloud Cover Analysis**: Stacked bar chart displaying cloud coverage at different altitudes (low, mid, high)
- **Automatic Caching**: 15-minute data caching to optimize API usage and performance
- **Auto-refresh**: Automatically updates data every 15 minutes
- **Responsive Design**: Works on desktop and mobile devices
- **Error Handling**: Graceful error handling with retry functionality

## Architecture

The application consists of two main components:

### Backend (Go)
- HTTP server with REST API endpoints
- Weather data fetching from Open-Meteo API
- Thread-safe 15-minute caching mechanism
- Data processing and formatting for frontend consumption
- Static file serving for frontend assets

### Frontend (HTML/CSS/JavaScript)
- Modern, responsive web interface
- Interactive charts using Chart.js
- Real-time data loading and visualization
- Error handling and loading states

## Installation & Setup

### Prerequisites
- Go 1.21 or higher
- Internet connection for API access

### Steps

1. **Clone or create the project structure:**
```
flugwetter/
├── backend/
│   ├── main.go
│   ├── weather.go
│   └── go.mod
└── frontend/
    ├── index.html
    ├── styles.css
    └── app.js
```

2. **Install Go dependencies:**
```bash
cd backend
go mod tidy
```

3. **Run the application:**
```bash
cd backend
go run .
```

4. **Open your browser:**
Navigate to `http://localhost:8080`

## API Endpoints

### `GET /`
Serves the main HTML page.

### `GET /api/weather`
Returns processed weather data in JSON format:
```json
{
  "temperature_data": [
    {
      "time": "2024-01-01T00:00",
      "temperature": 10.5
    }
  ],
  "cloud_data": [
    {
      "time": "2024-01-01T00:00",
      "low_cloud_cover": 20,
      "mid_cloud_cover": 30,
      "high_cloud_cover": 10
    }
  ]
}
```

### `GET /static/*`
Serves static frontend assets (CSS, JavaScript).

## Configuration

The application is configured to fetch weather data for coordinates:
- **Latitude**: 52.13499946
- **Longitude**: 7.684830594

To change the location, modify the `apiURL` variable in `backend/weather.go`.

## Data Source

Weather data is provided by the [Open-Meteo API](https://open-meteo.com/), which offers:
- High-resolution weather forecasts
- Detailed atmospheric data including temperature, cloud coverage, wind, precipitation
- Free access without API key requirements
- Hourly and sub-hourly forecasts

## Development

### Project Structure
```
flugwetter/
├── specs.md              # Project specifications
├── README.md             # This file
├── backend/              # Go backend server
│   ├── main.go          # Main server and routing
│   ├── weather.go       # Weather API client and caching
│   └── go.mod           # Go module dependencies
└── frontend/            # Web frontend
    ├── index.html       # Main HTML page
    ├── styles.css       # Styling
    └── app.js          # JavaScript for charts and API calls
```

### Key Components

#### Backend Components
- **Weather API Client**: Handles HTTP requests to Open-Meteo API
- **Caching System**: Thread-safe caching with automatic expiration
- **Data Processing**: Converts raw API data to frontend-friendly format
- **HTTP Server**: Serves API endpoints and static files

#### Frontend Components
- **Chart.js Integration**: Interactive temperature and cloud cover charts
- **Async Data Loading**: Non-blocking API calls with loading states
- **Error Handling**: User-friendly error messages and retry functionality
- **Responsive Design**: Mobile-first CSS with modern styling

## Future Enhancements

- [ ] Add wind speed and direction visualization
- [ ] Implement precipitation forecasts
- [ ] Add multiple location support
- [ ] Include weather alerts and warnings
- [ ] Add data export functionality
- [ ] Implement user preferences and settings
- [ ] Add historical weather data comparison

## License

This project is open source and available under the MIT License. 