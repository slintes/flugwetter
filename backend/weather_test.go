package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testAirport is EDWN, fixed here rather than read from airports.json so the scoring tests
// keep their expected values when the configured list changes.
var testAirport = Airport{
	Identifier:     "EDWN",
	Name:           "Nordhorn-Lingen",
	Latitude:       52.4575,
	Longitude:      7.1850,
	Runways:        []string{"05/23"},
	RunwayHeadings: []float64{55, 235},
}

// testDayLight is a fixed summer day at EDWN. Times are UTC, matching the naive-UTC
// timestamps Open-Meteo returns under timezone=GMT.
func testDayLight(t *testing.T) *SunriseSunsetResponse {
	t.Helper()

	mustParse := func(s string) time.Time {
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("bad fixture time %q: %v", s, err)
		}
		return ts
	}

	resp := &SunriseSunsetResponse{Status: "OK"}
	resp.Parsed.CivilTwilightBegin = mustParse("2026-08-03T02:40:00Z")
	resp.Parsed.Sunrise = mustParse("2026-08-03T03:30:00Z")
	resp.Parsed.Sunset = mustParse("2026-08-03T19:30:00Z")
	resp.Parsed.CivilTwilightEnd = mustParse("2026-08-03T20:20:00Z")
	return resp
}

// stubDayLight points getDayLightFn at testDayLight and restores the real implementation
// when the test ends.
func stubDayLight(t *testing.T) {
	t.Helper()

	resp := testDayLight(t)
	original := getDayLightFn
	getDayLightFn = func(_ context.Context, latitude, longitude string, _ time.Time) (*SunriseSunsetResponse, error) {
		return resp, nil
	}
	t.Cleanup(func() { getDayLightFn = original })
}

func ptrFloat(v float64) *float64 { return &v }
func ptrInt(v int) *int           { return &v }

// midday is inside both civil twilight and sunrise..sunset, so daylight costs nothing.
const midday = "2026-08-03T12:00"

// calmCAVOK is the baseline input: no cloud base, no wind, mild, dry.
func calmCAVOK() (cloudBase *int, wind, crosswind, gusts float64, temp TemperaturePoint) {
	return nil, 0, 0, 0, TemperaturePoint{Temperature: 18}
}

func TestCalculateVFRProbability_UnknownVisibilityStillScores(t *testing.T) {
	base, wind, xw, gusts, temp := calmCAVOK()

	prob, known := calculateVFRProbability(testDayLight(t), base, wind, xw, gusts, nil, temp, midday)

	if known {
		t.Errorf("visibilityKnown = true, want false when visibility is nil")
	}
	// The regression this guards: nil visibility used to force probability to -1,
	// discarding every other factor for the ~41 tail hours of each forecast.
	if prob != 100 {
		t.Errorf("probability = %d, want 100 (score must survive missing visibility)", prob)
	}
}

func TestCalculateVFRProbability_UnknownVisibilityKeepsOtherPenalties(t *testing.T) {
	// Low ceiling at FL16 costs 25; nothing else applies.
	prob, known := calculateVFRProbability(testDayLight(t), ptrInt(16), 0, 0, 0, nil, TemperaturePoint{Temperature: 18}, midday)

	if known {
		t.Errorf("visibilityKnown = true, want false")
	}
	if prob != 75 {
		t.Errorf("probability = %d, want 75 (cloud base penalty must still apply)", prob)
	}
}

func TestCalculateVFRProbability_KnownVisibility(t *testing.T) {
	base, wind, xw, gusts, temp := calmCAVOK()

	prob, known := calculateVFRProbability(testDayLight(t), base, wind, xw, gusts, ptrFloat(40), temp, midday)

	if !known {
		t.Errorf("visibilityKnown = false, want true when visibility is present")
	}
	if prob != 100 {
		t.Errorf("probability = %d, want 100", prob)
	}
}

