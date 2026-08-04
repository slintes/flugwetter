package server

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

// This file tests the decode path against a real Open-Meteo response, captured with the
// exact query the app sends (see apiURLTemplate) and trimmed to 24 hours.
//
// The ~80 `json:` tags on WeatherAPIResponse are the most brittle thing in the backend:
// nothing in the type system connects them to the variables apiURLTemplate asks for. A
// renamed or dropped upstream field leaves its slice empty, and every value derived from it
// silently becomes zero -- a cloud layer at 0ft, a wind of 0kn -- with no error anywhere.

const goldenFixture = "testdata/openmeteo_edwn.json"

// goldenFixtureHours is the number of hours in the captured response.
const goldenFixtureHours = 24

func loadGoldenFixture(t *testing.T) *WeatherAPIResponse {
	t.Helper()

	body, err := os.ReadFile(goldenFixture)
	if err != nil {
		t.Fatalf("failed to read %s: %v", goldenFixture, err)
	}

	var response WeatherAPIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("failed to decode %s: %v", goldenFixture, err)
	}
	return &response
}

// stubDayLightByDate answers with a plausible early-August window for whatever date is
// asked, so a fixture spanning several dates scores the way it would in production. The
// fixed-date stubDayLight cannot be used here: every hour after the fixture's first sunset
// would compare as "outside civil twilight" and score 0.
func stubDayLightByDate(t *testing.T) {
	t.Helper()

	original := getDayLightFn
	getDayLightFn = func(_ context.Context, _, _ string, at time.Time) (*SunriseSunsetResponse, error) {
		day := at.Format("2006-01-02")
		mustParse := func(clock string) time.Time {
			ts, err := time.Parse(time.RFC3339, day+"T"+clock+"Z")
			if err != nil {
				t.Fatalf("bad fixture time %q: %v", day+"T"+clock, err)
			}
			return ts
		}

		resp := &SunriseSunsetResponse{Status: "OK"}
		resp.Parsed.CivilTwilightBegin = mustParse("02:40:00")
		resp.Parsed.Sunrise = mustParse("03:30:00")
		resp.Parsed.Sunset = mustParse("19:30:00")
		resp.Parsed.CivilTwilightEnd = mustParse("20:20:00")
		return resp, nil
	}
	t.Cleanup(func() { getDayLightFn = original })
}

// TestGoldenFixture_EveryHourlyFieldBinds is the tag guard. Every hourly slice the struct
// declares is a variable apiURLTemplate requests, so all of them must come back populated.
// An empty one means the tag and the query have drifted apart.
func TestGoldenFixture_EveryHourlyFieldBinds(t *testing.T) {
	response := loadGoldenFixture(t)

	hourly := reflect.ValueOf(response.Hourly)
	hourlyType := hourly.Type()

	for i := 0; i < hourly.NumField(); i++ {
		field := hourly.Field(i)
		name := hourlyType.Field(i).Name
		tag := hourlyType.Field(i).Tag.Get("json")

		if field.Kind() != reflect.Slice {
			continue
		}
		if field.Len() != goldenFixtureHours {
			t.Errorf("%s (json:%q) decoded %d values, want %d — the tag and the query have drifted apart",
				name, tag, field.Len(), goldenFixtureHours)
		}
	}
}

