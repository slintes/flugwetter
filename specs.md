Goal
====

This is an aviation weather application designed for flight planning and weather analysis. It reads detailed weather forecast data from the Open-Meteo API and displays comprehensive weather information on a responsive web interface with synchronized interactive charts.

Architecture
============

The application consists of a Go backend and a JavaScript frontend:

**Backend (Go)**
- HTTP server using `github.com/gorilla/mux` router
- Fetches weather data from Open-Meteo API
- Caches results for 15 minutes to optimize API usage
- Processes and converts raw API data for frontend consumption
- Converts all heights from meters to feet for aviation use
- Serves static frontend files and provides REST API endpoint

**Frontend (JavaScript/HTML/CSS)**
- Responsive web interface with 2x2 chart grid layout
- Built with Chart.js for interactive time-series visualization
- Custom Chart.js plugins for weather symbols and wind barbs
- Manual pan/zoom implementation with chart synchronization
- Mobile-responsive design (single column on small screens)

Chart Layout (2x2 Grid)
=======================

**1. Temperature Chart (Top Left)**
- **Primary Data**: 2m Temperature (°C) - Red line
- **Secondary Data**: 2m Dew Point (°C) - Green line
- **Precipitation**: Area chart (mm) - Blue with transparency based on precipitation probability
- **Y-Axes**: Left (Temperature/Dew Point), Right (Precipitation 0-10mm), Invisible (Precipitation Probability 0-100%)
- **Features**: Dynamic precipitation transparency, tooltip shows all values

**2. Cloud Chart (Top Right)**
- **Data**: Cloud coverage at pressure levels (hPa) converted to altitude
- **Y-Axis**: Height in feet (100ft - 24,000ft) - Logarithmic scale
- **Visualization**: Cloud symbols (☁) with transparency based on coverage percentage
- **Features**: 0% coverage = transparent, 100% coverage = opaque, 200% symbol scaling

**3. Surface Wind Chart (Bottom Left)**
- **Wind Barbs**: Wind speed/direction at 10m (33ft) and 80m (262ft)
- **Line Data**: Wind speed and gusts at 10m (knots)
- **Y-Axes**: Left (Height 0-300ft linear), Right (Wind Speed 0+ knots)
- **Features**: Traditional aviation wind barbs, synchronized with other charts

**4. Wind Chart (Bottom Right)**
- **Data**: Wind speed/direction at various pressure levels
- **Y-Axis**: Height in feet (600ft - 12,000ft) - Logarithmic scale
- **Visualization**: Traditional aviation wind barbs with proper rotation
- **Features**: Barbs positioned at tail end, left side of shaft

Interactive Features
===================

**Chart Synchronization**
- All 4 charts are synchronized for pan and zoom operations
- Manual pan/zoom implementation using canvas event listeners
- Consistent time range displayed across all charts (24 hours on load)

**Time Axis**
- X-axis shows 3-hour intervals starting at midnight
- Gridlines every 3 hours for easy time reference
- Date displayed at midnight gridlines
- No axis title (clean design)

**Responsive Design**
- 2x2 grid layout on desktop/tablet (≥768px width)
- Single column layout on mobile (<768px width)
- Full screen width utilization with appropriate margins
- Centered chart titles, single-line legends

Data Processing
===============

**Backend Processing**
- **Cloud Data**: Uses hPa pressure levels with geopotential height conversion
- **Wind Data**: Processes both fixed heights (10m, 80m) and pressure levels
- **Height Conversion**: All altitudes converted from meters to feet (1m = 3.28084ft)
- **Filtering**: Only includes relevant altitude ranges for each chart
- **Symbol Generation**: Determines appropriate weather symbols based on conditions

**API Data Sources**
- **Temperature**: 2m temperature, dew point
- **Precipitation**: Hourly precipitation amount and probability
- **Clouds**: Cloud coverage at multiple pressure levels (1000hPa to 30hPa)
- **Winds**: Wind speed/direction at fixed heights and pressure levels
- **Geopotential**: Height conversion data for pressure levels

Technical Implementation
========================

**Custom Chart.js Plugins**
- **Cloud Symbols**: Custom rendering with dynamic transparency
- **Wind Barbs**: Traditional aviation wind barbs (calm circle, half barb, full barb, pennant)
- **Chart Area Clipping**: Ensures symbols stay within chart boundaries

