package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type Location struct {
	Latitude  string
	Longitude string
}

var EDDG = Location{
	Latitude:  "52.13499946",
	Longitude: "7.684830594",
}

// WeatherAPIResponse represents the complete API response from open-meteo
type WeatherAPIResponse struct {
	Hourly struct {
		Time                     []string  `json:"time"`
		Temperature2m            []float64 `json:"temperature_2m"`
		DewPoint2m               []float64 `json:"dew_point_2m"`
		PrecipitationProbability []int     `json:"precipitation_probability"`
		CloudCoverLow            []int     `json:"cloud_cover_low"`
		CloudCover               []int     `json:"cloud_cover"`
		CloudCoverMid            []int     `json:"cloud_cover_mid"`
		CloudCoverHigh           []int     `json:"cloud_cover_high"`
		Precipitation            []float64 `json:"precipitation"`
		WindSpeed10m             []float64 `json:"wind_speed_10m"`
		WindDirection10m         []int     `json:"wind_direction_10m"`
		WindGusts10m             []float64 `json:"wind_gusts_10m"`
		WindSpeed80m             []float64 `json:"wind_speed_80m"`
		WindDirection80m         []int     `json:"wind_direction_80m"`
		Pressure                 []float64 `json:"pressure_msl"`
		RelativeHumidity2m       []int     `json:"relative_humidity_2m"`
		Visibility               []float64 `json:"visibility"`

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
	apiURL        = fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s&hourly=precipitation_probability,pressure_msl,cloud_cover_low,cloud_cover,cloud_cover_mid,cloud_cover_high,wind_speed_120m,wind_speed_180m,wind_direction_120m,wind_direction_180m,temperature_180m,temperature_120m,temperature_2m,relative_humidity_2m,dew_point_2m,apparent_temperature,precipitation,rain,showers,snowfall,snow_depth,weather_code,surface_pressure,visibility,wind_speed_10m,wind_speed_80m,wind_direction_10m,wind_direction_80m,wind_gusts_10m,temperature_80m,cloud_cover_1000hPa,cloud_cover_975hPa,cloud_cover_950hPa,cloud_cover_925hPa,cloud_cover_900hPa,cloud_cover_850hPa,cloud_cover_800hPa,cloud_cover_700hPa,cloud_cover_600hPa,cloud_cover_500hPa,cloud_cover_400hPa,cloud_cover_300hPa,cloud_cover_200hPa,cloud_cover_250hPa,cloud_cover_150hPa,cloud_cover_100hPa,cloud_cover_70hPa,cloud_cover_50hPa,cloud_cover_30hPa,wind_speed_1000hPa,wind_speed_975hPa,wind_speed_950hPa,wind_speed_925hPa,wind_speed_900hPa,wind_speed_850hPa,wind_speed_800hPa,wind_speed_700hPa,wind_speed_600hPa,wind_direction_600hPa,wind_direction_700hPa,wind_direction_800hPa,wind_direction_850hPa,wind_direction_900hPa,wind_direction_925hPa,wind_direction_950hPa,wind_direction_975hPa,wind_direction_1000hPa,geopotential_height_1000hPa,geopotential_height_975hPa,geopotential_height_950hPa,geopotential_height_925hPa,geopotential_height_900hPa,geopotential_height_850hPa,geopotential_height_800hPa,geopotential_height_700hPa,geopotential_height_600hPa,geopotential_height_500hPa,geopotential_height_400hPa,geopotential_height_300hPa,geopotential_height_250hPa,geopotential_height_200hPa,geopotential_height_150hPa,geopotential_height_100hPa,geopotential_height_70hPa,geopotential_height_50hPa,geopotential_height_30hPa&models=icon_seamless&minutely_15=precipitation,rain,snowfall,weather_code,wind_speed_10m,wind_speed_80m,wind_direction_10m,wind_direction_80m,wind_gusts_10m,visibility,cape,lightning_potential&timezone=auto&wind_speed_unit=kn&forecast_minutely_15=96", EDDG.Latitude, EDDG.Longitude)
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
		WindData:        make([]WindPoint, 0),
	}

	// Process temperature and cloud data
	for i, timeStr := range apiResponse.Hourly.Time {
		// Add temperature data
		tempPoint := TemperaturePoint{}
		if i < len(apiResponse.Hourly.Temperature2m) && i < len(apiResponse.Hourly.DewPoint2m) && i < len(apiResponse.Hourly.Precipitation) && i < len(apiResponse.Hourly.PrecipitationProbability) {
			tempPoint = TemperaturePoint{
				Time:                     timeStr,
				Temperature:              apiResponse.Hourly.Temperature2m[i],
				DewPoint:                 apiResponse.Hourly.DewPoint2m[i],
				Precipitation:            apiResponse.Hourly.Precipitation[i],
				PrecipitationProbability: apiResponse.Hourly.PrecipitationProbability[i],
			}
			processed.TemperatureData = append(processed.TemperatureData, tempPoint)
		}

		// Add cloud data - process all hPa levels
		cloudLayers := processCloudLayers(apiResponse, i)

		// Get visibility data if available
		var visibility float64
		if i < len(apiResponse.Hourly.Visibility) {
			visibility = apiResponse.Hourly.Visibility[i]
			// Convert visibility from meters to kilometers
			visibility = visibility / 1000
		}

		cloudBase := getCloudBase(cloudLayers)
		// Always include a CloudPoint with visibility data, even if there are no cloud layers
		processed.CloudData = append(processed.CloudData, CloudPoint{
			Time:        timeStr,
			CloudLayers: cloudLayers,
			Visibility:  visibility,
			Base:        cloudBase,
		})

		// Get 10m wind speed and gusts for line chart
		var windSpeed10m, windGusts10m float64
		if i < len(apiResponse.Hourly.WindSpeed10m) {
			windSpeed10m = apiResponse.Hourly.WindSpeed10m[i]
		}
		if i < len(apiResponse.Hourly.WindGusts10m) {
			windGusts10m = apiResponse.Hourly.WindGusts10m[i]
		}

		// Add wind data - process all levels
		windLayers := processWindLayers(apiResponse, i)
		if len(windLayers) > 0 {
			processed.WindData = append(processed.WindData, WindPoint{
				Time:         timeStr,
				WindSpeed10m: windSpeed10m,
				WindGusts10m: windGusts10m,
				WindLayers:   windLayers,
			})
		}

		// Calculate VFR probability
		vfrProbability := calculateVFRProbability(cloudBase, windSpeed10m, visibility, tempPoint, timeStr)
		processed.VfrData = append(processed.VfrData, VfrPoint{
			Time:        timeStr,
			Probability: vfrProbability,
		})

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
			// Convert geopotential height from meters to feet (1 meter = 3.28084 feet)
			heightFeet := int(geoHeight * 3.28084)

			// Only include layers with some cloud coverage (avoid completely transparent symbols)
			if coverage > 0 {
				layers = append(layers, CloudLayer{
					HeightFeet: heightFeet,
					Coverage:   coverage,
				})
			}
		}
	}

	return layers
}