func TestCalculateVFRProbability_HardNoGo(t *testing.T) {
	tests := []struct {
		name       string
		cloudBase  *int
		visibility *float64
		timeStr    string
	}{
		{"visibility below 5km", nil, ptrFloat(3), midday},
		{"cloud base below FL10", ptrInt(8), ptrFloat(40), midday},
		{"before civil twilight", nil, ptrFloat(40), "2026-08-03T01:00"},
		{"after civil twilight", nil, ptrFloat(40), "2026-08-03T21:00"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prob, _ := calculateVFRProbability(testDayLight(t), tc.cloudBase, 0, 0, 0, tc.visibility, TemperaturePoint{Temperature: 18}, tc.timeStr)
			if prob != 0 {
				t.Errorf("probability = %d, want 0", prob)
			}
		})
	}
}

func TestCalculateVFRProbability_UnparseableTime(t *testing.T) {
	prob, known := calculateVFRProbability(testDayLight(t), nil, 0, 0, 0, ptrFloat(40), TemperaturePoint{}, "not-a-time")

	if prob != -1 {
		t.Errorf("probability = %d, want -1 for an unscoreable hour", prob)
	}
	if known {
		t.Errorf("visibilityKnown = true, want false when no score was computed")
	}
}

// A date resolveDaylight could not look up is absent from its map, so the hour arrives here
// with a nil window. Scoring it on the remaining factors would report a CAVOK afternoon and
// the middle of the night identically, so it must yield "no data" instead.
func TestCalculateVFRProbability_NoDaylightWindow(t *testing.T) {
	base, wind, xw, gusts, temp := calmCAVOK()

	prob, known := calculateVFRProbability(nil, base, wind, xw, gusts, ptrFloat(40), temp, midday)

	if prob != -1 {
		t.Errorf("probability = %d, want -1 without a daylight window", prob)
	}
	if known {
		t.Errorf("visibilityKnown = true, want false when no score was computed")
	}
}

func TestGetCloudBase(t *testing.T) {
	tests := []struct {
		name   string
		layers []CloudLayer
		want   *int
	}{
		{
			name: "lowest layer at or above 40 percent wins",
			layers: []CloudLayer{
				{HeightFeet: 1000, Coverage: 20},
				{HeightFeet: 2500, Coverage: 60},
				{HeightFeet: 5000, Coverage: 90},
			},
			want: ptrInt(25),
		},
		{
			name:   "no qualifying layer",
			layers: []CloudLayer{{HeightFeet: 1000, Coverage: 39}},
			want:   nil,
		},
		{
			name:   "no layers at all",
			layers: nil,
			want:   nil,
		},
		{
			// A ceiling below 100ft yields FL0. It must be returned, not conflated
			// with "no ceiling" -- it is the most safety-critical case there is.
			name:   "sub-100ft base is FL0, not absent",
			layers: []CloudLayer{{HeightFeet: 50, Coverage: 100}},
			want:   ptrInt(0),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getCloudBase(tc.layers)
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("got FL%d, want nil", *got)
			case tc.want != nil && got == nil:
				t.Errorf("got nil, want FL%d", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Errorf("got FL%d, want FL%d", *got, *tc.want)
			}
		})
	}
}

// hourlyFixture builds a response whose hourly arrays are all the same length, with
// every wind level dead calm so processWindLayers yields nothing.
func hourlyFixture(times []string) *WeatherAPIResponse {
	n := len(times)
	zerosF := make([]float64, n)
	zerosI := make([]int, n)
	visibility := make([]*float64, n)
	for i := range visibility {
		visibility[i] = ptrFloat(30000)
	}

	r := &WeatherAPIResponse{}
	r.Hourly.Time = times
	r.Hourly.Temperature2m = zerosF
	r.Hourly.DewPoint2m = zerosF
	r.Hourly.Precipitation = zerosF
	r.Hourly.PrecipitationProbability = zerosI
	r.Hourly.WeatherCode = zerosI
	r.Hourly.Visibility = visibility
	r.Hourly.WindSpeed10m = zerosF
	r.Hourly.WindGusts10m = zerosF
	r.Hourly.WindDirection10m = zerosI
	return r
}

