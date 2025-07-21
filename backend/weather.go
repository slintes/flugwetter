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
		CloudCoverLow      []int     `json:"cloud_cover_low"`
		CloudCover         []int     `json:"cloud_cover"`
		CloudCoverMid      []int     `json:"cloud_cover_mid"`
		CloudCoverHigh     []int     `json:"cloud_cover_high"`
		Precipitation      []float64 `json:"precipitation"`
		WindSpeed10m       []float64 `json:"wind_speed_10m"`
		WindDirection10m   []int     `json:"wind_direction_10m"`
		Pressure           []float64 `json:"pressure_msl"`
		RelativeHumidity2m []int     `json:"relative_humidity_2m"`
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
	}

	// Process temperature and cloud data
	for i, timeStr := range apiResponse.Hourly.Time {
		// Add temperature data
		if i < len(apiResponse.Hourly.Temperature2m) {
			processed.TemperatureData = append(processed.TemperatureData, TemperaturePoint{
				Time:        timeStr,
				Temperature: apiResponse.Hourly.Temperature2m[i],
			})
		}

		// Add cloud data
		if i < len(apiResponse.Hourly.CloudCoverLow) &&
			i < len(apiResponse.Hourly.CloudCoverMid) &&
			i < len(apiResponse.Hourly.CloudCoverHigh) {
			processed.CloudData = append(processed.CloudData, CloudPoint{
				Time:           timeStr,
				LowCloudCover:  apiResponse.Hourly.CloudCoverLow[i],
				MidCloudCover:  apiResponse.Hourly.CloudCoverMid[i],
				HighCloudCover: apiResponse.Hourly.CloudCoverHigh[i],
			})
		}
	}

	return processed
}
