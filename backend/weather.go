package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// WeatherAPIResponse represents the complete API response from open-meteo
type WeatherAPIResponse struct {
	Hourly struct {
		Time               []string  `json:"time"`
		Temperature2m      []float64 `json:"temperature_2m"`
		DewPoint2m         []float64 `json:"dew_point_2m"`
		CloudCoverLow      []int     `json:"cloud_cover_low"`
		CloudCover         []int     `json:"cloud_cover"`
		CloudCoverMid      []int     `json:"cloud_cover_mid"`
		CloudCoverHigh     []int     `json:"cloud_cover_high"`
		Precipitation      []float64 `json:"precipitation"`
		WindSpeed10m       []float64 `json:"wind_speed_10m"`
		WindDirection10m   []int     `json:"wind_direction_10m"`
		WindGusts10m       []float64 `json:"wind_gusts_10m"`
		WindSpeed80m       []float64 `json:"wind_speed_80m"`
		WindDirection80m   []int     `json:"wind_direction_80m"`
		Pressure           []float64 `json:"pressure_msl"`
		RelativeHumidity2m []int     `json:"relative_humidity_2m"`

		// hPa-based wind data
		WindSpeed1000hPa []float64 `json:"wind_speed_1000hPa"`
		WindSpeed975hPa  []float64 `json:"wind_speed_975hPa"`
		WindSpeed950hPa  []float64 `json:"wind_speed_950hPa"`
		WindSpeed925hPa  []float64 `json:"wind_speed_925hPa"`
		WindSpeed900hPa  []float64 `json:"wind_speed_900hPa"`
		WindSpeed850hPa  []float64 `json:"wind_speed_850hPa"`
		WindSpeed800hPa  []float64 `json:"wind_speed_800hPa"`
		WindSpeed700hPa  []float64 `json:"wind_speed_700hPa"`
		WindSpeed600hPa  []float64 `json:"wind_speed_600hPa"`

		WindDirection1000hPa []int `json:"wind_direction_1000hPa"`
		WindDirection975hPa  []int `json:"wind_direction_975hPa"`
		WindDirection950hPa  []int `json:"wind_direction_950hPa"`
		WindDirection925hPa  []int `json:"wind_direction_925hPa"`
		WindDirection900hPa  []int `json:"wind_direction_900hPa"`
		WindDirection850hPa  []int `json:"wind_direction_850hPa"`
		WindDirection800hPa  []int `json:"wind_direction_800hPa"`
		WindDirection700hPa  []int `json:"wind_direction_700hPa"`
		WindDirection600hPa  []int `json:"wind_direction_600hPa"`

		// hPa-based cloud cover data
		CloudCover1000hPa []int `json:"cloud_cover_1000hPa"`
		CloudCover975hPa  []int `json:"cloud_cover_975hPa"`
		CloudCover950hPa  []int `json:"cloud_cover_950hPa"`
		CloudCover925hPa  []int `json:"cloud_cover_925hPa"`
		CloudCover900hPa  []int `json:"cloud_cover_900hPa"`
		CloudCover850hPa  []int `json:"cloud_cover_850hPa"`
		CloudCover800hPa  []int `json:"cloud_cover_800hPa"`
		CloudCover700hPa  []int `json:"cloud_cover_700hPa"`
		CloudCover600hPa  []int `json:"cloud_cover_600hPa"`
		CloudCover500hPa  []int `json:"cloud_cover_500hPa"`
		CloudCover400hPa  []int `json:"cloud_cover_400hPa"`
		CloudCover300hPa  []int `json:"cloud_cover_300hPa"`
		CloudCover250hPa  []int `json:"cloud_cover_250hPa"`
		CloudCover200hPa  []int `json:"cloud_cover_200hPa"`
		CloudCover150hPa  []int `json:"cloud_cover_150hPa"`
		CloudCover100hPa  []int `json:"cloud_cover_100hPa"`
		CloudCover70hPa   []int `json:"cloud_cover_70hPa"`
		CloudCover50hPa   []int `json:"cloud_cover_50hPa"`
		CloudCover30hPa   []int `json:"cloud_cover_30hPa"`

		// Geopotential heights
		GeopotentialHeight1000hPa []float64 `json:"geopotential_height_1000hPa"`
		GeopotentialHeight975hPa  []float64 `json:"geopotential_height_975hPa"`
		GeopotentialHeight950hPa  []float64 `json:"geopotential_height_950hPa"`
		GeopotentialHeight925hPa  []float64 `json:"geopotential_height_925hPa"`
		GeopotentialHeight900hPa  []float64 `json:"geopotential_height_900hPa"`
		GeopotentialHeight850hPa  []float64 `json:"geopotential_height_850hPa"`
		GeopotentialHeight800hPa  []float64 `json:"geopotential_height_800hPa"`
		GeopotentialHeight700hPa  []float64 `json:"geopotential_height_700hPa"`
		GeopotentialHeight600hPa  []float64 `json:"geopotential_height_600hPa"`
		GeopotentialHeight500hPa  []float64 `json:"geopotential_height_500hPa"`
		GeopotentialHeight400hPa  []float64 `json:"geopotential_height_400hPa"`
		GeopotentialHeight300hPa  []float64 `json:"geopotential_height_300hPa"`
		GeopotentialHeight250hPa  []float64 `json:"geopotential_height_250hPa"`
		GeopotentialHeight200hPa  []float64 `json:"geopotential_height_200hPa"`
		GeopotentialHeight150hPa  []float64 `json:"geopotential_height_150hPa"`
		GeopotentialHeight100hPa  []float64 `json:"geopotential_height_100hPa"`
		GeopotentialHeight70hPa   []float64 `json:"geopotential_height_70hPa"`
		GeopotentialHeight50hPa   []float64 `json:"geopotential_height_50hPa"`
		GeopotentialHeight30hPa   []float64 `json:"geopotential_height_30hPa"`
	} `json:"hourly"`
}

