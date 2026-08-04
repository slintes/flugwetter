package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedAirportsAreValid guards the shipped list itself: a typo in airports.json is a
// startup failure in production, and this is the cheapest place to catch it.
func TestEmbeddedAirportsAreValid(t *testing.T) {
	var list []Airport
	if err := json.Unmarshal(airportsJSON, &list); err != nil {
		t.Fatalf("embedded airports.json does not parse: %v", err)
	}
	if err := validateAirports(list); err != nil {
		t.Fatalf("embedded airports.json is invalid: %v", err)
	}

	var pinned []string
	for _, a := range list {
		if a.Pinned {
			pinned = append(pinned, a.Identifier)
		}
		// Runway headings are true degrees; anything outside 0..360 is a data entry slip
		// that would silently distort every crosswind figure.
		for _, h := range a.RunwayHeadings {
			if h < 0 || h >= 360 {
				t.Errorf("%s has runway heading %v out of range", a.Identifier, h)
			}
		}
		// Both ends of every runway are expected, so the count is even.
		if len(a.RunwayHeadings)%2 != 0 {
			t.Errorf("%s has %d runway headings, want both ends of every runway", a.Identifier, len(a.RunwayHeadings))
		}
	}

	if len(pinned) != 1 || pinned[0] != "EDWN" {
		t.Errorf("pinned airports = %v, want exactly [EDWN]", pinned)
	}
}

func TestLoadAirportsEmbedded(t *testing.T) {
	t.Setenv(airportsFileEnv, "")

	if err := loadAirports(); err != nil {
		t.Fatalf("loadAirports() failed: %v", err)
	}

	if len(airports) == 0 {
		t.Fatal("no airports loaded")
	}
	if defaultAirport.Identifier != "EDWN" {
		t.Errorf("defaultAirport = %q, want EDWN", defaultAirport.Identifier)
	}
	if airports[0].Identifier != "EDWN" {
		t.Errorf("airports[0] = %q, want the pinned EDWN on top", airports[0].Identifier)
	}
	if len(airportsByID) != len(airports) {
		t.Errorf("airportsByID has %d entries, want %d", len(airportsByID), len(airports))
	}

	// Everything after the pinned entry runs north to south.
	for i := 2; i < len(airports); i++ {
		if airports[i].Latitude > airports[i-1].Latitude {
			t.Errorf("airports[%d] (%s, %v) is north of airports[%d] (%s, %v), want north-to-south order",
				i, airports[i].Identifier, airports[i].Latitude,
				i-1, airports[i-1].Identifier, airports[i-1].Latitude)
		}
	}
}

func TestLoadAirportsFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "airports.json")
	content := `[
		{"identifier":"AAAA","name":"South","latitude":50.0,"longitude":7.0,"runway_headings":[90,270]},
		{"identifier":"BBBB","name":"North","latitude":54.0,"longitude":7.0,"runway_headings":[90,270]}
	]`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	t.Setenv(airportsFileEnv, path)

	if err := loadAirports(); err != nil {
		t.Fatalf("loadAirports() failed: %v", err)
	}
	if len(airports) != 2 {
		t.Fatalf("loaded %d airports, want 2 — the external file must replace the embedded list", len(airports))
	}
	// Nothing is pinned, so the northernmost becomes the default.
	if defaultAirport.Identifier != "BBBB" {
		t.Errorf("defaultAirport = %q, want BBBB", defaultAirport.Identifier)
	}

	// Leave the globals in their shipped state for the rest of the package.
	t.Cleanup(func() {
		os.Unsetenv(airportsFileEnv)
		if err := loadAirports(); err != nil {
			t.Fatalf("failed to restore embedded airports: %v", err)
		}
	})
}

func TestLoadAirportsMissingFile(t *testing.T) {
	t.Setenv(airportsFileEnv, filepath.Join(t.TempDir(), "does-not-exist.json"))

	if err := loadAirports(); err == nil {
		t.Fatal("loadAirports() succeeded with a missing file, want an error")
	}
}

