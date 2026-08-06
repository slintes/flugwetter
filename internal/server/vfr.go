package server

import (
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"
)

// VFR scoring.
//
// An hour starts at 100 and every factor in vfrLimits subtracts what its value is worth.
// A factor's cost is not a step but a curve: the table gives a handful of anchor points
// and the cost between two anchors is linearly interpolated, so a value a fraction past a
// threshold costs a fraction of that threshold's penalty. The stepped ladder this replaced
// scored an otherwise perfect hour at 95% because a 4.24kn gust spread crossed a 3kn line
// and immediately cost the full 5 points.
//
// vfrLimits below is the single source of truth for every limit and every penalty.
// Retuning the score means editing that table and nothing else.

// severity names the bands of a factor's curve. The order matters: a curve's anchors must
// be listed in increasing severity, and the evaluator reports the worst one reached.
type severity int

const (
	perfect severity = iota
	good
	difficult
	critical
	noGo
)

func (s severity) String() string {
	switch s {
	case perfect:
		return "perfect"
	case good:
		return "good"
	case difficult:
		return "difficult"
	case critical:
		return "critical"
	case noGo:
		return "no-go"
	}
	return "unknown"
}

// anchor is one point on a factor's cost curve: at `at`, in the factor's unit, the factor
// costs `cost` points.
//
// Two costs are implicit and must be left at zero in the table: the first anchor is always
// `perfect` and costs nothing, and a `noGo` anchor costs noGoCost -- reaching it zeroes the
// whole hour, so its exact value only shapes the ramp leading into it.
type anchor struct {
	severity severity
	at       float64
	cost     float64
}

// noGoCost is where the ramp into a no-go anchor arrives. It is not subtracted from
// anything: a factor that reaches its no-go returns 0% for the hour outright.
const noGoCost = 100

type factor struct {
	// name and unit are for the breakdown and the debug log only; nothing in the scoring
	// reads them. The unit doubles as documentation of what the thresholds mean -- cloud
	// base is in flight levels, not feet, which is easy to misread.
	name string
	unit string

	// value extracts this factor's input for one hour. ok=false skips the factor
	// entirely, either because the data is missing (visibility beyond the model horizon)
	// or because the factor does not apply (precipitation probability without
	// precipitation to speak of).
	value func(c conditions) (v float64, ok bool)

	// curve is ordered from perfect to worst. Whether the factor gets worse as its value
	// rises or falls is inferred from the direction the thresholds run in.
	curve []anchor
}

// conditions is one hour's input to the scoring.
type conditions struct {
	time     time.Time
	daylight *SunriseSunsetResponse

	cloudBaseFL    *int // flight levels, i.e. feet/100
	windSpeed      float64
	crosswind      float64
	crosswindGusts float64
	visibilityKM   *float64

	temperature              float64
	precipitation            float64
	precipitationProbability int
}

// Daylight is an ordinal rather than a measurement: the twilight boundaries move with the
// date and the latitude, so there is nothing constant to put in a threshold column.
const (
	daylightDay      = 0.0 // between sunrise and sunset
	daylightTwilight = 1.0 // inside civil twilight but not between sunrise and sunset
	daylightNight    = 2.0 // outside civil twilight
)