// TestGoldenFixture_ProcessesToKnownValues pins the conversions that sit between the
// upstream response and the wire format: metres to feet, metres to kilometres, the
// coverage filter, the cloud base rule and the crosswind component.
func TestGoldenFixture_ProcessesToKnownValues(t *testing.T) {
	stubDayLightByDate(t)

	got := processWeatherData(context.Background(), loadGoldenFixture(t), testAirport)

	for _, series := range []struct {
		name string
		n    int
	}{
		{"TemperatureData", len(got.TemperatureData)},
		{"CloudData", len(got.CloudData)},
		{"WindData", len(got.WindData)},
		{"VfrData", len(got.VfrData)},
	} {
		if series.n != goldenFixtureHours {
			t.Errorf("len(%s) = %d, want %d", series.name, series.n, goldenFixtureHours)
		}
	}

	t.Run("temperature", func(t *testing.T) {
		first := got.TemperatureData[0]
		if first.Time != "2026-08-04T00:00" {
			t.Errorf("Time = %q, want 2026-08-04T00:00", first.Time)
		}
		if first.Temperature != 20.7 {
			t.Errorf("Temperature = %v, want 20.7", first.Temperature)
		}
		if first.DewPoint != 16.9 {
			t.Errorf("DewPoint = %v, want 16.9", first.DewPoint)
		}
	})

	t.Run("visibility is converted to km", func(t *testing.T) {
		// Upstream reports 28400 m for this hour.
		vis := got.CloudData[0].Visibility
		if vis == nil {
			t.Fatal("Visibility is nil, want 28.4")
		}
		if *vis != 28.4 {
			t.Errorf("Visibility = %v km, want 28.4", *vis)
		}
	})

	t.Run("cloud layers use geopotential height in feet", func(t *testing.T) {
		layers := got.CloudData[0].CloudLayers
		// Six pressure levels report non-zero coverage for this hour; levels at 0% are
		// dropped so the chart does not draw fully transparent symbols.
		if len(layers) != 6 {
			t.Fatalf("len(CloudLayers) = %d, want 6", len(layers))
		}
		// 850hPa sits at 1512.7 m, which is 4963 ft after the 3.28084 conversion.
		if layers[0].HeightFeet != 4963 {
			t.Errorf("HeightFeet = %d, want 4963", layers[0].HeightFeet)
		}
		if layers[0].Coverage != 11 {
			t.Errorf("Coverage = %d, want 11", layers[0].Coverage)
		}
		for i, layer := range layers {
			if layer.Coverage <= 0 {
				t.Errorf("CloudLayers[%d].Coverage = %d, want a zero-coverage layer to be dropped", i, layer.Coverage)
			}
		}
	})

	t.Run("cloud base is the lowest layer at or above 40 percent, as a flight level", func(t *testing.T) {
		// Hour 0: 800hPa is the lowest layer at >= 40% (41%), at 6643 ft -> FL66.
		if base := got.CloudData[0].Base; base == nil {
			t.Error("Base = nil, want FL66")
		} else if *base != 66 {
			t.Errorf("Base = FL%d, want FL66", *base)
		}

		// Hour 12: nothing reaches 40%, so there is no ceiling at all.
		if base := got.CloudData[12].Base; base != nil {
			t.Errorf("Base = FL%d, want nil when no layer reaches 40%%", *base)
		}
	})

	t.Run("crosswind is taken against the best runway end", func(t *testing.T) {
		// Hour 0: 3.3 kn from 357 deg against EDWN's true 55/235.
		wind := got.WindData[0]
		if wind.WindSpeed10m != 3.3 {
			t.Errorf("WindSpeed10m = %v, want 3.3", wind.WindSpeed10m)
		}
		if wind.WindGusts10m != 8.2 {
			t.Errorf("WindGusts10m = %v, want 8.2", wind.WindGusts10m)
		}
		if diff := wind.Crosswind10m - 2.7986; diff > 0.001 || diff < -0.001 {
			t.Errorf("Crosswind10m = %v, want ~2.7986", wind.Crosswind10m)
		}
		if diff := wind.CrosswindGusts10m - 6.9540; diff > 0.001 || diff < -0.001 {
			t.Errorf("CrosswindGusts10m = %v, want ~6.9540", wind.CrosswindGusts10m)
		}
	})

	t.Run("vfr score and night suffix", func(t *testing.T) {
		// Midday, CAVOK-ish: no ceiling, 42.78 km visibility, no precipitation. The score
		// loses 10 to crosswind gusts (11.49 kn against a 4.54 kn steady crosswind) and 9
		// to a 31.9 C afternoon, both of which the ladder penalises.
		midday := got.VfrData[12]
		if midday.Time != "2026-08-04T12:00" {
			t.Fatalf("Time = %q, want 2026-08-04T12:00", midday.Time)
		}
		if midday.Probability != 81 {
			t.Errorf("Probability = %d, want 81", midday.Probability)
		}
		if !midday.VisibilityKnown {
			t.Error("VisibilityKnown = false, want true — the fixture has visibility for this hour")
		}
		if midday.WeatherCode != "0" {
			t.Errorf("WeatherCode = %q, want \"0\" with no suffix at midday", midday.WeatherCode)
		}

		// Midnight is outside civil twilight, which is a hard no-go, and the icon takes
		// the -night variant.
		midnight := got.VfrData[0]
		if midnight.Probability != 0 {
			t.Errorf("Probability = %d, want 0 outside civil twilight", midnight.Probability)
		}
		if midnight.WeatherCode != "3-night" {
			t.Errorf("WeatherCode = %q, want \"3-night\"", midnight.WeatherCode)
		}
	})
}