// processWindLayers extracts wind data for hPa levels and converts to layers with heights
func processWindLayers(apiResponse *WeatherAPIResponse, timeIndex int) []WindLayer {
	// Only use hPa-based wind data with geopotential heights
	windLevels := []struct {
		Speed     []float64
		Direction []int
		GeoHeight []float64
	}{
		{apiResponse.Hourly.WindSpeed10m, apiResponse.Hourly.WindDirection10m, []float64{}},
		{apiResponse.Hourly.WindSpeed80m, apiResponse.Hourly.WindDirection80m, []float64{}},
		//{apiResponse.Hourly.WindSpeed1000hPa, apiResponse.Hourly.WindDirection1000hPa, apiResponse.Hourly.GeopotentialHeight1000hPa},
		{apiResponse.Hourly.WindSpeed975hPa, apiResponse.Hourly.WindDirection975hPa, apiResponse.Hourly.GeopotentialHeight975hPa},
		{apiResponse.Hourly.WindSpeed950hPa, apiResponse.Hourly.WindDirection950hPa, apiResponse.Hourly.GeopotentialHeight950hPa},
		{apiResponse.Hourly.WindSpeed925hPa, apiResponse.Hourly.WindDirection925hPa, apiResponse.Hourly.GeopotentialHeight925hPa},
		//{apiResponse.Hourly.WindSpeed900hPa, apiResponse.Hourly.WindDirection900hPa, apiResponse.Hourly.GeopotentialHeight900hPa},
		//{apiResponse.Hourly.WindSpeed850hPa, apiResponse.Hourly.WindDirection850hPa, apiResponse.Hourly.GeopotentialHeight850hPa},
		{apiResponse.Hourly.WindSpeed800hPa, apiResponse.Hourly.WindDirection800hPa, apiResponse.Hourly.GeopotentialHeight800hPa},
		//{apiResponse.Hourly.WindSpeed700hPa, apiResponse.Hourly.WindDirection700hPa, apiResponse.Hourly.GeopotentialHeight700hPa},
		{apiResponse.Hourly.WindSpeed600hPa, apiResponse.Hourly.WindDirection600hPa, apiResponse.Hourly.GeopotentialHeight600hPa},
	}

	var layers []WindLayer

	// Process hPa-based levels only
	for i, level := range windLevels {
		if timeIndex < len(level.Speed) && timeIndex < len(level.Direction) {
			speed := level.Speed[timeIndex]
			direction := level.Direction[timeIndex]

			geoHeight := 0.0
			if timeIndex < len(level.GeoHeight) {
				geoHeight = level.GeoHeight[timeIndex]
			} else if i == 0 {
				geoHeight = 10 // wind 10m
			} else if i == 1 {
				geoHeight = 80 // wind 80m
			}

			// Convert geopotential height from meters to feet (1 meter = 3.28084 feet)
			heightFeet := int(geoHeight * 3.28084)

			// Only include if we have valid data and height is within range (600-12000 feet)
			if speed > 0 && heightFeet <= 12000 {
				speedKnots := speed

				symbol := getWindSymbol(speedKnots, direction)

				layers = append(layers, WindLayer{
					HeightFeet: heightFeet,
					Speed:      speedKnots,
					Direction:  direction,
					Symbol:     symbol,
				})
			}
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

// getCloudBase calculates the cloud base
// Returns the height as flight level if any
func getCloudBase(cloudLayers []CloudLayer) *int {
	// Find the lowest layer with coverage >= 40%
	for _, layer := range cloudLayers {
		if layer.Coverage >= 40 {
			feet := layer.HeightFeet
			fl := feet / 100
			return &fl
		}
	}
	// No cloud base found
	return nil
}

// calculateVFRProbability calculates the VFR probability based on weather conditions
// Returns a percentage value (0-100)
func calculateVFRProbability(cloudBase *int, windSpeed float64, visibility float64, tempPoint TemperaturePoint, timeStr string) int {

	timeStr += ":00Z"
	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		fmt.Printf("failed to parse time: %w", err)
		return 0
	}

	// Start with 100% VFR probability
	probability := 100

	// check daylight
	dayLight, err := getDayLight(EDDG.Latitude, EDDG.Longitude, t)
	if err != nil {
		fmt.Printf("failed to get daylight: %w", err)
		return 0
	}

	// outside civil twilight no go
	if t.Before(dayLight.Parsed.CivilTwilightBegin) || t.After(dayLight.Parsed.CivilTwilightEnd) {
		return 0
	}
	// before sunrise and after sunset reduced...
	if t.Before(dayLight.Parsed.Sunrise) || t.After(dayLight.Parsed.Sunset) {
		probability -= 30
	}

	// Cloud base rules
	if cloudBase != nil {
		// cloud base is flight level!
		if *cloudBase < 10 {
			return 0
		} else if *cloudBase < 15 {
			probability -= 50
		} else if *cloudBase < 20 {
			probability -= 40
		} else if *cloudBase < 25 {
			probability -= 25
		} else if *cloudBase < 30 {
			probability -= 15
		}
	}

	// Wind rules
	// TODO calculate crosswind
	if windSpeed > 20 {
		probability -= int(4 * windSpeed)
	} else if windSpeed > 15 {
		probability -= int(3 * windSpeed)
	} else if windSpeed > 10 {
		probability -= int(2 * windSpeed)
	}

	// Visibility rules
	if visibility < 5 {
		// When visibility below 5km, VFR is 0%
		return 0
	} else if visibility < 10 {
		// When visibility below 10km, subtract 50%
		probability -= 60
	} else if visibility < 20 {
		// When visibility below 20km, subtract 25%
		probability -= 30
	}

	// Precipitation rules
	if tempPoint.Precipitation > 8 && tempPoint.PrecipitationProbability >= 20 {
		probability -= 30
	} else if tempPoint.Precipitation > 4 && tempPoint.PrecipitationProbability >= 40 {
		probability -= 30
	} else if tempPoint.Precipitation > 2 && tempPoint.PrecipitationProbability >= 60 {
		probability -= 30
	} else if tempPoint.Precipitation > 1 && tempPoint.PrecipitationProbability >= 80 {
		probability -= 30
	} else if tempPoint.Precipitation > 0.5 && tempPoint.PrecipitationProbability >= 80 {
		probability -= 10
	} else if tempPoint.Precipitation > 0 {
		probability -= 10
	}

	// Ensure probability is within 0-100 range
	if probability < 0 {
		probability = 0
	} else if probability > 100 {
		probability = 100
	}

	return probability
}

type SunriseSunsetResponse struct {
	Results struct {
		Sunrise                    string `json:"sunrise"`
		Sunset                     string `json:"sunset"`
		SolarNoon                  string `json:"solar_noon"`
		DayLength                  int64  `json:"day_length"`
		CivilTwilight_Begin        string `json:"civil_twilight_begin"`
		CivilTwilight_End          string `json:"civil_twilight_end"`
		NauticalTwilight_Begin     string `json:"nautical_twilight_begin"`
		NauticalTwilight_End       string `json:"nautical_twilight_end"`
		AstronomicalTwilight_Begin string `json:"astronomical_twilight_begin"`
		AstronomicalTwilight_End   string `json:"astronomical_twilight_end"`
	} `json:"results"`
	Parsed struct {
		CivilTwilightBegin time.Time
		CivilTwilightEnd   time.Time
		Sunrise            time.Time
		Sunset             time.Time
	}
	Status string `json:"status"`
}

type SunriseCache struct {
	data  map[string]*SunriseSunsetResponse
	mutex sync.RWMutex
}

var (
	sunriseCache = &SunriseCache{
		data: make(map[string]*SunriseSunsetResponse),
	}
)

func getDayLight(latitude, longitude string, t time.Time) (*SunriseSunsetResponse, error) {

	// Format date as YYYY-MM-DD
	dateStr := t.Format("2006-01-02")

	// Generate cache key
	cacheKey := fmt.Sprintf("%s_%s_%s", latitude, longitude, dateStr)

	// Check cache
	sunriseCache.mutex.RLock()
	if data, ok := sunriseCache.data[cacheKey]; ok {
		sunriseCache.mutex.RUnlock()
		return data, nil
	}
	sunriseCache.mutex.RUnlock()

	url := fmt.Sprintf("https://api.sunrise-sunset.org/json?lat=%s&lng=%s&date=%s&formatted=0",
		latitude, longitude, dateStr)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sunrise/sunset data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var result SunriseSunsetResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	// Parse the result

	if result.Results.Sunrise != "" {
		result.Parsed.Sunrise, err = time.Parse(time.RFC3339, result.Results.Sunrise)
		if err != nil {
			return nil, fmt.Errorf("failed to parse sunrise time: %w", err)
		}
	}

	if result.Results.Sunset != "" {
		result.Parsed.Sunset, err = time.Parse(time.RFC3339, result.Results.Sunset)
		if err != nil {
			return nil, fmt.Errorf("failed to parse sunset time: %w", err)
		}
	}

	if result.Results.CivilTwilight_Begin != "" {
		result.Parsed.CivilTwilightBegin, err = time.Parse(time.RFC3339, result.Results.CivilTwilight_Begin)
		if err != nil {
			return nil, fmt.Errorf("failed to parse civil twilight begin time: %w", err)
		}
	}

	if result.Results.CivilTwilight_End != "" {
		result.Parsed.CivilTwilightEnd, err = time.Parse(time.RFC3339, result.Results.CivilTwilight_End)
		if err != nil {
			return nil, fmt.Errorf("failed to parse civil twilight end time: %w", err)
		}
	}

	// Cache the result
	sunriseCache.mutex.Lock()
	sunriseCache.data[cacheKey] = &result
	sunriseCache.mutex.Unlock()

	return &result, nil
}
