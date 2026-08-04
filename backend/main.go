package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

const DEBUG = false

// ProcessedWeatherData represents the data structure sent to the frontend
type ProcessedWeatherData struct {
	TemperatureData []TemperaturePoint `json:"temperature_data"`
	CloudData       []CloudPoint       `json:"cloud_data"`
	WindData        []WindPoint        `json:"wind_data"`
	VfrData         []VfrPoint         `json:"vfr_data"`
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

func main() {
	// A broken airport list is fatal: an empty one renders as a working UI with no data.
	if err := loadAirports(); err != nil {
		log.Fatalf("Failed to load airports: %v", err)
	}

	r := mux.NewRouter()

	// pre cache weather data for the default airport only. Warming all of them would fire
	// one very large Open-Meteo request per airfield before the first user arrives.
	_, _ = GetWeatherData(defaultAirport)

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

	fmt.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
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

	data, err := GetWeatherData(airport)
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
