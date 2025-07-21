# Flugwetter Frontend 🌐

JavaScript frontend for the Flugwetter aviation weather application, featuring interactive synchronized charts with professional weather visualization.

## 🎯 Features

### 📊 Chart Visualization
- **Chart.js Integration**: Professional time-series charts with aviation data
- **2x2 Grid Layout**: Responsive design (desktop) → single column (mobile)
- **Synchronized Interaction**: All 4 charts pan/zoom together
- **Custom Plugins**: Weather symbols and traditional wind barbs

### ⚡ Interactive Controls
- **Manual Pan/Zoom**: Custom canvas event handling for smooth interaction
- **Chart Synchronization**: Coordinated movement across all charts
- **Responsive Design**: Optimized for desktop, tablet, and mobile
- **Touch Support**: Mobile-friendly gesture handling

### ✈️ Aviation Symbols
- **Traditional Wind Barbs**: Meteorologically accurate aviation symbols
- **Cloud Symbols**: Dynamic transparency based on coverage percentage
- **Symbol Clipping**: Proper boundary management within chart areas
- **Professional Styling**: Aviation industry standard visualizations

## 📁 File Structure

```
frontend/
├── 📖 README.md          # This documentation
├── 🏠 index.html         # Main application page
├── 🎨 styles.css         # Responsive styling & grid layout
└── ⚡ app.js            # Chart initialization & interactions
```

## 🛠️ Dependencies

### Core Libraries
- **Chart.js**: `^4.x` - Interactive chart library
- **Chart.js Adapter Date-FNS**: Date/time handling for time-series data
- **Date-FNS**: Date formatting and manipulation

### CDN Resources
```html
<!-- Chart.js Core -->
<script src="https://cdn.jsdelivr.net/npm/chart.js"></script>

<!-- Date Adapter -->
<script src="https://cdn.jsdelivr.net/npm/chartjs-adapter-date-fns/dist/chartjs-adapter-date-fns.bundle.min.js"></script>
```

## 📊 Chart Implementation

### 1. Temperature Chart (Top Left)
```javascript
// Multi-axis chart with temperature, dew point, and precipitation
- Red line: 2m Temperature (°C)
- Green line: 2m Dew Point (°C)
- Blue area: Precipitation (mm) with probability-based transparency
- Axes: Left (temp/dew point), Right (precipitation), Invisible (probability)
```

### 2. Cloud Chart (Top Right)
```javascript
// Scatter plot with custom cloud symbols
- Y-axis: Height in feet (100-24,000ft, logarithmic)
- Symbols: ☁ with dynamic transparency (0-100% coverage)
- Plugin: cloudSymbols for custom rendering
```

### 3. Surface Wind Chart (Bottom Left)
```javascript
// Combined scatter + line chart
- Scatter: Wind barbs at 33ft & 262ft
- Lines: Wind speed (green) & gusts (red) at 10m
- Axes: Left (height 0-300ft), Right (wind speed knots)
```

### 4. Wind Chart (Bottom Right)
```javascript
// Scatter plot with wind barbs
- Y-axis: Height in feet (600-12,000ft, logarithmic)
- Symbols: Traditional aviation wind barbs
- Plugin: windBarbs for meteorological accuracy
```

## 🎨 Custom Chart.js Plugins

### Cloud Symbols Plugin
```javascript
// Renders cloud symbols with dynamic transparency
- Hook: afterDatasetsDraw
- Symbol: ☁ (200% scale)
- Transparency: Based on coverage percentage
- Clipping: Constrained to chart area
```

### Wind Barbs Plugin
```javascript
// Traditional aviation wind barb rendering
- Calm: Circle for <1 knot
- Half Barb: 5 knots
- Full Barb: 10 knots
- Pennant: 50 knots
- Direction: Points FROM wind direction
- Position: Left side of shaft, at tail end
```

### Manual Pan/Zoom Implementation
```javascript
// Custom event handling for chart interaction
- Mouse Events: mousedown, mousemove, mouseup
- Wheel Events: zoom in/out on time axis
- Touch Events: Mobile gesture support
- Synchronization: All charts move together
- Reset: Double-click to reset zoom
```

## 🎯 Development

### Local Development
```bash
# Serve static files (any method)
cd frontend
python -m http.server 8080
# OR
npx serve .
# OR
php -S localhost:8080
```

