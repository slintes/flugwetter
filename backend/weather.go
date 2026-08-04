package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// httpClient is shared by every upstream call. http.DefaultClient has no timeout at any
// layer, so a hung connection to Open-Meteo or sunrise-sunset.org blocked its goroutine
// forever. The timeout covers the whole request including the body read.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// getJSON performs a GET and returns the body, honouring ctx so a client that goes away
// cancels the upstream call instead of leaving it running to completion.
func getJSON(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return body, nil
}

// WeatherAPIResponse represents the complete API response from open-meteo
type WeatherAPIResponse struct {
	Hourly struct {
		Time                     []string   `json:"time"`
		Temperature2m            []float64  `json:"temperature_2m"`
		DewPoint2m               []float64  `json:"dew_point_2m"`
		PrecipitationProbability []int      `json:"precipitation_probability"`
		CloudCoverLow            []int      `json:"cloud_cover_low"`
		CloudCover               []int      `json:"cloud_cover"`
		CloudCoverMid            []int      `json:"cloud_cover_mid"`
		CloudCoverHigh           []int      `json:"cloud_cover_high"`
		Precipitation            []float64  `json:"precipitation"`
		WindSpeed10m             []float64  `json:"wind_speed_10m"`
		WindDirection10m         []int      `json:"wind_direction_10m"`
		WindGusts10m             []float64  `json:"wind_gusts_10m"`
		WindSpeed80m             []float64  `json:"wind_speed_80m"`
		WindDirection80m         []int      `json:"wind_direction_80m"`
		Pressure                 []float64  `json:"pressure_msl"`
		RelativeHumidity2m       []int      `json:"relative_humidity_2m"`
		Visibility               []*float64 `json:"visibility,omitempty"`
		WeatherCode              []int      `json:"weather_code,omitempty"`

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

// cacheEntry is one airport's cached payload.
type cacheEntry struct {
	data      *ProcessedWeatherData
	timestamp time.Time
}

// WeatherCache manages cached weather data, one entry per airport identifier.
type WeatherCache struct {
	entries map[string]*cacheEntry
	mutex   sync.RWMutex
}

var (
	cache = &WeatherCache{
		entries: make(map[string]*cacheEntry),
	}
	cacheDuration = 15 * time.Minute
	// apiURLTemplate takes latitude and longitude; everything else about the query is
	// identical for every airport.
	//
	// Every variable here is decoded by WeatherAPIResponse. The query previously also asked
	// for these, which nothing ever read -- they inflated the upstream response and the
	// parse for nothing. Kept as a list rather than deleted outright, because re-enabling
	// one is then a matter of adding it back here and declaring the field:
	//
	//   apparent_temperature, convective_cloud_base, rain, showers, snowfall, snow_depth,
	//   surface_pressure, temperature_80m, temperature_120m, temperature_180m,
	//   wind_speed_120m, wind_speed_180m, wind_direction_120m, wind_direction_180m
	//
	// Note the reverse also holds: a variable removed from this query while its field stays
	// declared makes TestGoldenFixture_EveryHourlyFieldBinds fail, which is the point.
	apiURLTemplate = "https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s&hourly=precipitation_probability,pressure_msl,cloud_cover_low,cloud_cover,cloud_cover_mid,cloud_cover_high,temperature_2m,relative_humidity_2m,dew_point_2m,precipitation,weather_code,visibility,wind_speed_10m,wind_speed_80m,wind_direction_10m,wind_direction_80m,wind_gusts_10m,cloud_cover_1000hPa,cloud_cover_975hPa,cloud_cover_950hPa,cloud_cover_925hPa,cloud_cover_900hPa,cloud_cover_850hPa,cloud_cover_800hPa,cloud_cover_700hPa,cloud_cover_600hPa,cloud_cover_500hPa,cloud_cover_400hPa,cloud_cover_300hPa,cloud_cover_200hPa,cloud_cover_250hPa,cloud_cover_150hPa,cloud_cover_100hPa,cloud_cover_70hPa,cloud_cover_50hPa,cloud_cover_30hPa,wind_speed_1000hPa,wind_speed_975hPa,wind_speed_950hPa,wind_speed_925hPa,wind_speed_900hPa,wind_speed_850hPa,wind_speed_800hPa,wind_speed_700hPa,wind_speed_600hPa,wind_direction_600hPa,wind_direction_700hPa,wind_direction_800hPa,wind_direction_850hPa,wind_direction_900hPa,wind_direction_925hPa,wind_direction_950hPa,wind_direction_975hPa,wind_direction_1000hPa,geopotential_height_1000hPa,geopotential_height_975hPa,geopotential_height_950hPa,geopotential_height_925hPa,geopotential_height_900hPa,geopotential_height_850hPa,geopotential_height_800hPa,geopotential_height_700hPa,geopotential_height_600hPa,geopotential_height_500hPa,geopotential_height_400hPa,geopotential_height_300hPa,geopotential_height_250hPa,geopotential_height_200hPa,geopotential_height_150hPa,geopotential_height_100hPa,geopotential_height_70hPa,geopotential_height_50hPa,geopotential_height_30hPa&models=icon_seamless&timezone=GMT&wind_speed_unit=kn"
)

// buildAPIURL returns the Open-Meteo query for one airport.
func buildAPIURL(airport Airport) string {
	return fmt.Sprintf(apiURLTemplate, airport.LatString(), airport.LonString())
}

// GetWeatherData returns cached data for the given airport if available and fresh,
// otherwise fetches new data.
func GetWeatherData(ctx context.Context, airport Airport) (*ProcessedWeatherData, error) {
	cache.mutex.RLock()
	entry, ok := cache.entries[airport.Identifier]
	cache.mutex.RUnlock()

	if ok && time.Since(entry.timestamp) < cacheDuration {
		return entry.data, nil
	}

	// Fetch new data
	return fetchAndCacheWeatherData(ctx, airport)
}

// cachedEntry returns the stored entry for an airport regardless of its age.
func cachedEntry(identifier string) (*cacheEntry, bool) {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	entry, ok := cache.entries[identifier]
	return entry, ok
}

// fetchAndCacheWeatherData fetches fresh data from the API and caches it.
//
// The upstream call deliberately runs with no lock held. Holding the cache mutex across it
// blocked every other airport -- including warm cache hits that needed no network at all --
// behind one slow Open-Meteo request. The tradeoff is that two simultaneous requests for the
// same cold airport may both fetch; the double-check below makes the loser discard its
// result rather than overwrite a fresher entry.
func fetchAndCacheWeatherData(ctx context.Context, airport Airport) (*ProcessedWeatherData, error) {
	slog.Info("fetching fresh weather data", "airport", airport.Identifier)

	processedData, err := fetchWeatherFn(ctx, airport)
	if err != nil {
		// Forecast data ages gracefully, so an expired entry beats no data at all when
		// upstream is unreachable. It is flagged rather than passed off as current: for a
		// flight-planning tool, silently showing stale weather is the worse failure.
		if entry, ok := cachedEntry(airport.Identifier); ok {
			slog.Warn("serving stale weather data",
				"airport", airport.Identifier,
				"age", time.Since(entry.timestamp).Round(time.Minute),
				"error", err)

			// A shallow copy: the cached payload is shared with other goroutines and must
			// not be mutated. Only the flag differs, and the slices are never written to.
			stale := *entry.data
			stale.Stale = true
			return &stale, nil
		}
		return nil, err
	}

	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	// Another goroutine may have stored a fresher entry while this fetch was in flight.
	if entry, ok := cache.entries[airport.Identifier]; ok && entry.timestamp.After(processedData.GeneratedAt) {
		return entry.data, nil
	}

	cache.entries[airport.Identifier] = &cacheEntry{
		data:      processedData,
		timestamp: processedData.GeneratedAt,
	}

	slog.Info("cached weather data", "airport", airport.Identifier, "points", len(processedData.TemperatureData))

	return processedData, nil
}

// fetchWeatherFn indirects fetchWeather so tests can stub the network call, as
// getDayLightFn does for the sunrise API.
var fetchWeatherFn = fetchWeather

// fetchWeather retrieves and processes one airport's forecast without touching the cache.
func fetchWeather(ctx context.Context, airport Airport) (*ProcessedWeatherData, error) {
	body, err := getJSON(ctx, buildAPIURL(airport))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch weather data: %w", err)
	}

	var apiResponse WeatherAPIResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	return processWeatherData(ctx, &apiResponse, airport), nil
}

// hourTime parses one of Open-Meteo's naive-UTC hourly timestamps ("2026-08-03T12:00").
func hourTime(timeStr string) (time.Time, error) {
	return time.Parse(time.RFC3339, timeStr+":00Z")
}

// resolveDaylight looks up the daylight windows for every distinct date in the forecast,
// keyed by "YYYY-MM-DD".
//
// This used to happen per hour, from two separate call sites -- the -night icon suffix and
// calculateVFRProbability -- which is ~336 lookups for a 7-day forecast. Results were cached
// by date so a healthy upstream only saw ~7 requests, but errors were never cached, so a
// failing sunrise-sunset.org produced 336 serial requests instead. Resolving up front bounds
// the failure path by the number of days rather than the length of the forecast.
//
// A date that could not be resolved is absent from the map. Callers treat that the same way
// they treated a failed lookup before: the icon keeps its daytime variant and the hour scores
// -1, rather than taking the process down with a nil dereference.
func resolveDaylight(ctx context.Context, airport Airport, times []string) map[string]*SunriseSunsetResponse {
	daylight := make(map[string]*SunriseSunsetResponse)

	for _, timeStr := range times {
		t, err := hourTime(timeStr)
		if err != nil {
			// Reported where the hour is scored; nothing to look up for it here.
			continue
		}

		date := t.Format("2006-01-02")
		if _, ok := daylight[date]; ok {
			continue
		}

		dayLight, err := getDayLightFn(ctx, airport.LatString(), airport.LonString(), t)
		if err != nil {
			slog.Error("failed to get daylight", "date", date, "error", err)
			continue
		}
		daylight[date] = dayLight
	}

	return daylight
}

// processWeatherData converts API response to frontend-friendly format
func processWeatherData(ctx context.Context, apiResponse *WeatherAPIResponse, airport Airport) *ProcessedWeatherData {
	processed := &ProcessedWeatherData{
		TemperatureData: make([]TemperaturePoint, 0),
		CloudData:       make([]CloudPoint, 0),
		WindData:        make([]WindPoint, 0),
		GeneratedAt:     time.Now(),
	}

	// One lookup per date, before the loop, rather than two per hour inside it.
	daylight := resolveDaylight(ctx, airport, apiResponse.Hourly.Time)

	// Process temperature and cloud data
	for i, timeStr := range apiResponse.Hourly.Time {
		// The daylight window covering this hour, or nil if the date could not be resolved.
		var hourDaylight *SunriseSunsetResponse
		if t, err := hourTime(timeStr); err == nil {
			hourDaylight = daylight[t.Format("2006-01-02")]
		}

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
		var visibility *float64 = nil
		if i < len(apiResponse.Hourly.Visibility) && apiResponse.Hourly.Visibility[i] != nil {
			// Convert visibility from meters to kilometers
			v := *apiResponse.Hourly.Visibility[i]
			v = v / 1000
			visibility = &v
		}

		cloudBase := getCloudBase(cloudLayers)
		// Always include a CloudPoint with visibility data, even if there are no cloud layers
		processed.CloudData = append(processed.CloudData, CloudPoint{
			Time:        timeStr,
			CloudLayers: cloudLayers,
			Visibility:  visibility,
			Base:        cloudBase,
		})

		// Get 10m wind speed, gusts and direction for line chart
		var windSpeed10m, windGusts10m float64
		var windDirection10m int
		if i < len(apiResponse.Hourly.WindSpeed10m) {
			windSpeed10m = apiResponse.Hourly.WindSpeed10m[i]
		}
		if i < len(apiResponse.Hourly.WindGusts10m) {
			windGusts10m = apiResponse.Hourly.WindGusts10m[i]
		}
		if i < len(apiResponse.Hourly.WindDirection10m) {
			windDirection10m = apiResponse.Hourly.WindDirection10m[i]
		}

		// Crosswind components for the runway in use
		crosswind10m := airport.crosswindComponent(windSpeed10m, windDirection10m)
		crosswindGusts10m := airport.crosswindComponent(windGusts10m, windDirection10m)

		// Add wind data - process all levels.
		// Always emit a WindPoint, even when no level qualified: the 10m speed, gusts
		// and crosswind are independent of the layer barbs, and dropping the whole hour
		// punched a hole in all five wind series that the chart's spline then smoothed
		// straight across.
		processed.WindData = append(processed.WindData, WindPoint{
			Time:              timeStr,
			WindSpeed10m:      windSpeed10m,
			WindGusts10m:      windGusts10m,
			Crosswind10m:      crosswind10m,
			CrosswindGusts10m: crosswindGusts10m,
			WindLayers:        processWindLayers(apiResponse, i),
		})

		// Calculate VFR probability
		vfrProbability, visibilityKnown := calculateVFRProbability(hourDaylight, cloudBase, windSpeed10m, crosswind10m, crosswindGusts10m, visibility, tempPoint, timeStr)

		// Get weather code if available
		processWeatherCode := ""
		if i < len(apiResponse.Hourly.WeatherCode) {
			processWeatherCode = strconv.Itoa(apiResponse.Hourly.WeatherCode[i])

			// is daylight? An unresolved date leaves hourDaylight nil, and the icon simply
			// keeps its daytime variant rather than taking the process down.
			if hourDaylight != nil {
				if t, err := hourTime(timeStr); err == nil {
					slog.Debug("hour", "time", t)
					if !(t.After(hourDaylight.Parsed.Sunrise) && t.Before(hourDaylight.Parsed.Sunset)) {
						processWeatherCode += "-night"
					}
				}
			}
		}
		processed.VfrData = append(processed.VfrData, VfrPoint{
			Time:            timeStr,
			Probability:     vfrProbability,
			WeatherCode:     processWeatherCode,
			VisibilityKnown: visibilityKnown,
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

	// Non-nil so an overcast-free hour marshals as [] rather than null.
	layers := make([]CloudLayer, 0)

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

	// Non-nil so a calm hour marshals as [] rather than null.
	layers := make([]WindLayer, 0)

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
				layers = append(layers, WindLayer{
					HeightFeet: heightFeet,
					Speed:      speed,
					Direction:  direction,
				})
			}
		}
	}

	return layers
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

// calculateVFRProbability calculates the VFR probability based on weather conditions.
// Returns a percentage value (0-100), or -1 when no score could be computed at all.
//
// visibilityKnown reports whether the model supplied a visibility for this hour.
// Open-Meteo drops visibility beyond the ICON-EU horizon, which is the tail of every
// forecast. Those hours are still scored on the factors that are available; the caller
// is expected to present them as estimates rather than as hard numbers.
//
// dayLight is the window for this hour's date, resolved once per date by resolveDaylight.
// A nil dayLight means the lookup failed and the hour cannot be scored at all.
func calculateVFRProbability(dayLight *SunriseSunsetResponse, cloudBase *int, windSpeed, crosswind, crosswindGusts float64, visibility *float64, tempPoint TemperaturePoint, timeStr string) (probability int, visibilityKnown bool) {

	debugProb := func(reason string, value string) {
		slog.Debug("vfr penalty applied", "reason", reason, "value", value)
	}

	t, err := hourTime(timeStr)
	if err != nil {
		slog.Error("failed to parse time", "time", timeStr, "error", err)
		return -1, false
	}

	// Without a daylight window there is no way to tell a CAVOK afternoon from the middle
	// of the night, so the hour scores -1 ("no data") rather than a misleading number.
	if dayLight == nil {
		return -1, false
	}

	// Start with 100% VFR probability
	probability = 100
	visibilityKnown = visibility != nil

	slog.Debug("scoring vfr probability", "hour", t.Format(time.RFC822))

	// outside civil twilight no go
	if t.Before(dayLight.Parsed.CivilTwilightBegin) || t.After(dayLight.Parsed.CivilTwilightEnd) {
		debugProb("outside civil twilight", "0")
		return 0, visibilityKnown
	}
	// before sunrise and after sunset reduced...
	if t.Before(dayLight.Parsed.Sunrise) || t.After(dayLight.Parsed.Sunset) {
		debugProb("outside sunlight", "-30")
		probability -= 30
	}

	// Cloud base rules
	if cloudBase != nil {
		// cloud base is flight level!
		if *cloudBase < 10 {
			debugProb("cloudbase <1000ft", "0")
			return 0, visibilityKnown
		} else if *cloudBase < 15 {
			debugProb("cloudbase <1500ft", "-50")
			probability -= 50
		} else if *cloudBase < 20 {
			debugProb("cloudbase <2000ft", "-25")
			probability -= 25
		} else if *cloudBase < 25 {
			debugProb("cloudbase <2500ft", "-10")
			probability -= 10
		} else if *cloudBase < 30 {
			debugProb("cloudbase <3000ft", "-5")
			probability -= 5
		}
	}

	// wind
	sigWind := int(windSpeed) - 10
	if sigWind > 0 {
		reduce := 0
		if sigWind > 15 {
			reduce = 3 * sigWind
		} else if sigWind > 10 {
			reduce = 2 * sigWind
		} else {
			reduce = sigWind
		}
		debugProb("windspeed > 10", fmt.Sprintf("-%d", reduce))
		probability -= reduce
	}

	// crosswind
	sigCrossWind := int(crosswind) - 5
	if sigCrossWind > 0 {
		reduce := 0
		if sigCrossWind > 10 {
			reduce = 5 * sigCrossWind
		} else if sigCrossWind > 5 {
			reduce = 2 * sigCrossWind
		} else {
			reduce = sigCrossWind
		}
		debugProb("cross wind > 5", fmt.Sprintf("-%d", reduce))
		probability -= reduce
	}

	// crosswind gusts
	sigCrossWindGusts := int(crosswindGusts-crosswind) - 3
	if sigCrossWindGusts > 0 {
		reduce := 0
		if sigCrossWindGusts > 7 {
			reduce = 20
		} else if sigCrossWindGusts > 2 {
			reduce = 10
		} else {
			reduce = 5
		}
		debugProb("cross wind gusts", fmt.Sprintf("-%d", reduce))
		probability -= reduce
	}

	// Visibility rules
	if visibility != nil {
		if *visibility < 5 {
			// When visibility below 5km, VFR is 0%
			debugProb("visibility < 5km", "0")
			return 0, visibilityKnown
		} else if *visibility < 10 {
			debugProb("visibility < 10km", "-50")
			probability -= 50
		} else if *visibility < 20 {
			debugProb("visibility < 20km", "-20")
			probability -= 20
		} else if *visibility < 30 {
			debugProb("visibility < 30km", "-10")
			probability -= 10
		}
	} else {
		debugProb("no visibility avail", "")
	}

	// Precipitation rules
	switch {
	case tempPoint.Precipitation >= 8:
		debugProb("precipitation >= 8", "-25")
		probability -= 25
	case tempPoint.Precipitation >= 4:
		debugProb("precipitation >= 4", "-20")
		probability -= 20
	case tempPoint.Precipitation >= 2:
		debugProb("precipitation >= 2", "-15")
		probability -= 15
	case tempPoint.Precipitation >= 1:
		debugProb("precipitation >= 1", "-10")
		probability -= 10
	case tempPoint.Precipitation > 0:
		debugProb("precipitation > 0", "-5")
		probability -= 5
	}

	if tempPoint.Precipitation >= 2 {
		switch {
		case tempPoint.PrecipitationProbability >= 80:
			debugProb("precipitation >= 2, prob >= 80", "-20")
			probability -= 20
		case tempPoint.PrecipitationProbability >= 60:
			debugProb("precipitation >= 2, prob >= 60", "-15")
			probability -= 15
		case tempPoint.PrecipitationProbability >= 40:
			debugProb("precipitation >= 2, prob >= 40", "-10")
			probability -= 10
		}
	}

	// high tmp > 28
	tmp := int(tempPoint.Temperature) - 28
	if tmp > 0 {
		reduce := tmp * 3
		debugProb("high temperature", fmt.Sprintf("-%d", reduce))
		probability -= reduce
	}

	// Ensure probability is within 0 - 100 range. An hour without visibility keeps its
	// score -- the remaining factors are still meaningful -- and is flagged instead.
	if probability < 0 {
		probability = 0
	} else if probability > 100 {
		probability = 100
	}

	return probability, visibilityKnown
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

// pruneLocked drops entries for dates before yesterday. Daylight data is static per day, so
// once a date has passed its entry can never be needed again. Without this the map grew by
// (airports x days) for the life of the process, with nothing ever removed.
//
// The caller must hold the write lock. Keys are "lat_lon_YYYY-MM-DD", and an ISO date
// compares correctly as a string.
func (c *SunriseCache) pruneLocked(now time.Time) {
	cutoff := now.AddDate(0, 0, -1).Format("2006-01-02")
	for key := range c.data {
		i := strings.LastIndex(key, "_")
		if i < 0 {
			continue
		}
		if key[i+1:] < cutoff {
			delete(c.data, key)
		}
	}
}

// getDayLightFn indirects getDayLight so tests can stub the network call.
var getDayLightFn = getDayLight

func getDayLight(ctx context.Context, latitude, longitude string, t time.Time) (*SunriseSunsetResponse, error) {

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

	body, err := getJSON(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sunrise/sunset data: %w", err)
	}

	result, err := parseSunriseSunset(body, dateStr)
	if err != nil {
		return nil, err
	}

	// Cache the result
	sunriseCache.mutex.Lock()
	sunriseCache.data[cacheKey] = result
	sunriseCache.pruneLocked(time.Now())
	sunriseCache.mutex.Unlock()

	return result, nil
}

// parseSunriseSunset decodes a sunrise-sunset.org response and requires every field the
// VFR calculation depends on.
//
// Previously an empty field was skipped, leaving its Parsed value at the zero time
// (year 1). Every hour then compared as "after civil twilight end", which silently drove
// the entire VFR series to 0 and suffixed every icon with -night — with nothing in the
// log to say why, because `status` was decoded but never read. A degraded upstream must
// fail loudly instead: calculateVFRProbability turns the error into -1, which the
// frontend renders as "no data" rather than as a forecast of universally bad weather.
func parseSunriseSunset(body []byte, dateStr string) (*SunriseSunsetResponse, error) {
	var result SunriseSunsetResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	if result.Status != "OK" {
		return nil, fmt.Errorf("sunrise API returned status %q for %s", result.Status, dateStr)
	}

	for _, f := range []struct {
		name string
		raw  string
		dst  *time.Time
	}{
		{"sunrise", result.Results.Sunrise, &result.Parsed.Sunrise},
		{"sunset", result.Results.Sunset, &result.Parsed.Sunset},
		{"civil twilight begin", result.Results.CivilTwilight_Begin, &result.Parsed.CivilTwilightBegin},
		{"civil twilight end", result.Results.CivilTwilight_End, &result.Parsed.CivilTwilightEnd},
	} {
		if f.raw == "" {
			return nil, fmt.Errorf("sunrise API returned no %s for %s", f.name, dateStr)
		}
		parsed, err := time.Parse(time.RFC3339, f.raw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s time %q: %w", f.name, f.raw, err)
		}
		*f.dst = parsed
	}

	return &result, nil
}