**Wind Barb Specifications**
- **Calm Winds**: Circle symbol for <1 knot
- **Half Barb**: 5 knots
- **Full Barb**: 10 knots  
- **Pennant**: 50 knots
- **Direction**: Points FROM wind direction (meteorological convention)
- **Position**: Barbs on left side of shaft, at tail end

**Performance Optimizations**
- 15-minute server-side caching
- Efficient data filtering by altitude ranges
- Optimized symbol rendering with clipping
- Responsive canvas sizing

API Endpoint
============

**Open-Meteo API URL:**
```
https://api.open-meteo.com/v1/forecast?latitude=52.13499946&longitude=7.684830594&hourly=precipitation_probability,pressure_msl,cloud_cover_low,cloud_cover,cloud_cover_mid,cloud_cover_high,wind_speed_120m,wind_speed_180m,wind_direction_120m,wind_direction_180m,temperature_180m,temperature_120m,temperature_2m,relative_humidity_2m,dew_point_2m,apparent_temperature,precipitation,rain,showers,snowfall,snow_depth,weather_code,surface_pressure,visibility,wind_speed_10m,wind_speed_80m,wind_direction_10m,wind_direction_80m,wind_gusts_10m,temperature_80m,cloud_cover_1000hPa,cloud_cover_975hPa,cloud_cover_950hPa,cloud_cover_925hPa,cloud_cover_900hPa,cloud_cover_850hPa,cloud_cover_800hPa,cloud_cover_700hPa,cloud_cover_600hPa,cloud_cover_500hPa,cloud_cover_400hPa,cloud_cover_300hPa,cloud_cover_200hPa,cloud_cover_250hPa,cloud_cover_150hPa,cloud_cover_100hPa,cloud_cover_70hPa,cloud_cover_50hPa,cloud_cover_30hPa,wind_speed_1000hPa,wind_speed_975hPa,wind_speed_950hPa,wind_speed_925hPa,wind_speed_900hPa,wind_speed_850hPa,wind_speed_800hPa,wind_speed_700hPa,wind_speed_600hPa,wind_direction_600hPa,wind_direction_700hPa,wind_direction_800hPa,wind_direction_850hPa,wind_direction_900hPa,wind_direction_925hPa,wind_direction_950hPa,wind_direction_975hPa,wind_direction_1000hPa,geopotential_height_1000hPa,geopotential_height_975hPa,geopotential_height_950hPa,geopotential_height_925hPa,geopotential_height_900hPa,geopotential_height_850hPa,geopotential_height_800hPa,geopotential_height_700hPa,geopotential_height_600hPa,geopotential_height_500hPa,geopotential_height_400hPa,geopotential_height_300hPa,geopotential_height_250hPa,geopotential_height_200hPa,geopotential_height_150hPa,geopotential_height_100hPa,geopotential_height_70hPa,geopotential_height_50hPa,geopotential_height_30hPa&models=icon_seamless&minutely_15=precipitation,rain,snowfall,weather_code,wind_speed_10m,wind_speed_80m,wind_direction_10m,wind_direction_80m,wind_gusts_10m,visibility,cape,lightning_potential&timezone=auto&wind_speed_unit=kn&forecast_minutely_15=96
```

**Key Parameters:**
- **Location**: Latitude 52.13499946, Longitude 7.684830594
- **Wind Speed Unit**: Knots (aviation standard)
- **Timezone**: Auto-detected
- **Model**: ICON Seamless
- **Forecast Duration**: 96 hours

Development Notes
=================

**Dependencies**
- **Backend**: Go with `github.com/gorilla/mux`
- **Frontend**: Chart.js with date-fns adapter
- **Browser Compatibility**: Modern browsers with Canvas support

**File Structure**
```
backend/
  ├── main.go          # HTTP server and data structures
  ├── weather.go       # API fetching and data processing
  └── go.mod          # Go module dependencies

frontend/
  ├── index.html      # Main HTML structure
  ├── styles.css      # Responsive styling
  └── app.js          # Chart initialization and interaction
```

**Altitude Ranges (in feet)**
- **Cloud Chart**: 100ft - 24,000ft (logarithmic)
- **Surface Wind**: 0ft - 300ft (linear)  
- **Altitude Wind**: 600ft - 12,000ft (logarithmic)

This application provides comprehensive aviation weather visualization suitable for flight planning, weather analysis, and meteorological study.

