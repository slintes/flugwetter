package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// withTestAirports installs a small fixed airport list for the duration of the test and
// restores whatever was loaded before. The list is package-level state written once at
// startup, so tests must put it back.
func withTestAirports(t *testing.T) {
	t.Helper()

	prevAirports, prevByID, prevDefault := airports, airportsByID, defaultAirport
	t.Cleanup(func() {
		airports, airportsByID, defaultAirport = prevAirports, prevByID, prevDefault
	})

	second := Airport{
		Identifier:     "EDWG",
		Name:           "Wangerooge",
		Latitude:       53.78256,
		Longitude:      7.91957,
		Runways:        []string{"09/27"},
		RunwayHeadings: []float64{94.1, 274.1},
	}

	airports = []Airport{testAirport, second}
	airportsByID = map[string]Airport{
		testAirport.Identifier: testAirport,
		second.Identifier:      second,
	}
	defaultAirport = testAirport
}

func TestGetConfig(t *testing.T) {
	withTestAirports(t)

	rec := httptest.NewRecorder()
	getConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var config ConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &config); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(config.Airports) != 2 {
		t.Errorf("len(Airports) = %d, want 2", len(config.Airports))
	}
	if config.DefaultAirport != testAirport.Identifier {
		t.Errorf("DefaultAirport = %q, want %q", config.DefaultAirport, testAirport.Identifier)
	}
	// No key is set in tests, so the overlay must be reported as unavailable rather than
	// advertised to a frontend that would then request tiles the server cannot serve.
	t.Setenv(openAIPKeyEnv, "")
	if config.OpenAIPOverlay && openAIPEnabled() {
		t.Error("OpenAIPOverlay = true without an API key")
	}
}

func TestGetWeatherData(t *testing.T) {
	withTestAirports(t)

	payload := &ProcessedWeatherData{
		TemperatureData: []TemperaturePoint{{Time: "2026-08-04T10:00", Temperature: 18}},
		GeneratedAt:     time.Now(),
	}
	stubFetchWeather(t, func(_ context.Context, airport Airport) (*ProcessedWeatherData, error) {
		copied := *payload
		return &copied, nil
	})

	t.Run("default airport when none is given", func(t *testing.T) {
		rec := httptest.NewRecorder()
		getWeatherData(rec, httptest.NewRequest(http.MethodGet, "/api/weather", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		var got ProcessedWeatherData
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(got.TemperatureData) != 1 {
			t.Errorf("len(TemperatureData) = %d, want 1", len(got.TemperatureData))
		}
		if got.Stale {
			t.Error("Stale = true on a successful fetch")
		}
	})

	t.Run("named airport", func(t *testing.T) {
		rec := httptest.NewRecorder()
		getWeatherData(rec, httptest.NewRequest(http.MethodGet, "/api/weather?airport=EDWG", nil))

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	// An unknown identifier must not fall back to the default: serving one airfield's
	// weather under another's name is not something the user could notice.
	t.Run("unknown airport is rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		getWeatherData(rec, httptest.NewRequest(http.MethodGet, "/api/weather?airport=NOPE", nil))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})
}

// A failing upstream with nothing cached is the one case that must still surface as an
// error rather than an empty but successful-looking payload.
func TestGetWeatherData_UpstreamFailure(t *testing.T) {
	withTestAirports(t)
	stubFetchWeather(t, func(context.Context, Airport) (*ProcessedWeatherData, error) {
		return nil, errors.New("open-meteo unreachable")
	})

	rec := httptest.NewRecorder()
	getWeatherData(rec, httptest.NewRequest(http.MethodGet, "/api/weather", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// The same request with an expired entry available returns 200 and the stale flag, which is
// what the frontend keys its age banner on.
func TestGetWeatherData_ServesStaleWithFlag(t *testing.T) {
	withTestAirports(t)
	stubFetchWeather(t, func(context.Context, Airport) (*ProcessedWeatherData, error) {
		return nil, errors.New("open-meteo unreachable")
	})

	generatedAt := time.Now().Add(-42 * time.Minute)
	cache.mutex.Lock()
	cache.entries[testAirport.Identifier] = &cacheEntry{
		data:      &ProcessedWeatherData{GeneratedAt: generatedAt},
		timestamp: generatedAt,
	}
	cache.mutex.Unlock()

	rec := httptest.NewRecorder()
	getWeatherData(rec, httptest.NewRequest(http.MethodGet, "/api/weather", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got ProcessedWeatherData
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !got.Stale {
		t.Error("Stale = false, want true so the frontend can show the age banner")
	}
	if !got.GeneratedAt.Equal(generatedAt.Round(0)) && got.GeneratedAt.Unix() != generatedAt.Unix() {
		t.Errorf("GeneratedAt = %v, want the cached entry's %v", got.GeneratedAt, generatedAt)
	}
}

// buildAPIURL's coordinates and model are covered by TestBuildAPIURL in airports_test.go.
// timezone=GMT is asserted separately here because it is the one parameter the frontend
// depends on directly: updateCharts appends "Z" to every timestamp to parse it as UTC, so a
// change to the timezone would shift the whole forecast without any error.
func TestBuildAPIURL_TimezoneIsGMT(t *testing.T) {
	if url := buildAPIURL(testAirport); !strings.Contains(url, "timezone=GMT") {
		t.Errorf("URL is missing timezone=GMT: %s", url)
	}
}

func TestLoggingMiddlewareCapturesStatus(t *testing.T) {
	handler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
}
