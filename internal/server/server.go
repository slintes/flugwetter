// Package server is the whole application: the HTTP surface, the Open-Meteo and
// sunrise-sunset clients, the caches and the VFR scoring.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"flugwetter/internal/web"
)

// ProcessedWeatherData represents the data structure sent to the frontend
type ProcessedWeatherData struct {
	TemperatureData []TemperaturePoint `json:"temperature_data"`
	CloudData       []CloudPoint       `json:"cloud_data"`
	WindData        []WindPoint        `json:"wind_data"`
	VfrData         []VfrPoint         `json:"vfr_data"`
	// GeneratedAt is when this payload was built from an upstream response. It doubles as
	// the cache entry's timestamp, so the two can never disagree.
	GeneratedAt time.Time `json:"generated_at"`
	// Stale is true when upstream was unreachable and an expired cache entry was served
	// instead. The frontend must say so: silently presenting old weather as current is the
	// one failure a flight-planning tool cannot afford.
	Stale bool `json:"stale"`
}

type TemperaturePoint struct {
	Time                     string  `json:"time"`
	Temperature              float64 `json:"temperature"`
	DewPoint                 float64 `json:"dew_point"`
	Precipitation            float64 `json:"precipitation"`
	PrecipitationProbability int     `json:"precipitation_probability"`
}

type CloudPoint struct {
	Time        string       `json:"time"`
	CloudLayers []CloudLayer `json:"cloud_layers"`
	Visibility  *float64     `json:"visibility"`
	Base        *int         `json:"base"`
}

type CloudLayer struct {
	HeightFeet int `json:"height_feet"`
	Coverage   int `json:"coverage"`
}

type WindPoint struct {
	Time              string      `json:"time"`
	WindSpeed10m      float64     `json:"wind_speed_10m"`
	WindGusts10m      float64     `json:"wind_gusts_10m"`
	Crosswind10m      float64     `json:"crosswind_10m"`
	CrosswindGusts10m float64     `json:"crosswind_gusts_10m"`
	WindLayers        []WindLayer `json:"wind_layers"`
}

type WindLayer struct {
	HeightFeet int     `json:"height_feet"`
	Speed      float64 `json:"speed"`
	Direction  int     `json:"direction"`
	// No barb-type field here: the frontend's drawWindBarb decides calm-versus-barb from
	// Speed itself. A server-side copy of that decision was carried on every layer of
	// every hour and never read, leaving two 3kt thresholds of which only the JS one had
	// any effect.
}

type VfrPoint struct {
	Time        string `json:"time"`
	Probability int    `json:"probability"`
	WeatherCode string `json:"weather_code"`
	// VisibilityKnown is false when the model had no visibility for this hour.
	// Probability is then computed from the remaining factors and the frontend
	// marks it as an estimate. A Probability of -1 means no score at all.
	VisibilityKnown bool `json:"visibility_known"`
}

// responseWriter is a custom ResponseWriter that captures the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code and calls the underlying ResponseWriter's WriteHeader
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// loggingMiddleware logs information about each incoming request and its response status
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log the request
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "from", r.RemoteAddr)

		// Create a custom response writer to capture the status code
		rw := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // Default to 200 OK
		}

		// Call the next handler with our custom response writer
		next.ServeHTTP(rw, r)

		// Log the response status
		slog.Debug("response", "status", rw.statusCode, "method", r.Method, "path", r.URL.Path)
	})
}

// Server timeouts. The zero-value http.Server has none, which is gosec G114: a client can
// hold a connection open indefinitely without ever completing a request. nginx in front
// mitigates it in the current deployment, but the app should not depend on that.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	// Generous relative to the others: a cold airport waits on Open-Meteo, which the shared
	// client caps at 10s, and the response still has to be written after that.
	writeTimeout = 30 * time.Second
	idleTimeout  = 60 * time.Second
	// How long in-flight requests get to finish once a shutdown signal arrives.
	shutdownGrace = 10 * time.Second
)

// Run starts the server and blocks until a shutdown signal arrives. It returns an error
// rather than exiting, so main owns the process lifecycle.
func Run() error {
	setupLogging()

	// A broken airport list is fatal: an empty one renders as a working UI with no data.
	if err := loadAirports(); err != nil {
		return fmt.Errorf("failed to load airports: %w", err)
	}

	// Signal-driven shutdown, so `make restart` drains in-flight requests rather than
	// cutting them mid-response.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()

	// pre cache weather data for the default airport only. Warming all of them would fire
	// one very large Open-Meteo request per airfield before the first user arrives.
	_, _ = GetWeatherData(ctx, defaultAirport)

	// Serve static files from the embedded frontend (or from disk under FLUGWETTER_DEV).
	frontend := web.Root()
	mux.Handle("GET /static/", web.StaticHandler(frontend))

	// API endpoints
	mux.HandleFunc("GET /api/config", getConfig)
	mux.HandleFunc("GET /api/weather", getWeatherData)
	if openAIPEnabled() {
		mux.HandleFunc(tileRoute, serveOpenAIPTile)
		slog.Info("openAIP overlay enabled")
	} else {
		slog.Info("openAIP overlay disabled", "hint", "set "+openAIPKeyEnv+" to enable it")
	}
	// "GET /{$}" matches only the root path; a bare "GET /" would also catch every
	// unmatched URL, which gorilla's exact-match router did not do.
	mux.Handle("GET /{$}", web.IndexHandler(frontend))

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           loggingMiddleware(gzipMiddleware(mux)),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Either a signal arrives or the listener fails outright; the second case must not
	// leave the process sitting here forever waiting for a signal that will never come.
	select {
	case err := <-serverErr:
		return fmt.Errorf("server failed: %w", err)
	case <-ctx.Done():
	}
	stop() // restore default signal handling, so a second Ctrl-C kills immediately

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}
	return nil
}

// ConfigResponse is everything the frontend needs to boot: which airfields exist, which one
// to show first, and whether the openAIP overlay is available.
type ConfigResponse struct {
	Airports       []Airport `json:"airports"`
	DefaultAirport string    `json:"default_airport"`
	OpenAIPOverlay bool      `json:"openaip_overlay"`
}

func getConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	config := ConfigResponse{
		Airports:       airports,
		DefaultAirport: defaultAirport.Identifier,
		OpenAIPOverlay: openAIPEnabled(),
	}

	if err := json.NewEncoder(w).Encode(config); err != nil {
		slog.Error("failed to encode config", "error", err)
		http.Error(w, "Failed to encode config", http.StatusInternalServerError)
	}
}

func getWeatherData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// An unknown identifier is rejected rather than falling back to the default: serving
	// another airfield's weather under the wrong name is not something the user can spot.
	airport, err := lookupAirport(r.URL.Query().Get("airport"))
	if err != nil {
		slog.Warn("rejected unknown airport", "error", err)
		http.Error(w, "Unknown airport", http.StatusBadRequest)
		return
	}

	data, err := GetWeatherData(r.Context(), airport)
	if err != nil {
		slog.Error("failed to fetch weather data", "airport", airport.Identifier, "error", err)
		http.Error(w, "Failed to fetch weather data", http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode weather data", "error", err)
		http.Error(w, "Failed to encode weather data", http.StatusInternalServerError)
		return
	}
}