### File Modification Workflow
1. **HTML Structure**: Modify `index.html` for layout changes
2. **Styling**: Update `styles.css` for visual improvements
3. **Chart Logic**: Edit `app.js` for chart behavior and data processing
4. **Test**: Open in browser and test with backend running

### Key Functions in app.js

#### Chart Initialization
```javascript
initializeTemperatureChart()    // Temperature, dew point, precipitation
initializeCloudChart()          // Cloud coverage with symbols
initializeSurfaceWindChart()    // Surface wind barbs + line data
initializeWindChart()           // Altitude wind barbs
```

#### Data Processing
```javascript
updateCharts(data)              // Process API response and update all charts
setupManualPanZoom()           // Initialize custom pan/zoom handlers
resetZoom()                    // Reset all charts to original view
```

#### Custom Drawing
```javascript
drawCloudSymbol()              // Render cloud symbol with transparency
drawWindBarb()                 // Draw traditional aviation wind barbs
```

## 🎨 Responsive Design

### Grid Layout (styles.css)
```css
/* Desktop: 2x2 grid */
.charts-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    grid-template-rows: 1fr 1fr;
}

/* Mobile: Single column */
@media (max-width: 768px) {
    .charts-grid {
        grid-template-columns: 1fr;
    }
}
```

### Chart Responsiveness
- **Canvas Sizing**: Automatic resize with container
- **Font Scaling**: Responsive text based on screen size
- **Touch Optimization**: Mobile-friendly interaction
- **Legend Layout**: Single-line legends on smaller screens

## 🔧 Configuration

### Chart Altitude Ranges
```javascript
// Modify in app.js chart initialization
cloudChart: { min: 100, max: 24000 }      // feet, logarithmic
surfaceWind: { min: 0, max: 300 }         // feet, linear  
altitudeWind: { min: 600, max: 12000 }    // feet, logarithmic
```

### Time Display
```javascript
// X-axis configuration
- Intervals: 3-hour gridlines starting at midnight
- Format: Date at midnight, time at other intervals
- Range: 24 hours displayed on load
- Navigation: Pan/zoom for extended ranges
```

### Symbol Customization
```javascript
// Cloud symbols
cloudScale: 2.0                    // 200% symbol size
transparencyMode: 'coverage'       // 0-100% based on coverage

// Wind barbs  
barbLength: 15                     // Barb line length
calmRadius: 3                      // Calm wind circle size
```

## 🚀 Performance Optimizations

### Chart Rendering
- **Data Filtering**: Only render visible data points
- **Symbol Clipping**: Constrain drawing to chart areas
- **Event Throttling**: Optimized pan/zoom event handling
- **Canvas Efficiency**: Minimal redraw operations

### Memory Management
- **Data Caching**: Reuse processed data structures
- **Event Cleanup**: Proper event listener management
- **Chart Updates**: Incremental data updates vs full reinitialization

## 🧪 Testing & Debugging

### Browser Developer Tools
```javascript
// Global variables for debugging
window.temperatureChart    // Access Chart.js instance
window.cloudChart         // Debug cloud rendering
window.surfaceWindChart   // Check wind barb data
window.windChart          // Inspect altitude wind data
```

### Common Issues
1. **Symbols Not Visible**: Check chart area clipping and data ranges
2. **Pan/Zoom Not Working**: Verify event listeners and chart synchronization
3. **Mobile Touch Issues**: Test touch event handling and responsive layout
4. **Symbol Positioning**: Validate wind barb rotation and cloud transparency

## 🤝 Contributing

### Adding New Charts
1. Create chart initialization function
2. Add to `updateCharts()` data processing
3. Include in pan/zoom synchronization
4. Update responsive CSS grid layout

### Custom Plugins
1. Follow Chart.js plugin architecture
2. Use appropriate hooks (`afterDatasetsDraw`, etc.)
3. Implement chart area clipping
4. Add to plugin registration

### Styling Guidelines
- Follow aviation industry visual standards
- Maintain responsive design principles
- Use consistent color schemes and typography
- Ensure accessibility compliance

---

*Part of the Flugwetter aviation weather dashboard - Frontend implementation*

**Tech Stack**: Vanilla JavaScript, Chart.js, CSS Grid, Canvas API 