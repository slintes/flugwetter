package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
)

const DEBUG = false

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
	Symbol     string  `json:"symbol"`
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
		log.Printf("Request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

		// Create a custom response writer to capture the status code
		rw := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // Default to 200 OK
		}

		// Call the next handler with our custom response writer
		next.ServeHTTP(rw, r)

		// Log the response status
		debug("Response: %d for %s %s", rw.statusCode, r.Method, r.URL.Path)
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

func main() {
	// A broken airport list is fatal: an empty one renders as a working UI with no data.
	if err := loadAirports(); err != nil {
		log.Fatalf("Failed to load airports: %v", err)
	}

	// Signal-driven shutdown, so `make restart` drains in-flight requests rather than
	// cutting them mid-response.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	r := mux.NewRouter()

	// pre cache weather data for the default airport only. Warming all of them would fire
	// one very large Open-Meteo request per airfield before the first user arrives.
	_, _ = GetWeatherData(ctx, defaultAirport)

	// Add logging middleware to log all requests
	r.Use(loggingMiddleware)

	// Serve static files
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("../frontend/"))))

	// API endpoints
	r.HandleFunc("/api/config", getConfig).Methods("GET")
	r.HandleFunc("/api/weather", getWeatherData).Methods("GET")
	if openAIPEnabled() {
		r.HandleFunc(tileRoute, serveOpenAIPTile).Methods("GET")
		fmt.Println("openAIP overlay enabled")
	} else {
		fmt.Printf("openAIP overlay disabled: set %s to enable it\n", openAIPKeyEnv)
	}
	r.HandleFunc("/", serveIndex).Methods("GET")

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           r,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	go func() {
		fmt.Println("Server starting on :8080")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	<-ctx.Done()
	stop() // restore default signal handling, so a second Ctrl-C kills immediately

	log.Println("Shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Graceful shutdown failed: %v", err)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "../frontend/index.html")
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
	w.Header().Set("Access-Control-Allow-Origin", "*")

	config := ConfigResponse{
		Airports:       airports,
		DefaultAirport: defaultAirport.Identifier,
		OpenAIPOverlay: openAIPEnabled(),
	}

	if err := json.NewEncoder(w).Encode(config); err != nil {
		log.Printf("Error encoding config: %v", err)
		http.Error(w, "Failed to encode config", http.StatusInternalServerError)
	}
}

func getWeatherData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// An unknown identifier is rejected rather than falling back to the default: serving
	// another airfield's weather under the wrong name is not something the user can spot.
	airport, err := lookupAirport(r.URL.Query().Get("airport"))
	if err != nil {
		log.Printf("Error resolving airport: %v", err)
		http.Error(w, "Unknown airport", http.StatusBadRequest)
		return
	}

	data, err := GetWeatherData(r.Context(), airport)
	if err != nil {
		log.Printf("Error fetching weather data for %s: %v", airport.Identifier, err)
		http.Error(w, "Failed to fetch weather data", http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding weather data: %v", err)
		http.Error(w, "Failed to encode weather data", http.StatusInternalServerError)
		return
	}
}

func debug(s string, v ...interface{}) {
	if DEBUG {
		fmt.Printf(s, v...)
		fmt.Println()
	}
}
