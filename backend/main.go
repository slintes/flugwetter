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
}

type TemperaturePoint struct {
	Time        string  `json:"time"`
	Temperature float64 `json:"temperature"`
}

type CloudPoint struct {
	Time        string       `json:"time"`
	CloudLayers []CloudLayer `json:"cloud_layers"`
}

type CloudLayer struct {
	HeightMeters int    `json:"height_meters"`
	Coverage     int    `json:"coverage"`
	Symbol       string `json:"symbol"`
}

func main() {
	r := mux.NewRouter()

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
