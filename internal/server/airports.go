package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"strconv"
)

// airportsJSON is the built-in airfield list. Setting FLUGWETTER_AIRPORTS_FILE replaces it
// wholesale at startup, so a deployment can change the list without a rebuild.
//
//go:embed airports.json
var airportsJSON []byte

// airportsFileEnv names the env var holding a path to an alternative airport list.
const airportsFileEnv = "FLUGWETTER_AIRPORTS_FILE"

// Airport is one selectable airfield.
type Airport struct {
	// Identifier is the ICAO code where one exists, otherwise the AIP/openAIP short code.
	// It is the API parameter and the value persisted in the browser, so it must be
	// unique and stable.
	Identifier string  `json:"identifier"`
	Name       string  `json:"name"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	// Runways holds the published (magnetic, rounded) designators, for display only.
	Runways []string `json:"runways"`
	// RunwayHeadings holds TRUE headings in degrees, both ends of every runway.
	// crosswindComponent takes the most favourable of them, so a multi-runway field
	// reports the crosswind after picking the best runway.
	RunwayHeadings []float64 `json:"runway_headings"`
	// Pinned sorts this airfield to the top of the list and makes it the default.
	Pinned bool `json:"pinned,omitempty"`
	// OpeningHours is the airfield's published operating times, copied verbatim from the
	// AIP's TIME block -- UTC, with SS+30, SR-30, ECET, LDG, O/T PPR and the
	// bracketed-summer notation left intact.
	//
	// Free text on purpose. Parsing it into a schedule meant discarding exactly the parts
	// that matter (sunset caps, lunch breaks, PPR, seasons that ignore the daylight-saving
	// boundary) to draw a rectangle that would assert an hour is open. The published line
	// says what the AIP says and lets the reader judge it.
	OpeningHours string `json:"opening_hours,omitempty"`
	// OpeningHoursSource is the AIP page and its date, e.g. "AIP VFR AD 2-78, 12 DEC 2024".
	// The AIP moves on a 28-day AIRAC cycle and this file does not, so the date is what
	// makes the drift visible instead of silent.
	OpeningHoursSource string `json:"opening_hours_source,omitempty"`
	// Website is the airfield's own page, deep-linked to its opening times where it
	// publishes such a page. It is how a reader checks the line above against the source.
	Website string `json:"website,omitempty"`
}

// LatString and LonString format the coordinates for the two consumers that need strings:
// the Open-Meteo query and the sunrise-sunset cache key. Fixed precision keeps the cache
// key stable, which %v on a float would not.
func (a Airport) LatString() string { return strconv.FormatFloat(a.Latitude, 'f', 4, 64) }
func (a Airport) LonString() string { return strconv.FormatFloat(a.Longitude, 'f', 4, 64) }

// crosswindComponent returns the crosswind in knots for a wind of speedKnots
// from directionDegrees (meteorological, true north), taking the most
// favourable of the airport's runway headings.
func (a Airport) crosswindComponent(speedKnots float64, directionDegrees int) float64 {
	crosswind := math.Inf(1)
	for _, heading := range a.RunwayHeadings {
		angle := (float64(directionDegrees) - heading) * math.Pi / 180
		crosswind = math.Min(crosswind, math.Abs(speedKnots*math.Sin(angle)))
	}
	return crosswind
}

var (
	// airports is the loaded list in display order: pinned first, then north to south.
	airports []Airport
	// airportsByID indexes airports by identifier.
	airportsByID map[string]Airport
	// defaultAirport is the pinned entry, or the northernmost one if none is pinned.
	defaultAirport Airport
)

// loadAirports populates the package-level airport list. It is called from main before the
// server starts; a bad list is fatal there, because an empty list renders as a working UI
// with no data at all.
func loadAirports() error {
	raw := airportsJSON
	if path := os.Getenv(airportsFileEnv); path != "" {
		fileContent, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s=%q: %w", airportsFileEnv, path, err)
		}
		raw = fileContent
		slog.Info("loading airports from file", "path", path)
	}

	var parsed []Airport
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("failed to parse airport list: %w", err)
	}

	if err := validateAirports(parsed); err != nil {
		return err
	}

	sortAirports(parsed)

	airports = parsed
	airportsByID = make(map[string]Airport, len(parsed))
	for _, a := range parsed {
		airportsByID[a.Identifier] = a
	}
	defaultAirport = parsed[0]

	slog.Info("loaded airports", "count", len(airports), "default", defaultAirport.Identifier)
	return nil
}

// validateAirports rejects a list that would produce wrong weather rather than an obvious
// error: a duplicate identifier silently shadows an airfield, and a missing runway heading
// makes crosswindComponent return +Inf.
func validateAirports(list []Airport) error {
	if len(list) == 0 {
		return fmt.Errorf("airport list is empty")
	}

	seen := make(map[string]bool, len(list))
	pinned := 0
	for i, a := range list {
		switch {
		case a.Identifier == "":
			return fmt.Errorf("airport %d has no identifier", i)
		case seen[a.Identifier]:
			return fmt.Errorf("duplicate airport identifier %q", a.Identifier)
		case a.Name == "":
			return fmt.Errorf("airport %s has no name", a.Identifier)
		case a.Latitude < -90 || a.Latitude > 90:
			return fmt.Errorf("airport %s has latitude %v out of range", a.Identifier, a.Latitude)
		case a.Longitude < -180 || a.Longitude > 180:
			return fmt.Errorf("airport %s has longitude %v out of range", a.Identifier, a.Longitude)
		case len(a.RunwayHeadings) == 0:
			return fmt.Errorf("airport %s has no runway headings", a.Identifier)
		}
		seen[a.Identifier] = true
		if a.Pinned {
			pinned++
		}
	}

	if pinned > 1 {
		return fmt.Errorf("%d airports are pinned, expected at most 1", pinned)
	}

	return nil
}

// sortAirports puts the pinned airfield first and the rest north to south. Display order is
// computed rather than taken from the file, so a new entry can be appended anywhere.
func sortAirports(list []Airport) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Pinned != list[j].Pinned {
			return list[i].Pinned
		}
		return list[i].Latitude > list[j].Latitude
	})
}

// lookupAirport resolves an identifier from a request. An empty identifier means the
// default; an unknown one is an error, never a silent fallback -- serving another
// airfield's weather under the wrong name is not something the user could notice.
func lookupAirport(identifier string) (Airport, error) {
	if identifier == "" {
		return defaultAirport, nil
	}
	a, ok := airportsByID[identifier]
	if !ok {
		return Airport{}, fmt.Errorf("unknown airport %q", identifier)
	}
	return a, nil
}