func TestProcessWeatherData_CalmHourKeepsWindRow(t *testing.T) {
	stubDayLight(t)
	times := []string{"2026-08-03T10:00", "2026-08-03T11:00", "2026-08-03T12:00"}

	got := processWeatherData(context.Background(), hourlyFixture(times), testAirport)

	// The regression: `if len(windLayers) > 0` dropped the entire WindPoint when no
	// level qualified, taking the 10m speed, gusts and both crosswind series with it
	// and leaving a hole the chart's spline smoothed straight across.
	if len(got.WindData) != len(times) {
		t.Fatalf("len(WindData) = %d, want %d — a calm hour must keep its row", len(got.WindData), len(times))
	}
	for i, wp := range got.WindData {
		if wp.Time != times[i] {
			t.Errorf("WindData[%d].Time = %q, want %q", i, wp.Time, times[i])
		}
		if wp.WindLayers == nil {
			t.Errorf("WindData[%d].WindLayers is nil, want an empty slice so it marshals as []", i)
		}
	}
}

func TestProcessWeatherData_SurvivesDaylightLookupFailure(t *testing.T) {
	original := getDayLightFn
	getDayLightFn = func(_ context.Context, latitude, longitude string, _ time.Time) (*SunriseSunsetResponse, error) {
		return nil, errors.New("sunrise API unavailable")
	}
	t.Cleanup(func() { getDayLightFn = original })

	times := []string{"2026-08-03T10:00", "2026-08-03T11:00"}

	// Previously the error was logged and dayLight.Parsed.Sunrise dereferenced anyway.
	// On the startup cache warm that panic takes the whole process down.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("processWeatherData panicked on a daylight lookup failure: %v", r)
		}
	}()
	got := processWeatherData(context.Background(), hourlyFixture(times), testAirport)

	if len(got.VfrData) != len(times) {
		t.Fatalf("len(VfrData) = %d, want %d", len(got.VfrData), len(times))
	}
	for i, vp := range got.VfrData {
		if vp.Probability != -1 {
			t.Errorf("VfrData[%d].Probability = %d, want -1 when the hour cannot be scored", i, vp.Probability)
		}
		if strings.HasSuffix(vp.WeatherCode, "-night") {
			t.Errorf("VfrData[%d].WeatherCode = %q, want no -night suffix from a failed lookup", i, vp.WeatherCode)
		}
	}
}

func TestProcessWeatherData_SeriesStayAligned(t *testing.T) {
	stubDayLight(t)
	times := []string{"2026-08-03T10:00", "2026-08-03T11:00", "2026-08-03T12:00"}

	got := processWeatherData(context.Background(), hourlyFixture(times), testAirport)

	for _, tc := range []struct {
		name string
		n    int
	}{
		{"TemperatureData", len(got.TemperatureData)},
		{"CloudData", len(got.CloudData)},
		{"WindData", len(got.WindData)},
		{"VfrData", len(got.VfrData)},
	} {
		if tc.n != len(times) {
			t.Errorf("len(%s) = %d, want %d", tc.name, tc.n, len(times))
		}
	}
}

// The regression this guards: daylight used to be looked up once per hour from two separate
// call sites -- the -night icon suffix and calculateVFRProbability -- so a 48-hour forecast
// issued 96 lookups. Results were cached by date, but errors were not, so a failing upstream
// produced every one of them as a real serial request. The bound must be the number of
// distinct dates, not the length of the forecast.
func TestProcessWeatherData_ResolvesDaylightOncePerDate(t *testing.T) {
	resp := testDayLight(t)

	var calls int
	original := getDayLightFn
	getDayLightFn = func(_ context.Context, latitude, longitude string, _ time.Time) (*SunriseSunsetResponse, error) {
		calls++
		return resp, nil
	}
	t.Cleanup(func() { getDayLightFn = original })

	// 26 hours spanning two dates.
	var times []string
	for h := 0; h < 24; h++ {
		times = append(times, fmt.Sprintf("2026-08-03T%02d:00", h))
	}
	times = append(times, "2026-08-04T00:00", "2026-08-04T01:00")

	got := processWeatherData(context.Background(), hourlyFixture(times), testAirport)

	if len(got.VfrData) != len(times) {
		t.Fatalf("len(VfrData) = %d, want %d", len(got.VfrData), len(times))
	}
	if calls != 2 {
		t.Errorf("daylight lookups = %d, want 2 (one per distinct date, not one per hour)", calls)
	}
}