// WeatherCache manages cached weather data
type WeatherCache struct {
	data      *ProcessedWeatherData
	timestamp time.Time
	mutex     sync.RWMutex
}

var (
	cache         = &WeatherCache{}
	cacheDuration = 15 * time.Minute
	apiURL        = "https://api.open-meteo.com/v1/forecast?latitude=52.13499946&longitude=7.684830594&hourly=precipitation_probability,pressure_msl,cloud_cover_low,cloud_cover,cloud_cover_mid,cloud_cover_high,wind_speed_120m,wind_speed_180m,wind_direction_120m,wind_direction_180m,temperature_180m,temperature_120m,temperature_2m,relative_humidity_2m,dew_point_2m,apparent_temperature,precipitation,rain,showers,snowfall,snow_depth,weather_code,surface_pressure,visibility,wind_speed_10m,wind_speed_80m,wind_direction_10m,wind_direction_80m,wind_gusts_10m,temperature_80m,cloud_cover_1000hPa,cloud_cover_975hPa,cloud_cover_950hPa,cloud_cover_925hPa,cloud_cover_900hPa,cloud_cover_850hPa,cloud_cover_800hPa,cloud_cover_700hPa,cloud_cover_600hPa,cloud_cover_500hPa,cloud_cover_400hPa,cloud_cover_300hPa,cloud_cover_200hPa,cloud_cover_250hPa,cloud_cover_150hPa,cloud_cover_100hPa,cloud_cover_70hPa,cloud_cover_50hPa,cloud_cover_30hPa,wind_speed_1000hPa,wind_speed_975hPa,wind_speed_950hPa,wind_speed_925hPa,wind_speed_900hPa,wind_speed_850hPa,wind_speed_800hPa,wind_speed_700hPa,wind_speed_600hPa,wind_direction_600hPa,wind_direction_700hPa,wind_direction_800hPa,wind_direction_850hPa,wind_direction_900hPa,wind_direction_925hPa,wind_direction_950hPa,wind_direction_975hPa,wind_direction_1000hPa,geopotential_height_1000hPa,geopotential_height_975hPa,geopotential_height_950hPa,geopotential_height_925hPa,geopotential_height_900hPa,geopotential_height_850hPa,geopotential_height_800hPa,geopotential_height_700hPa,geopotential_height_600hPa,geopotential_height_500hPa,geopotential_height_400hPa,geopotential_height_300hPa,geopotential_height_250hPa,geopotential_height_200hPa,geopotential_height_150hPa,geopotential_height_100hPa,geopotential_height_70hPa,geopotential_height_50hPa,geopotential_height_30hPa&models=icon_seamless&minutely_15=precipitation,rain,snowfall,weather_code,wind_speed_10m,wind_speed_80m,wind_direction_10m,wind_direction_80m,wind_gusts_10m,visibility,cape,lightning_potential&timezone=auto&wind_speed_unit=kn&forecast_minutely_15=96"
)