// vfrLimits is the table. Every limit and every penalty in the application lives here.
//
// Read `{difficult, 20, 25}` as "at FL20 this factor costs 25 points, and it is a
// difficult one". Costs between two anchors are interpolated, costs beyond the last anchor
// are clamped to it, and a no-go anchor ends the scoring at 0% for the hour.
var vfrLimits = []factor{
	{
		// Cloud base is a flight level (feet/100), and only layers covering 40% or more
		// count -- see getCloudBase. No cloud at all skips the factor.
		name: "cloud base",
		unit: "FL",
		value: func(c conditions) (float64, bool) {
			if c.cloudBaseFL == nil {
				return 0, false
			}
			return float64(*c.cloudBaseFL), true
		},
		curve: []anchor{
			{perfect, 50, 0},
			{good, 30, 1},
			{difficult, 20, 5},
			{critical, 15, 20},
			{noGo, 10, 0},
		},
	},
	{
		// Open-Meteo drops visibility beyond the ICON-EU horizon, which is the tail of
		// every forecast. Those hours are scored on everything else and flagged as
		// estimates rather than being penalised for the gap.
		name: "visibility",
		unit: "km",
		value: func(c conditions) (float64, bool) {
			if c.visibilityKM == nil {
				return 0, false
			}
			return *c.visibilityKM, true
		},
		curve: []anchor{
			{perfect, 40, 0},
			{good, 20, 5},
			{difficult, 15, 10},
			{critical, 10, 20},
			{noGo, 5, 0},
		},
	},
	{
		// Total wind is deliberately gentler than crosswind: the two overlap, and a
		// strong wind straight down the runway is not the problem a strong crosswind is.
		name:  "wind",
		unit:  "kn",
		value: func(c conditions) (float64, bool) { return c.windSpeed, true },
		curve: []anchor{
			{perfect, 5, 0},
			{good, 10, 5},
			{difficult, 15, 10},
			{critical, 20, 20},
			{noGo, 30, 0},
		},
	},
	{
		// Crosswind is against the best runway end -- crosswindComponent takes the
		// minimum over the airport's runway headings.
		name:  "crosswind",
		unit:  "kn",
		value: func(c conditions) (float64, bool) { return c.crosswind, true },
		curve: []anchor{
			{perfect, 5, 0},
			{good, 8, 10},
			{difficult, 10, 20},
			{critical, 15, 40},
			{noGo, 20, 0},
		},
	},
	{
		// What costs points is the gust's margin over the steady crosswind, not its
		// absolute value: 5 gusting 15 is harder to land in than a steady 15. A wide
		// spread is a sign of heavy gusting in its own right, whatever the steady
		// crosswind is doing, which is why this carries a no-go of its own.
		name:  "crosswind gusts",
		unit:  "kn",
		value: func(c conditions) (float64, bool) { return c.crosswindGusts - c.crosswind, true },
		curve: []anchor{
			{perfect, 5, 0},
			{good, 10, 5},
			{difficult, 15, 10},
			{critical, 20, 20},
			{noGo, 30, 0},
		},
	},
	{
		name:  "precipitation",
		unit:  "mm/h",
		value: func(c conditions) (float64, bool) { return c.precipitation, true },
		curve: []anchor{
			{perfect, 1, 0},
			{good, 2, 5},
			{difficult, 10, 20},
			{critical, 20, 40},
			{noGo, 30, 0},
		},
	},
	{
		// Only meaningful once there is rain to be probable about. A 90% chance of 0.1mm
		// is not worth a penalty on top of the amount itself.
		name: "precipitation probability",
		unit: "%",
		value: func(c conditions) (float64, bool) {
			if c.precipitation < 2 {
				return 0, false
			}
			return float64(c.precipitationProbability), true
		},
		curve: []anchor{
			{perfect, 30, 0},
			{good, 60, 10},
			{difficult, 100, 20},
		},
	},
	{
		// Density altitude on a short grass strip.
		name:  "temperature",
		unit:  "C",
		value: func(c conditions) (float64, bool) { return c.temperature, true },
		curve: []anchor{
			{perfect, 25, 0},
			{good, 28, 1},
			{difficult, 30, 10},
			{critical, 35, 30},
			{noGo, 40, 0},
		},
	},
	{
		// Legal but not comfortable inside civil twilight; outside it, not legal at all.
		name:  "daylight",
		unit:  "",
		value: func(c conditions) (float64, bool) { return daylightOrdinal(c), true },
		curve: []anchor{
			{perfect, daylightDay, 0},
			{critical, daylightTwilight, 30},
			{noGo, daylightNight, 0},
		},
	},
}

// daylightOrdinal places the hour in the day / twilight / night bands. The caller
// guarantees c.daylight is non-nil.
func daylightOrdinal(c conditions) float64 {
	if c.time.Before(c.daylight.Parsed.CivilTwilightBegin) || c.time.After(c.daylight.Parsed.CivilTwilightEnd) {
		return daylightNight
	}
	if c.time.Before(c.daylight.Parsed.Sunrise) || c.time.After(c.daylight.Parsed.Sunset) {
		return daylightTwilight
	}
	return daylightDay
}

// worseHigher reports whether the factor gets worse as its value rises. It is inferred
// from the thresholds rather than declared, so the table cannot disagree with itself.
func (f factor) worseHigher() bool {
	return f.curve[len(f.curve)-1].at > f.curve[0].at
}

// evaluate returns what this factor's value costs, the band it landed in, and whether it
// reached a no-go.
//
// An anchor names the band that ends at it, so a value is called "difficult" from the
// moment it leaves the good anchor until it reaches the difficult one. The exception is a
// no-go anchor, which names a wall rather than a band: values on the last approach to it
// keep the name of the band before it.
//
// Costs are rounded per factor rather than at the end so that the numbers in the breakdown
// add up to the score that is displayed.
func (f factor) evaluate(v float64) (cost int, sev severity, isNoGo bool) {
	// Normalise so the curve always runs in ascending order, and compare in that space.
	sign := 1.0
	if !f.worseHigher() {
		sign = -1.0
	}
	at := func(i int) float64 { return sign * f.curve[i].at }
	costAt := func(i int) float64 {
		if f.curve[i].severity == noGo {
			return noGoCost
		}
		return f.curve[i].cost
	}
	n := sign * v
	last := len(f.curve) - 1

	if n <= at(0) {
		return 0, perfect, false
	}
	if n >= at(last) {
		if f.curve[last].severity == noGo {
			return noGoCost, noGo, true
		}
		// No no-go for this factor: the curve stops rising rather than extrapolating.
		return int(math.Round(costAt(last))), f.curve[last].severity, false
	}

	// Find the segment (at(i), at(i+1)] that holds the value, and ramp across it.
	i := 0
	for i+1 < last && n > at(i+1) {
		i++
	}
	frac := (n - at(i)) / (at(i+1) - at(i))
	cost = int(math.Round(costAt(i) + frac*(costAt(i+1)-costAt(i))))

	sev = f.curve[i+1].severity
	if sev == noGo {
		sev = f.curve[i].severity
	}
	return cost, sev, false
}

