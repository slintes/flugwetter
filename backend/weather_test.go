package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// stubDayLight points getDayLightFn at a fixed summer day at EDWN and restores the
// real implementation when the test ends. Times are UTC, matching the naive-UTC
// timestamps Open-Meteo returns under timezone=GMT.
func stubDayLight(t *testing.T) {
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

	original := getDayLightFn
	getDayLightFn = func(latitude, longitude string, _ time.Time) (*SunriseSunsetResponse, error) {
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
	stubDayLight(t)
	base, wind, xw, gusts, temp := calmCAVOK()

	prob, known := calculateVFRProbability(base, wind, xw, gusts, nil, temp, midday)

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
	stubDayLight(t)
	// Low ceiling at FL16 costs 25; nothing else applies.
	prob, known := calculateVFRProbability(ptrInt(16), 0, 0, 0, nil, TemperaturePoint{Temperature: 18}, midday)

	if known {
		t.Errorf("visibilityKnown = true, want false")
	}
	if prob != 75 {
		t.Errorf("probability = %d, want 75 (cloud base penalty must still apply)", prob)
	}
}

func TestCalculateVFRProbability_KnownVisibility(t *testing.T) {
	stubDayLight(t)
	base, wind, xw, gusts, temp := calmCAVOK()

	prob, known := calculateVFRProbability(base, wind, xw, gusts, ptrFloat(40), temp, midday)

	if !known {
		t.Errorf("visibilityKnown = false, want true when visibility is present")
	}
	if prob != 100 {
		t.Errorf("probability = %d, want 100", prob)
	}
}

func TestCalculateVFRProbability_HardNoGo(t *testing.T) {
	stubDayLight(t)

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
			prob, _ := calculateVFRProbability(tc.cloudBase, 0, 0, 0, tc.visibility, TemperaturePoint{Temperature: 18}, tc.timeStr)
			if prob != 0 {
				t.Errorf("probability = %d, want 0", prob)
			}
		})
	}
}

func TestCalculateVFRProbability_UnparseableTime(t *testing.T) {
	stubDayLight(t)

	prob, known := calculateVFRProbability(nil, 0, 0, 0, ptrFloat(40), TemperaturePoint{}, "not-a-time")

	if prob != -1 {
		t.Errorf("probability = %d, want -1 for an unscoreable hour", prob)
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

	got := processWeatherData(hourlyFixture(times))

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
	getDayLightFn = func(latitude, longitude string, _ time.Time) (*SunriseSunsetResponse, error) {
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
	got := processWeatherData(hourlyFixture(times))

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

	got := processWeatherData(hourlyFixture(times))

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

func TestCrosswindComponent(t *testing.T) {
	// EDWN runway 05/23, true headings {50, 230}.
	tests := []struct {
		name      string
		speed     float64
		direction int
		want      float64
	}{
		{"straight down runway 05", 20, 50, 0},
		{"straight down runway 23", 20, 230, 0},
		{"90 degrees off both ends", 20, 140, 20},
		{"calm", 0, 140, 0},
	}

	const tolerance = 0.01
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EDWN.crosswindComponent(tc.speed, tc.direction)
			if diff := got - tc.want; diff > tolerance || diff < -tolerance {
				t.Errorf("crosswindComponent(%v, %v) = %v, want %v", tc.speed, tc.direction, got, tc.want)
			}
		})
	}
}