// GeneratedAt is what fetchAndCacheWeatherData stores as the cache entry's timestamp, so an
// unset value would make every entry look permanently expired.
func TestProcessWeatherData_SetsGeneratedAt(t *testing.T) {
	stubDayLight(t)

	before := time.Now()
	got := processWeatherData(context.Background(), hourlyFixture([]string{"2026-08-03T10:00"}), testAirport)

	if got.GeneratedAt.Before(before) || got.GeneratedAt.After(time.Now()) {
		t.Errorf("GeneratedAt = %v, want a timestamp from during this call", got.GeneratedAt)
	}
	if got.Stale {
		t.Error("Stale = true on freshly processed data")
	}
}

// stubFetchWeather replaces the upstream weather call for the duration of the test and
// resets the cache, which is package-level state shared between tests.
func stubFetchWeather(t *testing.T, fn func(context.Context, Airport) (*ProcessedWeatherData, error)) {
	t.Helper()

	original := fetchWeatherFn
	fetchWeatherFn = fn
	t.Cleanup(func() {
		fetchWeatherFn = original
		cache.mutex.Lock()
		cache.entries = make(map[string]*cacheEntry)
		cache.mutex.Unlock()
	})

	cache.mutex.Lock()
	cache.entries = make(map[string]*cacheEntry)
	cache.mutex.Unlock()
}

// With upstream down and the TTL expired, a 16-minute-old forecast is far more useful than
// an error page -- but it must arrive flagged, or the dashboard presents it as current.
func TestFetchAndCacheWeatherData_ServesStaleOnFailure(t *testing.T) {
	stubFetchWeather(t, func(context.Context, Airport) (*ProcessedWeatherData, error) {
		return nil, errors.New("open-meteo unreachable")
	})

	expired := &ProcessedWeatherData{
		TemperatureData: []TemperaturePoint{{Time: "2026-08-03T10:00", Temperature: 18}},
		GeneratedAt:     time.Now().Add(-30 * time.Minute),
	}
	cache.mutex.Lock()
	cache.entries[testAirport.Identifier] = &cacheEntry{data: expired, timestamp: expired.GeneratedAt}
	cache.mutex.Unlock()

	got, err := fetchAndCacheWeatherData(context.Background(), testAirport)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Stale {
		t.Error("Stale = false, want true when an expired entry was served")
	}
	if len(got.TemperatureData) != 1 {
		t.Errorf("len(TemperatureData) = %d, want the cached payload's 1", len(got.TemperatureData))
	}
	// The shared cached payload must not have been mutated -- other goroutines hold it.
	if expired.Stale {
		t.Error("the cached payload was mutated; Stale must only be set on the returned copy")
	}
}

// With nothing cached there is nothing to fall back to, and the error has to surface.
func TestFetchAndCacheWeatherData_ErrorsWithoutCache(t *testing.T) {
	stubFetchWeather(t, func(context.Context, Airport) (*ProcessedWeatherData, error) {
		return nil, errors.New("open-meteo unreachable")
	})

	if _, err := fetchAndCacheWeatherData(context.Background(), testAirport); err == nil {
		t.Error("got no error, want one when the fetch fails and the cache is empty")
	}
}

// The fetch runs with no lock held, so a slower one can return after a fresher entry has
// already been stored. It must not clobber it.
func TestFetchAndCacheWeatherData_KeepsFresherEntry(t *testing.T) {
	fresh := &ProcessedWeatherData{GeneratedAt: time.Now()}

	stubFetchWeather(t, func(context.Context, Airport) (*ProcessedWeatherData, error) {
		// Simulates a fetch that started earlier and finished later: while it was in
		// flight, another goroutine stored a newer entry.
		cache.mutex.Lock()
		cache.entries[testAirport.Identifier] = &cacheEntry{data: fresh, timestamp: fresh.GeneratedAt}
		cache.mutex.Unlock()

		return &ProcessedWeatherData{GeneratedAt: time.Now().Add(-time.Minute)}, nil
	})

	got, err := fetchAndCacheWeatherData(context.Background(), testAirport)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != fresh {
		t.Error("the stale in-flight result overwrote a fresher cache entry")
	}
}