// scoreVFR scores one hour against vfrLimits.
//
// probability is 0-100, or -1 when the hour could not be scored at all: a nil daylight
// window means there is no way to tell a CAVOK afternoon from the middle of the night, so
// the frontend shows "no data" rather than a misleading number.
//
// penalties lists only the factors that actually cost something, worst first, and is what
// the API hands the frontend to explain the score. A factor that reaches its no-go is the
// only penalty returned: it is the reason, and nothing else about the hour matters.
//
// visibilityKnown reports whether the model supplied a visibility for this hour. Those
// hours are still scored on the remaining factors; the caller presents them as estimates.
func scoreVFR(c conditions) (probability int, penalties []VfrPenalty, visibilityKnown bool) {
	if c.daylight == nil {
		return -1, nil, false
	}
	visibilityKnown = c.visibilityKM != nil

	slog.Debug("scoring vfr probability", "hour", c.time.Format(time.RFC822))

	probability = 100
	for _, f := range vfrLimits {
		v, ok := f.value(c)
		if !ok {
			continue
		}

		cost, sev, isNoGo := f.evaluate(v)
		if isNoGo {
			slog.Debug("vfr no-go", "factor", f.name, "value", v, "unit", f.unit)
			return 0, []VfrPenalty{{Factor: f.name, Value: v, Unit: f.unit, Severity: sev.String(), Cost: noGoCost}}, visibilityKnown
		}
		if cost <= 0 {
			continue
		}

		slog.Debug("vfr penalty applied",
			"factor", f.name, "value", v, "unit", f.unit, "severity", sev.String(), "cost", cost)
		penalties = append(penalties, VfrPenalty{
			Factor: f.name, Value: v, Unit: f.unit, Severity: sev.String(), Cost: cost,
		})
		probability -= cost
	}

	// Worst first, so the tooltip leads with the reason that dominates the score.
	sort.SliceStable(penalties, func(i, j int) bool { return penalties[i].Cost > penalties[j].Cost })

	if probability < 0 {
		probability = 0
	}
	return probability, penalties, visibilityKnown
}

// init rejects a malformed vfrLimits at startup, the same way a malformed airports.json
// is fatal: a curve that runs backwards or repeats a threshold would silently score every
// hour wrong, and the table is the one place where that mistake is easy to make.
func init() {
	for _, f := range vfrLimits {
		if err := f.validate(); err != nil {
			panic(fmt.Sprintf("vfrLimits: factor %q: %v", f.name, err))
		}
	}
}

func (f factor) validate() error {
	if len(f.curve) < 2 {
		return fmt.Errorf("needs at least two anchors, has %d", len(f.curve))
	}
	if f.curve[0].severity != perfect {
		return fmt.Errorf("first anchor must be perfect, is %s", f.curve[0].severity)
	}
	if f.curve[0].cost != 0 {
		return fmt.Errorf("the perfect anchor must cost nothing, costs %v", f.curve[0].cost)
	}

	ascending := f.worseHigher()
	for i := 1; i < len(f.curve); i++ {
		prev, cur := f.curve[i-1], f.curve[i]

		if cur.severity <= prev.severity {
			return fmt.Errorf("anchor %d (%s) does not come after %s", i, cur.severity, prev.severity)
		}
		if prev.severity == noGo {
			return fmt.Errorf("anchor %d comes after a no-go, which must be last", i)
		}
		if ascending && cur.at <= prev.at {
			return fmt.Errorf("threshold %v at anchor %d does not rise above %v", cur.at, i, prev.at)
		}
		if !ascending && cur.at >= prev.at {
			return fmt.Errorf("threshold %v at anchor %d does not fall below %v", cur.at, i, prev.at)
		}

		if cur.severity == noGo {
			if cur.cost != 0 {
				return fmt.Errorf("the no-go anchor's cost is implicit, set it to 0 not %v", cur.cost)
			}
			if prev.cost >= noGoCost {
				return fmt.Errorf("cost %v at anchor %d leaves no room to ramp into the no-go", prev.cost, i-1)
			}
			continue
		}
		if cur.cost <= prev.cost {
			return fmt.Errorf("cost %v at anchor %d does not rise above %v", cur.cost, i, prev.cost)
		}
	}
	return nil
}