// GetWeatherData returns cached data if available and fresh, otherwise fetches new data
func GetWeatherData() (*ProcessedWeatherData, error) {
	cache.mutex.RLock()
	if cache.data != nil && time.Since(cache.timestamp) < cacheDuration {
		defer cache.mutex.RUnlock()
		return cache.data, nil
	}
	cache.mutex.RUnlock()

	// Fetch new data
	return fetchAndCacheWeatherData()
}

// fetchAndCacheWeatherData fetches fresh data from the API and caches it
func fetchAndCacheWeatherData() (*ProcessedWeatherData, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	// Double-check if another goroutine already updated the cache
	if cache.data != nil && time.Since(cache.timestamp) < cacheDuration {
		return cache.data, nil
	}

	fmt.Println("Fetching fresh weather data from API...")

	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch weather data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var apiResponse WeatherAPIResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	processedData := processWeatherData(&apiResponse)

	// Update cache
	cache.data = processedData
	cache.timestamp = time.Now()

	fmt.Printf("Successfully cached weather data with %d temperature points\n", len(processedData.TemperatureData))

	return processedData, nil
}

// processWeatherData converts API response to frontend-friendly format
func processWeatherData(apiResponse *WeatherAPIResponse) *ProcessedWeatherData {
	processed := &ProcessedWeatherData{
		TemperatureData: make([]TemperaturePoint, 0),
		CloudData:       make([]CloudPoint, 0),
		SurfaceWindData: make([]SurfaceWindPoint, 0),
		WindData:        make([]WindPoint, 0),
	}

	// Process temperature and cloud data
	for i, timeStr := range apiResponse.Hourly.Time {
		// Add temperature data
		if i < len(apiResponse.Hourly.Temperature2m) && i < len(apiResponse.Hourly.DewPoint2m) && i < len(apiResponse.Hourly.Precipitation) {
			processed.TemperatureData = append(processed.TemperatureData, TemperaturePoint{
				Time:          timeStr,
				Temperature:   apiResponse.Hourly.Temperature2m[i],
				DewPoint:      apiResponse.Hourly.DewPoint2m[i],
				Precipitation: apiResponse.Hourly.Precipitation[i],
			})
		}

		// Add cloud data - process all hPa levels
		cloudLayers := processCloudLayers(apiResponse, i)
		if len(cloudLayers) > 0 {
			processed.CloudData = append(processed.CloudData, CloudPoint{
				Time:        timeStr,
				CloudLayers: cloudLayers,
			})
		}

		// Add surface wind data (10m and 80m)
		surfaceWindLayers := processSurfaceWindLayers(apiResponse, i)

		// Get 10m wind speed and gusts for line chart
		var windSpeed10m, windGusts10m float64
		if i < len(apiResponse.Hourly.WindSpeed10m) {
			windSpeed10m = apiResponse.Hourly.WindSpeed10m[i]
		}
		if i < len(apiResponse.Hourly.WindGusts10m) {
			windGusts10m = apiResponse.Hourly.WindGusts10m[i]
		}

		processed.SurfaceWindData = append(processed.SurfaceWindData, SurfaceWindPoint{
			Time:         timeStr,
			WindSpeed10m: windSpeed10m,
			WindGusts10m: windGusts10m,
			WindLayers:   surfaceWindLayers,
		})

		// Add wind data - process all levels
		windLayers := processWindLayers(apiResponse, i)
		if len(windLayers) > 0 {
			processed.WindData = append(processed.WindData, WindPoint{
				Time:       timeStr,
				WindLayers: windLayers,
			})
		}
	}

	return processed
}