// A client that goes away must cancel the upstream call rather than leave it running.
func TestGetJSONHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Never dialled: Do checks the context first, which is exactly what is asserted here.
	if _, err := getJSON(ctx, "http://127.0.0.1:1/unreachable"); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestGetJSON(t *testing.T) {
	t.Run("returns the body on 200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		t.Cleanup(srv.Close)

		body, err := getJSON(context.Background(), srv.URL)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(body) != `{"ok":true}` {
			t.Errorf("body = %q, want {\"ok\":true}", body)
		}
	})

	// Upstream returning 500 with an HTML error page must not be handed to the JSON
	// decoder, where it would surface as a confusing parse error instead of the status.
	t.Run("rejects a non-200 status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream exploded", http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		if _, err := getJSON(context.Background(), srv.URL); err == nil {
			t.Error("got no error, want one for a 500 response")
		} else if !strings.Contains(err.Error(), "500") {
			t.Errorf("error = %v, want it to name the status code", err)
		}
	})

	t.Run("times out rather than hanging", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done() // never responds
		}))
		t.Cleanup(srv.Close)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		if _, err := getJSON(ctx, srv.URL); err == nil {
			t.Error("got no error, want a deadline exceeded")
		}
	})
}

// The layer builders index into ~19 parallel slices per hour. The bounds guards are what
// stop a short or absent slice from panicking, and they are easy to break.
func TestProcessLayers_ToleratesShortSlices(t *testing.T) {
	// One time step, but every level's data is missing entirely.
	response := &WeatherAPIResponse{}
	response.Hourly.Time = []string{"2026-08-03T10:00"}

	t.Run("cloud layers", func(t *testing.T) {
		layers := processCloudLayers(response, 0)
		if layers == nil {
			t.Error("got nil, want an empty slice so it marshals as [] rather than null")
		}
		if len(layers) != 0 {
			t.Errorf("len = %d, want 0 when no level has data", len(layers))
		}
	})

	t.Run("wind layers", func(t *testing.T) {
		layers := processWindLayers(response, 0)
		if layers == nil {
			t.Error("got nil, want an empty slice so it marshals as [] rather than null")
		}
		if len(layers) != 0 {
			t.Errorf("len = %d, want 0 when no level has data", len(layers))
		}
	})

	// An index past the end of every slice must also be safe: the hourly arrays are not
	// guaranteed to be the same length as Time.
	t.Run("index past the end", func(t *testing.T) {
		if got := processCloudLayers(response, 99); len(got) != 0 {
			t.Errorf("cloud layers = %d, want 0", len(got))
		}
		if got := processWindLayers(response, 99); len(got) != 0 {
			t.Errorf("wind layers = %d, want 0", len(got))
		}
	})
}

func TestProcessWindLayers_FiltersAndHeights(t *testing.T) {
	response := &WeatherAPIResponse{}
	response.Hourly.Time = []string{"2026-08-03T10:00"}
	// 10m and 80m carry no geopotential height and fall back to fixed altitudes.
	response.Hourly.WindSpeed10m = []float64{12}
	response.Hourly.WindDirection10m = []int{270}
	response.Hourly.WindSpeed80m = []float64{0} // calm: dropped by the speed > 0 filter
	response.Hourly.WindDirection80m = []int{270}
	// 975hPa well inside range, 600hPa deliberately above the 12000ft ceiling.
	response.Hourly.WindSpeed975hPa = []float64{20}
	response.Hourly.WindDirection975hPa = []int{300}
	response.Hourly.GeopotentialHeight975hPa = []float64{300}
	response.Hourly.WindSpeed600hPa = []float64{40}
	response.Hourly.WindDirection600hPa = []int{310}
	response.Hourly.GeopotentialHeight600hPa = []float64{4400} // ~14435 ft

	layers := processWindLayers(response, 0)

	if len(layers) != 2 {
		t.Fatalf("len(layers) = %d, want 2 (calm 80m dropped, 600hPa above the ceiling)", len(layers))
	}
	// 10m falls back to 10 metres -> 32 ft.
	if layers[0].HeightFeet != 32 {
		t.Errorf("10m HeightFeet = %d, want 32", layers[0].HeightFeet)
	}
	if layers[0].Speed != 12 {
		t.Errorf("10m Speed = %v, want 12", layers[0].Speed)
	}
	// 300 m -> 984 ft.
	if layers[1].HeightFeet != 984 {
		t.Errorf("975hPa HeightFeet = %d, want 984", layers[1].HeightFeet)
	}
	for _, layer := range layers {
		if layer.HeightFeet > 12000 {
			t.Errorf("HeightFeet = %d, want everything above 12000 ft dropped", layer.HeightFeet)
		}
	}
}

