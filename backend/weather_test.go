package main

import (
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
