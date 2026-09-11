package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"sync"
	"sync/atomic"
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
		if vp.Rating != "" {
			t.Errorf("VfrData[%d].Rating = %q, want empty when the hour cannot be scored", i, vp.Rating)
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

// Precipitation is the only scaled factor, and neither the golden fixture nor a typical
// summer forecast contains a drop of rain -- so without this the path from the decoded hour
// through conditions to a scaled VfrFactor is never walked end to end.
func TestProcessWeatherData_ScaledFactorReachesTheWireFormat(t *testing.T) {
	stubDayLight(t)

	fixture := hourlyFixture([]string{"2026-08-03T12:00"})
	fixture.Hourly.Precipitation = []float64{3.2}
	fixture.Hourly.PrecipitationProbability = []int{88}

	got := processWeatherData(context.Background(), fixture, testAirport)

	if len(got.VfrData) != 1 {
		t.Fatalf("len(VfrData) = %d, want 1", len(got.VfrData))
	}
	// The fixture's other fields are not all free -- its visibility is merely good, not
	// perfect -- so pick the factor under test out rather than assuming it is alone.
	var rain *VfrFactor
	for i, f := range got.VfrData[0].Factors {
		if f.Factor == "precipitation" {
			rain = &got.VfrData[0].Factors[i]
		}
	}
	if rain == nil {
		t.Fatalf("Factors = %+v, want one for precipitation", got.VfrData[0].Factors)
	}

	if rain.Value != 3.2 || rain.Unit != "mm/h" {
		t.Errorf("factor = %+v, want the amount in mm/h, unscaled", *rain)
	}
	if rain.Scale == nil {
		t.Fatal("Scale = nil, want the probability that discounted it")
	}
	if rain.Scale.Value != 88 {
		t.Errorf("Scale.Value = %v, want 88 -- the hour's probability must reach the rating",
			rain.Scale.Value)
	}
	// At 88% the discount weight has already reached 1, so the raw 3.2mm/h is read as-is;
	// it sits in (1.0, 4.0], the segment climbing toward the critical anchor.
	if rain.Severity != critical.String() {
		t.Errorf("Severity = %q, want %q", rain.Severity, critical.String())
	}
	if got.VfrData[0].Rating != critical.String() {
		t.Errorf("Rating = %q, want %q (precipitation is the worst factor this hour)",
			got.VfrData[0].Rating, critical.String())
	}
}

// Parsing the hour is the caller's job -- scoreVFR takes a time.Time -- so an upstream
// timestamp in an unexpected shape has to leave the hour unscored here rather than being
// scored against a zero time, which would fall outside every daylight window and read as
// a confident 0% instead of "no data".
func TestProcessWeatherData_UnparseableHourIsNotScored(t *testing.T) {
	stubDayLight(t)

	got := processWeatherData(context.Background(), hourlyFixture([]string{"not-a-time"}), testAirport)

	if len(got.VfrData) != 1 {
		t.Fatalf("len(VfrData) = %d, want 1", len(got.VfrData))
	}
	if got.VfrData[0].Rating != "" {
		t.Errorf("Rating = %q, want empty for an unscoreable hour", got.VfrData[0].Rating)
	}
	if got.VfrData[0].VisibilityKnown {
		t.Error("VisibilityKnown = true, want false when no score was computed")
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

// Concurrent requests for one cold airport must coalesce into a single upstream fetch.
// cache.invalidateAll drops every airport at once, so a burst at that moment is the case
// this guards.
func TestFetchAndCacheWeatherData_CoalescesConcurrentCallers(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	stubFetchWeather(t, func(context.Context, Airport) (*ProcessedWeatherData, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		return &ProcessedWeatherData{GeneratedAt: time.Now()}, nil
	})

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := fetchAndCacheWeatherData(context.Background(), testAirport)
			errs[i] = err
		}(i)
	}

	time.Sleep(20 * time.Millisecond) // let every goroutine reach the shared call
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("fetchWeatherFn called %d times, want 1", got)
	}
	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d: unexpected error: %v", i, err)
		}
	}
}

// One caller's context being cancelled must not fail the other callers waiting on the same
// coalesced fetch -- the leader's context is replaced with context.WithoutCancel precisely
// so that a disconnected client cannot take the shared call down with it.
func TestFetchAndCacheWeatherData_OneCallerCancellingDoesNotFailTheOthers(t *testing.T) {
	release := make(chan struct{})
	stubFetchWeather(t, func(ctx context.Context, _ Airport) (*ProcessedWeatherData, error) {
		<-release
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &ProcessedWeatherData{GeneratedAt: time.Now()}, nil
	})

	cancelledCtx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	var survivorErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = fetchAndCacheWeatherData(cancelledCtx, testAirport)
	}()
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond) // let the first call become the singleflight leader
		_, survivorErr = fetchAndCacheWeatherData(context.Background(), testAirport)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()
	close(release)
	wg.Wait()

	if survivorErr != nil {
		t.Errorf("a sibling caller's cancellation failed this one: %v", survivorErr)
	}
}

