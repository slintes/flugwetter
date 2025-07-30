package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

// WeatherData represents the processed weather information
type WeatherData struct {
	Hourly struct {
		Time           []string  `json:"time"`
		Temperature2m  []float64 `json:"temperature_2m"`
		CloudCoverLow  []int     `json:"cloud_cover_low"`
		CloudCover     []int     `json:"cloud_cover"`
		CloudCoverMid  []int     `json:"cloud_cover_mid"`
		CloudCoverHigh []int     `json:"cloud_cover_high"`
	} `json:"hourly"`
}

// ProcessedWeatherData represents the data structure sent to frontend
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
	Time         string      `json:"time"`
	WindSpeed10m float64     `json:"wind_speed_10m"`
	WindGusts10m float64     `json:"wind_gusts_10m"`
	WindLayers   []WindLayer `json:"wind_layers"`
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
		//log.Printf("Response: %d for %s %s", rw.statusCode, r.Method, r.URL.Path)
	})
}

func main() {
	r := mux.NewRouter()

	// pre cache weather data
	_, _ = GetWeatherData()

	// Add logging middleware to log all requests
	r.Use(loggingMiddleware)

	// Serve static files
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("../frontend/"))))

	// API endpoints
	r.HandleFunc("/api/weather", getWeatherData).Methods("GET")
	r.HandleFunc("/", serveIndex).Methods("GET")

	fmt.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "../frontend/index.html")
}

func getWeatherData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	data, err := GetWeatherData()
	if err != nil {
		log.Printf("Error fetching weather data: %v", err)
		http.Error(w, "Failed to fetch weather data", http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding weather data: %v", err)
		http.Error(w, "Failed to encode weather data", http.StatusInternalServerError)
		return
	}
}