// processCloudLayers extracts cloud cover data for all hPa levels and converts to layers with heights
func processCloudLayers(apiResponse *WeatherAPIResponse, timeIndex int) []CloudLayer {
	// Define pressure levels and their corresponding cloud cover and geopotential height data
	pressureLevels := []struct {
		CloudCover []int
		GeoHeight  []float64
	}{
		{apiResponse.Hourly.CloudCover1000hPa, apiResponse.Hourly.GeopotentialHeight1000hPa},
		{apiResponse.Hourly.CloudCover975hPa, apiResponse.Hourly.GeopotentialHeight975hPa},
		{apiResponse.Hourly.CloudCover950hPa, apiResponse.Hourly.GeopotentialHeight950hPa},
		{apiResponse.Hourly.CloudCover925hPa, apiResponse.Hourly.GeopotentialHeight925hPa},
		{apiResponse.Hourly.CloudCover900hPa, apiResponse.Hourly.GeopotentialHeight900hPa},
		{apiResponse.Hourly.CloudCover850hPa, apiResponse.Hourly.GeopotentialHeight850hPa},
		{apiResponse.Hourly.CloudCover800hPa, apiResponse.Hourly.GeopotentialHeight800hPa},
		{apiResponse.Hourly.CloudCover700hPa, apiResponse.Hourly.GeopotentialHeight700hPa},
		{apiResponse.Hourly.CloudCover600hPa, apiResponse.Hourly.GeopotentialHeight600hPa},
		{apiResponse.Hourly.CloudCover500hPa, apiResponse.Hourly.GeopotentialHeight500hPa},
		{apiResponse.Hourly.CloudCover400hPa, apiResponse.Hourly.GeopotentialHeight400hPa},
		{apiResponse.Hourly.CloudCover300hPa, apiResponse.Hourly.GeopotentialHeight300hPa},
		{apiResponse.Hourly.CloudCover250hPa, apiResponse.Hourly.GeopotentialHeight250hPa},
		{apiResponse.Hourly.CloudCover200hPa, apiResponse.Hourly.GeopotentialHeight200hPa},
		{apiResponse.Hourly.CloudCover150hPa, apiResponse.Hourly.GeopotentialHeight150hPa},
		{apiResponse.Hourly.CloudCover100hPa, apiResponse.Hourly.GeopotentialHeight100hPa},
		{apiResponse.Hourly.CloudCover70hPa, apiResponse.Hourly.GeopotentialHeight70hPa},
		{apiResponse.Hourly.CloudCover50hPa, apiResponse.Hourly.GeopotentialHeight50hPa},
		{apiResponse.Hourly.CloudCover30hPa, apiResponse.Hourly.GeopotentialHeight30hPa},
	}

	var layers []CloudLayer

	for _, level := range pressureLevels {
		// Check if data is available for this time index
		if timeIndex < len(level.CloudCover) && timeIndex < len(level.GeoHeight) {
			coverage := level.CloudCover[timeIndex]
			geoHeight := level.GeoHeight[timeIndex]

			// Only include layers with some cloud coverage
			if coverage > 0 {
				// Convert geopotential height to meters (geopotential height is already in meters)
				heightMeters := int(geoHeight)

				// Determine cloud symbol based on coverage (divided into eighths)
				symbol := getCloudSymbol(coverage)

				layers = append(layers, CloudLayer{
					HeightMeters: heightMeters,
					Coverage:     coverage,
					Symbol:       symbol,
				})
			}
		}
	}

	return layers
}

// getCloudSymbol returns appropriate cloud symbol based on coverage percentage
func getCloudSymbol(coverage int) string {
	switch {
	case coverage <= 12:
		return "☀" // Clear (0-12%)
	case coverage <= 25:
		return "⛅" // Partly sunny (13-25%)
	case coverage <= 37:
		return "🌤" // Partly cloudy (26-37%)
	case coverage <= 50:
		return "🌥" // Mostly cloudy (38-50%)
	case coverage <= 62:
		return "☁" // Cloudy (51-62%)
	case coverage <= 75:
		return "🌫" // Very cloudy (63-75%)
	case coverage <= 87:
		return "☁☁" // Heavy clouds (76-87%)
	default:
		return "☁☁☁" // Overcast (88-100%)
	}
}