func TestSunriseCachePruneLocked(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	c := &SunriseCache{data: map[string]*SunriseSunsetResponse{
		"52.4575_7.1850_2026-07-30": {}, // well past, drop
		"52.4575_7.1850_2026-08-02": {}, // before yesterday, drop
		"52.4575_7.1850_2026-08-03": {}, // yesterday, keep
		"52.4575_7.1850_2026-08-04": {}, // today, keep
		"52.4575_7.1850_2026-08-09": {}, // forecast tail, keep
	}}

	c.pruneLocked(now)

	want := []string{
		"52.4575_7.1850_2026-08-03",
		"52.4575_7.1850_2026-08-04",
		"52.4575_7.1850_2026-08-09",
	}
	if len(c.data) != len(want) {
		t.Fatalf("cache holds %d entries, want %d: %v", len(c.data), len(want), c.data)
	}
	for _, key := range want {
		if _, ok := c.data[key]; !ok {
			t.Errorf("%q was pruned, want kept", key)
		}
	}
}

func TestParseSunriseSunset(t *testing.T) {
	const good = `{"results":{"sunrise":"2026-08-03T03:30:00+00:00",
		"sunset":"2026-08-03T19:30:00+00:00",
		"civil_twilight_begin":"2026-08-03T02:40:00+00:00",
		"civil_twilight_end":"2026-08-03T20:20:00+00:00"},"status":"OK"}`

	t.Run("valid response", func(t *testing.T) {
		got, err := parseSunriseSunset([]byte(good), "2026-08-03")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Parsed.Sunrise.UTC().Hour() != 3 {
			t.Errorf("sunrise hour = %d, want 3", got.Parsed.Sunrise.UTC().Hour())
		}
		if got.Parsed.CivilTwilightEnd.UTC().Hour() != 20 {
			t.Errorf("civil twilight end hour = %d, want 20", got.Parsed.CivilTwilightEnd.UTC().Hour())
		}
	})

	// Each of these used to be accepted, leaving zero times that read as "outside civil
	// twilight" for every hour and zeroed the whole VFR series without a log line.
	bad := []struct {
		name string
		body string
	}{
		{"non-OK status", `{"results":{},"status":"INVALID_REQUEST"}`},
		{"empty results with OK status", `{"results":{},"status":"OK"}`},
		{
			name: "missing civil twilight end",
			body: `{"results":{"sunrise":"2026-08-03T03:30:00+00:00",
				"sunset":"2026-08-03T19:30:00+00:00",
				"civil_twilight_begin":"2026-08-03T02:40:00+00:00",
				"civil_twilight_end":""},"status":"OK"}`,
		},
		{
			name: "unparseable sunrise",
			body: `{"results":{"sunrise":"not a time",
				"sunset":"2026-08-03T19:30:00+00:00",
				"civil_twilight_begin":"2026-08-03T02:40:00+00:00",
				"civil_twilight_end":"2026-08-03T20:20:00+00:00"},"status":"OK"}`,
		},
	}

	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSunriseSunset([]byte(tc.body), "2026-08-03")
			if err == nil {
				t.Fatalf("got no error and result %+v, want an error", got.Parsed)
			}
		})
	}
}

func TestCrosswindComponent(t *testing.T) {
	// EDWN runway 05/23, true headings {55, 235}. See testAirport.
	tests := []struct {
		name      string
		speed     float64
		direction int
		want      float64
	}{
		{"straight down runway 05", 20, 55, 0},
		{"straight down runway 23", 20, 235, 0},
		{"90 degrees off both ends", 20, 145, 20},
		{"calm", 0, 145, 0},
	}

	const tolerance = 0.01
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := testAirport.crosswindComponent(tc.speed, tc.direction)
			if diff := got - tc.want; diff > tolerance || diff < -tolerance {
				t.Errorf("crosswindComponent(%v, %v) = %v, want %v", tc.speed, tc.direction, got, tc.want)
			}
		})
	}
}
