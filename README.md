# Flugwetter ✈️🌤️

A comprehensive aviation weather application designed for flight planning and weather analysis. Features interactive synchronized charts displaying detailed meteorological data with traditional aviation symbols and professional weather visualization.

![Aviation Weather Dashboard](https://img.shields.io/badge/Aviation-Weather%20Dashboard-blue?style=for-the-badge&logo=airplane)

## 🚀 Features

### 📊 Interactive Weather Charts (2x2 Grid Layout)
- **🌡️ Temperature Chart**: Temperature, dew point, and precipitation with probability-based transparency
- **☁️ Cloud Chart**: Cloud coverage symbols at multiple altitudes with dynamic transparency
- **💨 Surface Wind Chart**: Wind barbs and speed/gust lines for low-level winds (10m & 80m)
- **🌪️ Altitude Wind Chart**: Traditional aviation wind barbs for mid-level winds (600ft - 12,000ft)

### 🎯 Aviation-Specific Features
- **Traditional Wind Barbs**: Meteorologically accurate wind symbols (calm, half-barb, full-barb, pennant)
- **Aviation Units**: Heights in feet, wind speeds in knots
- **Pressure Level Data**: Uses hPa levels with geopotential height conversion
- **Flight Planning Focus**: Optimized altitude ranges for practical aviation use

### 🖱️ Interactive Controls
- **Synchronized Pan/Zoom**: All 4 charts move together for consistent analysis
- **Manual Controls**: Custom pan/zoom implementation using canvas events
- **Time Navigation**: 3-hour intervals with date markers at midnight
- **Responsive Design**: 2x2 grid on desktop, single column on mobile

### ⚡ Performance & Reliability
- **Smart Caching**: 15-minute server-side caching for optimal API usage
- **Real-time Updates**: Automatic data refresh with visual indicators
- **Error Handling**: Graceful error recovery with retry functionality
- **Mobile Optimized**: Full responsive design for all device sizes

## 🏗️ Architecture

### Backend (Go)
```
🔧 HTTP Server (gorilla/mux)
📡 Open-Meteo API Integration
💾 15-minute Smart Caching
🔄 Data Processing & Conversion
📊 Aviation Unit Conversion (m→ft)
🌐 Static File Serving
```

### Frontend (JavaScript/HTML/CSS)
```
📈 Chart.js Interactive Visualization
🎨 Custom Weather Symbol Plugins
🖱️ Manual Pan/Zoom Implementation
📱 Responsive Grid Layout
⚡ Async Data Loading
```

## 🛠️ Installation & Setup

### Prerequisites
- **Go**: Version 1.21 or higher
- **Internet**: For Open-Meteo API access
- **Browser**: Modern browser with Canvas support

### Quick Start

1. **Clone the repository:**
```bash
git clone <repository-url>
cd flugwetter
```

2. **Install dependencies:**
```bash
cd backend
go mod tidy
```

3. **Start the server:**
```bash
go run .
```

4. **Open in browser:**
```
http://localhost:8080
```

### 🐳 Docker Setup (Optional)
```bash
# Build image
docker build -t flugwetter .

# Run container
docker run -p 8080:8080 flugwetter
```

## 📡 API Reference

### Weather Data Endpoint
```http
GET /api/weather
```

**Response Structure:**
```json
{
  "temperature_data": [
    {
      "time": "2024-01-01T00:00",
      "temperature": 10.5,
      "dew_point": 8.2,
      "precipitation": 0.3,
      "precipitation_probability": 75
    }
  ],
  "cloud_data": [
    {
      "time": "2024-01-01T00:00",
      "cloud_layers": [
        {
          "height_feet": 2500,
          "coverage": 85,
          "symbol": "☁"
        }
      ]
    }
  ],
  "surface_wind_data": [
    {
      "time": "2024-01-01T00:00",
      "wind_speed_10m": 12.5,
      "wind_gusts_10m": 18.2,
      "wind_layers": [
        {
          "height_feet": 33,
          "speed": 12.5,
          "direction": 240,
          "symbol": "barb"
        }
      ]
    }
  ],
  "wind_data": [
    {
      "time": "2024-01-01T00:00",
      "wind_layers": [
        {
          "height_feet": 3280,
          "speed": 25.8,
          "direction": 270,
          "symbol": "barb"
        }
      ]
    }
  ]
}
```

### Static Assets
```http
GET /static/{file}    # CSS, JavaScript, images
GET /              # Main application page
```

## ⚙️ Configuration

### Location Settings
Default location (modify in `backend/weather.go`):
```go
// Coordinates for weather data
latitude := 52.13499946   // Northern Germany
longitude := 7.684830594  // Example location
```

### Chart Altitude Ranges
```javascript
// Configurable in frontend/app.js
cloudChart: 100ft - 24,000ft     (logarithmic)
surfaceWind: 0ft - 300ft         (linear)
altitudeWind: 600ft - 12,000ft   (logarithmic)
```

### Cache Duration
```go
// Modify in backend/weather.go
cacheDuration := 15 * time.Minute
```

## 🌐 Data Source

**[Open-Meteo API](https://open-meteo.com/)**
- ✅ High-resolution ICON model forecasts
- ✅ No API key required
- ✅ 96-hour forecast range
- ✅ Pressure level data (1000hPa - 30hPa)
- ✅ Geopotential height conversion
- ✅ Wind speeds in knots (aviation standard)

**Location**: Latitude 52.13499946, Longitude 7.684830594

## 📁 Project Structure

```
flugwetter/
├── 📋 specs.md                 # Detailed specifications
├── 📖 README.md               # This documentation
├── 🔧 backend/                # Go server
│   ├── 🏠 main.go             # HTTP server & data structures
│   ├── 🌡️ weather.go          # API client & data processing
│   └── 📦 go.mod              # Go dependencies
└── 🌐 frontend/               # Web interface
    ├── 🏠 index.html          # Main page structure
    ├── 🎨 styles.css          # Responsive styling
    └── ⚡ app.js              # Charts & interactions
```

## 🎯 Usage Guide

### Navigation
- **Pan**: Click and drag on any chart to pan horizontally
- **Zoom**: Use mouse wheel to zoom in/out on time range
- **Sync**: All charts move together automatically
- **Reset**: Double-click any chart to reset zoom

### Reading Weather Data

#### 🌡️ Temperature Chart
- **Red Line**: 2m Temperature (°C)
- **Green Line**: 2m Dew Point (°C) 
- **Blue Area**: Precipitation (mm) with probability-based transparency
- **Right Scale**: Precipitation amount (0-10mm)

#### ☁️ Cloud Chart
- **☁ Symbols**: Cloud coverage at various altitudes
- **Transparency**: 0% = invisible, 100% = opaque
- **Y-Axis**: Height in feet (logarithmic scale)

#### 💨 Surface Wind Chart
- **Wind Barbs**: Traditional aviation symbols at 33ft & 262ft
- **Green Line**: Wind speed at 10m
- **Red Dashed**: Wind gusts at 10m
- **Right Scale**: Wind speed in knots

#### 🌪️ Altitude Wind Chart
- **Wind Barbs**: Mid-level winds (600ft - 12,000ft)
- **Barb Direction**: Points FROM wind direction
- **Barb Symbols**: Calm (○), 5kn (╱), 10kn (│), 50kn (▲)

## 🔧 Development

### Backend Development
```bash
cd backend
go run . -dev          # Development mode
go build              # Build executable
go test               # Run tests
```

### Frontend Development
```bash
# Serve static files during development
cd frontend
python -m http.server 8000
# Or use any static file server
```

### Custom Chart Plugins
The application includes custom Chart.js plugins for:
- **Cloud Symbols**: Dynamic transparency rendering
- **Wind Barbs**: Traditional aviation wind symbols
- **Chart Clipping**: Symbol boundary management

### Adding New Features
1. **Backend**: Extend data structures in `main.go`
2. **API Processing**: Add logic in `weather.go`
3. **Frontend**: Create new chart in `app.js`
4. **Styling**: Add responsive CSS in `styles.css`

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit changes (`git commit -m 'Add amazing feature'`)
4. Push to branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development Guidelines
- Follow Go best practices for backend code
- Use semantic commit messages
- Add tests for new functionality
- Ensure responsive design for frontend changes
- Document new features in specs.md

## 🚀 Deployment

### Production Build
```bash
# Build optimized backend
cd backend
CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o flugwetter .

# Serve with reverse proxy (nginx/caddy)
./flugwetter
```

### Environment Variables
```bash
export PORT=8080                    # Server port
export CACHE_DURATION=15m           # Cache duration
export API_TIMEOUT=30s              # API request timeout
```

## 📊 Performance

- **API Caching**: 15-minute cache reduces API calls by ~95%
- **Data Filtering**: Backend filters irrelevant altitude data
- **Symbol Rendering**: Optimized canvas drawing with clipping
- **Responsive Design**: Efficient CSS Grid with mobile optimization
- **Chart Synchronization**: Minimal overhead pan/zoom implementation

## 🎯 Aviation Use Cases

- **Pre-flight Planning**: Comprehensive weather overview
- **Route Planning**: Wind data for fuel calculations
- **Weather Analysis**: Cloud layers and precipitation timing
- **Safety Assessment**: Wind shear and turbulence indicators
- **Training**: Educational weather visualization

## 🙏 Acknowledgments

This project was developed with the assistance of **Cursor AI**, an AI-powered code editor that helped accelerate development, refine features, and ensure best practices throughout the implementation of this aviation weather visualization platform.

## 📄 License

This project is open source and available under the [MIT License](LICENSE).

---

**Built with ❤️ for the aviation community** ✈️

*For detailed technical specifications, see [specs.md](specs.md)* 