func TestRedactedError_StripsSensitiveQueryParams(t *testing.T) {
	original := "https://api.tiles.openaip.net/tile.png?apiKey=SECRET123&z=7"
	err := &neturl.Error{Op: "Get", URL: original, Err: errors.New("i/o timeout")}

	got := redactedError(err)

	if strings.Contains(got, "SECRET123") {
		t.Errorf("redactedError(%v) = %q, still contains the key", err, got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Errorf("redactedError(%v) = %q, want it to say REDACTED", err, got)
	}
	if !strings.Contains(got, "z=7") {
		t.Errorf("redactedError(%v) = %q, dropped a harmless parameter it should have kept", err, got)
	}
}

func TestRedactedError_LeavesAnErrorWithNoSensitiveParamsUnchanged(t *testing.T) {
	err := &neturl.Error{Op: "Get", URL: "https://api.open-meteo.com/v1/forecast?latitude=52.7", Err: errors.New("timeout")}

	if got := redactedError(err); got != err.Error() {
		t.Errorf("redactedError(%v) = %q, want %q unchanged", err, got, err.Error())
	}
}

func TestRedactedError_NonURLErrorPassesThrough(t *testing.T) {
	err := errors.New("some other failure")
	if got := redactedError(err); got != err.Error() {
		t.Errorf("redactedError(%v) = %q, want %q unchanged", err, got, err.Error())
	}
}

func TestRedactedError_Nil(t *testing.T) {
	if got := redactedError(nil); got != "" {
		t.Errorf("redactedError(nil) = %q, want empty", got)
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

// gustWarning is judged on absolute readings, not the crosswind-style margin the rest of
// the wind scoring uses, and each threshold is exclusive at its own boundary -- exactly at
// the limit is not yet a warning, matching how vfrLimits treats a wall.
func TestGustWarning(t *testing.T) {
	tests := []struct {
		name           string
		windGusts      float64
		crosswindGusts float64
		want           bool
	}{
		{"calm", 0, 0, false},
		{"exactly at the wind gust limit", 20, 0, false},
		{"just past the wind gust limit", 20.1, 0, true},
		{"exactly at the crosswind gust limit", 0, 10, false},
		{"just past the crosswind gust limit", 0, 10.1, true},
		{"both past their own limit", 25, 15, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := gustWarning(tc.windGusts, tc.crosswindGusts); got != tc.want {
				t.Errorf("gustWarning(%v, %v) = %v, want %v", tc.windGusts, tc.crosswindGusts, got, tc.want)
			}
		})
	}
}

// The warning has to reach the wire independent of the rating -- it is judged on absolute
// gust readings, not the crosswind-margin factors that feed scoreVFR.
func TestProcessWeatherData_GustWarningReachesTheWireFormat(t *testing.T) {
	stubDayLight(t)

	// Direction 145 is 90 degrees off testAirport's runway (see TestCrosswindComponent),
	// so crosswindComponent returns the input speed unchanged -- crosswind gusts equal
	// wind gusts exactly, which is what lets each case move one threshold at a time.
	// Direction 55 is straight down the runway, where the crosswind component is 0.
	times := []string{"calm", "wind gusts alone", "crosswind gusts alone", "both"}
	fixture := hourlyFixture(times)
	fixture.Hourly.Time = []string{
		"2026-08-03T10:00", "2026-08-03T11:00", "2026-08-03T12:00", "2026-08-03T13:00",
	}
	fixture.Hourly.WindGusts10m = []float64{0, 25, 15, 25}
	fixture.Hourly.WindDirection10m = []int{145, 55, 145, 145}

	got := processWeatherData(context.Background(), fixture, testAirport)

	if len(got.VfrData) != len(times) {
		t.Fatalf("len(VfrData) = %d, want %d", len(got.VfrData), len(times))
	}

	tests := []struct {
		i              int
		wantWarning    bool
		wantWindGusts  float64
		wantCrossGusts float64
	}{
		{0, false, 0, 0},
		{1, true, 25, 0},
		{2, true, 15, 15},
		{3, true, 25, 25},
	}
	for _, tc := range tests {
		t.Run(times[tc.i], func(t *testing.T) {
			got := got.VfrData[tc.i].GustWarning
			if tc.wantWarning && got == nil {
				t.Fatal("GustWarning = nil, want a warning")
			}
			if !tc.wantWarning {
				if got != nil {
					t.Errorf("GustWarning = %+v, want nil", *got)
				}
				return
			}
			if got.WindGusts != tc.wantWindGusts {
				t.Errorf("WindGusts = %v, want %v", got.WindGusts, tc.wantWindGusts)
			}
			if got.CrosswindGusts != tc.wantCrossGusts {
				t.Errorf("CrosswindGusts = %v, want %v", got.CrosswindGusts, tc.wantCrossGusts)
			}
		})
	}
}