func TestValidateAirports(t *testing.T) {
	valid := Airport{Identifier: "EDWN", Name: "Nordhorn-Lingen", Latitude: 52.4575, Longitude: 7.185, RunwayHeadings: []float64{50, 230}}

	tests := []struct {
		name    string
		list    []Airport
		wantErr bool
	}{
		{"valid single entry", []Airport{valid}, false},
		{"empty list", nil, true},
		{
			name:    "duplicate identifier",
			list:    []Airport{valid, valid},
			wantErr: true,
		},
		{
			name:    "missing identifier",
			list:    []Airport{{Name: "x", RunwayHeadings: []float64{50}}},
			wantErr: true,
		},
		{
			name:    "missing name",
			list:    []Airport{{Identifier: "EDWN", RunwayHeadings: []float64{50}}},
			wantErr: true,
		},
		{
			// Without a heading crosswindComponent returns +Inf, which would sail
			// through the VFR scoring as "no crosswind penalty computed".
			name:    "no runway headings",
			list:    []Airport{{Identifier: "EDWN", Name: "Nordhorn-Lingen", Latitude: 52.4, Longitude: 7.1}},
			wantErr: true,
		},
		{
			name:    "latitude out of range",
			list:    []Airport{{Identifier: "EDWN", Name: "x", Latitude: 91, RunwayHeadings: []float64{50}}},
			wantErr: true,
		},
		{
			name:    "longitude out of range",
			list:    []Airport{{Identifier: "EDWN", Name: "x", Longitude: -181, RunwayHeadings: []float64{50}}},
			wantErr: true,
		},
		{
			name: "two pinned airports",
			list: []Airport{
				{Identifier: "A", Name: "a", RunwayHeadings: []float64{50}, Pinned: true},
				{Identifier: "B", Name: "b", RunwayHeadings: []float64{50}, Pinned: true},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAirports(tc.list)
			if tc.wantErr && err == nil {
				t.Error("got no error, want one")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("got error %v, want none", err)
			}
		})
	}
}

func TestSortAirports(t *testing.T) {
	list := []Airport{
		{Identifier: "SOUTH", Latitude: 51.0},
		{Identifier: "NORTH", Latitude: 54.0},
		{Identifier: "PINNED", Latitude: 52.0, Pinned: true},
		{Identifier: "MIDDLE", Latitude: 53.0},
	}

	sortAirports(list)

	want := []string{"PINNED", "NORTH", "MIDDLE", "SOUTH"}
	for i, id := range want {
		if list[i].Identifier != id {
			t.Errorf("list[%d] = %q, want %q", i, list[i].Identifier, id)
		}
	}
}

func TestLookupAirport(t *testing.T) {
	if err := loadAirports(); err != nil {
		t.Fatalf("loadAirports() failed: %v", err)
	}

	t.Run("empty identifier yields the default", func(t *testing.T) {
		got, err := lookupAirport("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Identifier != defaultAirport.Identifier {
			t.Errorf("got %q, want %q", got.Identifier, defaultAirport.Identifier)
		}
	})

	t.Run("known identifier", func(t *testing.T) {
		got, err := lookupAirport("EDWN")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Identifier != "EDWN" {
			t.Errorf("got %q, want EDWN", got.Identifier)
		}
	})

	t.Run("unknown identifier is an error, not a fallback", func(t *testing.T) {
		if got, err := lookupAirport("ZZZZ"); err == nil {
			t.Errorf("got %q and no error, want an error — a silent fallback serves the wrong airfield's weather", got.Identifier)
		}
	})
}

func TestAirportCoordinateStrings(t *testing.T) {
	// Fixed precision keeps the sunrise-sunset cache key stable across calls.
	a := Airport{Latitude: 52.4575, Longitude: 7.185}
	if got := a.LatString(); got != "52.4575" {
		t.Errorf("LatString() = %q, want %q", got, "52.4575")
	}
	if got := a.LonString(); got != "7.1850" {
		t.Errorf("LonString() = %q, want %q", got, "7.1850")
	}
}

func TestBuildAPIURL(t *testing.T) {
	url := buildAPIURL(Airport{Latitude: 52.4575, Longitude: 7.185})

	if want := "latitude=52.4575&longitude=7.1850"; !strings.Contains(url, want) {
		t.Errorf("URL %q does not contain %q", url, want)
	}
	if !strings.Contains(url, "models=icon_seamless") || !strings.Contains(url, "wind_speed_unit=kn") {
		t.Errorf("URL lost a required query parameter: %q", url)
	}
}