// processWindLayers extracts wind data for hPa levels and converts to layers with heights
func processWindLayers(apiResponse *WeatherAPIResponse, timeIndex int) []WindLayer {
	// Only use hPa-based wind data with geopotential heights
	hPaWindLevels := []struct {
		Speed     []float64
		Direction []int
		GeoHeight []float64
	}{
		{apiResponse.Hourly.WindSpeed1000hPa, apiResponse.Hourly.WindDirection1000hPa, apiResponse.Hourly.GeopotentialHeight1000hPa},
		{apiResponse.Hourly.WindSpeed975hPa, apiResponse.Hourly.WindDirection975hPa, apiResponse.Hourly.GeopotentialHeight975hPa},
		{apiResponse.Hourly.WindSpeed950hPa, apiResponse.Hourly.WindDirection950hPa, apiResponse.Hourly.GeopotentialHeight950hPa},
		{apiResponse.Hourly.WindSpeed925hPa, apiResponse.Hourly.WindDirection925hPa, apiResponse.Hourly.GeopotentialHeight925hPa},
		{apiResponse.Hourly.WindSpeed900hPa, apiResponse.Hourly.WindDirection900hPa, apiResponse.Hourly.GeopotentialHeight900hPa},
		{apiResponse.Hourly.WindSpeed850hPa, apiResponse.Hourly.WindDirection850hPa, apiResponse.Hourly.GeopotentialHeight850hPa},
		{apiResponse.Hourly.WindSpeed800hPa, apiResponse.Hourly.WindDirection800hPa, apiResponse.Hourly.GeopotentialHeight800hPa},
		{apiResponse.Hourly.WindSpeed700hPa, apiResponse.Hourly.WindDirection700hPa, apiResponse.Hourly.GeopotentialHeight700hPa},
		{apiResponse.Hourly.WindSpeed600hPa, apiResponse.Hourly.WindDirection600hPa, apiResponse.Hourly.GeopotentialHeight600hPa},
	}

	var layers []WindLayer

	// Process hPa-based levels only
	for _, level := range hPaWindLevels {
		if timeIndex < len(level.Speed) && timeIndex < len(level.Direction) && timeIndex < len(level.GeoHeight) {
			speed := level.Speed[timeIndex]
			direction := level.Direction[timeIndex]
			geoHeight := level.GeoHeight[timeIndex]

			// Only include if we have valid data and height is within range
			if speed > 0 && geoHeight >= 200 && geoHeight <= 4000 {
				heightMeters := int(geoHeight)
				speedKnots := speed

				symbol := getWindSymbol(speedKnots, direction)

				layers = append(layers, WindLayer{
					HeightMeters: heightMeters,
					Speed:        speedKnots,
					Direction:    direction,
					Symbol:       symbol,
				})
			}
		}
	}

	return layers
}

// processSurfaceWindLayers extracts wind data for fixed heights (10m and 80m)
func processSurfaceWindLayers(apiResponse *WeatherAPIResponse, timeIndex int) []SurfaceWindLayer {
	var layers []SurfaceWindLayer

	// Process 10m wind data
	if timeIndex < len(apiResponse.Hourly.WindSpeed10m) && timeIndex < len(apiResponse.Hourly.WindDirection10m) {
		speed10m := apiResponse.Hourly.WindSpeed10m[timeIndex]
		direction10m := apiResponse.Hourly.WindDirection10m[timeIndex]

		if speed10m > 0 {
			symbol := getWindSymbol(speed10m, direction10m)
			layers = append(layers, SurfaceWindLayer{
				HeightMeters: 10,
				Speed:        speed10m,
				Direction:    direction10m,
				Symbol:       symbol,
			})
		}
	}

	// Process 80m wind data
	if timeIndex < len(apiResponse.Hourly.WindSpeed80m) && timeIndex < len(apiResponse.Hourly.WindDirection80m) {
		speed80m := apiResponse.Hourly.WindSpeed80m[timeIndex]
		direction80m := apiResponse.Hourly.WindDirection80m[timeIndex]

		if speed80m > 0 {
			symbol := getWindSymbol(speed80m, direction80m)
			layers = append(layers, SurfaceWindLayer{
				HeightMeters: 80,
				Speed:        speed80m,
				Direction:    direction80m,
				Symbol:       symbol,
			})
		}
	}

	return layers
}

// getWindSymbol returns wind barb type based on speed (direction handled in frontend)
func getWindSymbol(speedKnots float64, direction int) string {
	// Return barb type based on wind speed for frontend rendering
	if speedKnots < 3 {
		return "calm"
	}
	// Return speed for barb calculation in frontend
	return "barb"
}
