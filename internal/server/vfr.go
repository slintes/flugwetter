package server

import (
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// VFR scoring.
//
// Every factor in vfrLimits is judged on its own curve -- perfect, good, difficult,
// critical, or past a hard limit -- and an hour's rating is the worst band any single
// factor reaches. This is a weakest-link read, not a sum: nothing is added, so nothing
// needs weighing against anything else, which is what the point system before this one
// needed weights for.
//
// vfrLimits below is the single source of truth for every limit in the application.
// Retuning the score means editing that table and nothing else. See design-vfr-scoring.md
// for why the score is shaped this way, including the point-based design this replaced.

// severity names the bands of a factor's curve, worst last. The order matters: a curve's
// anchors must be listed in increasing severity, and an hour's rating is the worst severity
// reached by any factor -- a plain ordinal comparison, which is the entire aggregation.
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

// anchor is one point on a factor's curve: at `at`, in the factor's unit, the factor has
// reached this severity.
type anchor struct {
	severity severity
	at       float64
}

type factor struct {
	// name and unit are for the breakdown and the debug log only; nothing in the scoring
	// reads them. The unit doubles as documentation of what the thresholds mean -- cloud
	// base is in flight levels, not feet, which is easy to misread.
	name string
	unit string

	// value extracts this factor's input for one hour. ok=false skips the factor
	// entirely, because the data is missing -- visibility beyond the model horizon.
	value func(c conditions) (v float64, ok bool)

	// curve is ordered from perfect to worst. Whether the factor gets worse as its value
	// rises or falls is inferred from the direction the thresholds run in.
	//
	// Four anchors is the usual shape -- perfect, good, difficult, critical -- which is
	// five bands once `wall` closes the last one.
	curve []anchor

	// wall makes the last anchor a no-go: past it the hour is rated no-go outright.
	// Without it the severity stops rising at the last anchor instead of escalating
	// further -- precipitation is the case, because its value is discounted by how likely
	// it is to happen, and a no-go reached only through an unlikely forecast is not a
	// decision anyone should ship.
	wall bool

	// scaledBy, when set, discounts this factor's value by how likely it is to
	// materialise before the curve is read. The curve stays in the factor's own unit, so
	// its anchors keep reading as "this much, if it happens", and the scale answers "and
	// how likely is that".
	//
	// Precipitation is the case this exists for: amount and probability are not
	// independent. An unlikely downpour should read as if it were lighter rain, not as
	// the downpour it might never become.
	scaledBy *scale
}

// scale modulates a factor's value by a second quantity.
//
// The weight is a curve of its own rather than a formula, so the shape is data: a straight
// line is the expected-value reading, and anything else is a deliberate risk attitude that
// gets retuned the same way every other number in this file does.
type scale struct {
	name string // "probability" -- for the breakdown
	unit string // "%"

	// by extracts the modulating value. ok=false means no scaling at all, i.e. weight 1.
	// Missing data must never discount a factor: an unknown probability is not a low one.
	by func(c conditions) (v float64, ok bool)

	// points are ordered by `at`, with the weight interpolated between them and clamped
	// outside them -- the same shape rule the cost curves used to follow.
	points []scalePoint
}

type scalePoint struct {
	at     float64
	weight float64
}

// weight returns the multiplier for v.
func (s *scale) weight(v float64) float64 {
	if v <= s.points[0].at {
		return s.points[0].weight
	}
	last := len(s.points) - 1
	if v >= s.points[last].at {
		return s.points[last].weight
	}

	i := 0
	for i+1 < last && v > s.points[i+1].at {
		i++
	}
	lo, hi := s.points[i], s.points[i+1]
	return lo.weight + (v-lo.at)/(hi.at-lo.at)*(hi.weight-lo.weight)
}

// effectiveValue resolves the value evaluate() should actually be run against, discounting
// it toward the curve's own perfect anchor when the factor is scaled. The second and third
// returns are the modulating value and whether scaling applied at all, for the breakdown.
//
// Discounting the value rather than a cost is what lets a factor with no points left to
// scale still express "this matters less if it's unlikely": at weight 1 (near-certain)
// nothing changes; at weight 0 (essentially won't happen) the value collapses onto the
// perfect anchor, whatever it actually was -- an unlikely deluge cannot make the hour worse
// than it would be if the rain never showed.
func (f factor) effectiveValue(c conditions, v float64) (effective, scaleValue float64, scaled bool) {
	if f.scaledBy == nil {
		return v, 0, false
	}
	scaleValue, ok := f.scaledBy.by(c)
	if !ok {
		return v, 0, false
	}
	w := f.scaledBy.weight(scaleValue)
	perfectAt := f.curve[0].at
	return perfectAt + (v-perfectAt)*w, scaleValue, true
}

// conditions is one hour's input to the scoring.
type conditions struct {
	time     time.Time
	daylight *SunriseSunsetResponse

	cloudBaseFL  *int // flight levels, i.e. feet/100
	windSpeed    float64
	crosswind    float64
	visibilityKM *float64

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

// vfrLimits is the table. Every limit in the application lives here.
//
// A factor is four thresholds. Read `{difficult, 20}` as "at FL20 this factor has become
// difficult". `wall: true` makes the last anchor a no-go -- the value that ends the hour
// outright, whatever the rest of the weather is doing.
//
// Five bands, four numbers: perfect | good | difficult | critical | no-go.
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
			{perfect, 30},
			{good, 20},
			{difficult, 15},
			{critical, 10},
		},
		wall: true,
	},
	{
		// Open-Meteo drops visibility beyond the ICON-EU horizon, which is the tail of
		// every forecast. Those hours are scored on everything else and flagged as
		// estimates rather than being penalised for the gap.
		//
		// The model's own visibility already carries the effect of rain and mist, so this
		// factor is doing more work than its single number suggests.
		name: "visibility",
		unit: "km",
		value: func(c conditions) (float64, bool) {
			if c.visibilityKM == nil {
				return 0, false
			}
			return *c.visibilityKM, true
		},
		curve: []anchor{
			{perfect, 30},
			{good, 20},
			{difficult, 10},
			{critical, 5},
		},
		wall: true,
	},
	{
		// Total wind overlaps crosswind on every hour: a strong wind straight down the
		// runway is not the problem a strong crosswind is, so its limits sit wider apart
		// and the two are left to stand on their own rather than one discounting the other.
		name:  "wind",
		unit:  "kn",
		value: func(c conditions) (float64, bool) { return c.windSpeed, true },
		curve: []anchor{
			{perfect, 5},
			{good, 10},
			{difficult, 20},
			{critical, 30},
		},
		wall: true,
	},
	{
		// Crosswind is against the best runway end -- crosswindComponent takes the
		// minimum over the airport's runway headings.
		name:  "crosswind",
		unit:  "kn",
		value: func(c conditions) (float64, bool) { return c.crosswind, true },
		curve: []anchor{
			{perfect, 4},
			{good, 8},
			{difficult, 12},
			{critical, 16},
		},
		wall: true,
	},
	{
		// The anchors are what the rain is worth if it falls; the scale then discounts the
		// value by how likely that is. No wall, because a no-go reached only through an
		// unlikely forecast is not a decision worth shipping -- the severity stops rising
		// at critical instead.
		//
		// The limits are where this airfield's rain actually lives. Three years of ERA5 at
		// EDWN: half of all wet hours are below 0.2mm/h, 1.0 is the 90th percentile
		// (~280 h/year), and 4.0 is moderate-and-above (~20 h/year).
		name:  "precipitation",
		unit:  "mm/h",
		value: func(c conditions) (float64, bool) { return c.precipitation, true },
		curve: []anchor{
			{perfect, 0},
			{good, 0.2},
			{difficult, 1.0},
			{critical, 4.0},
		},
		scaledBy: &scale{
			// Open-Meteo's precipitation_probability is the ensemble's P(>0.1mm in the
			// hour), while the amount comes from the deterministic run -- so the two can
			// disagree, and a month at EDWN contains 2.7mm/h forecast at 3%.
			//
			// The weights are decision-shaped rather than a straight line: below 30% the
			// hour is not in question, above 70% it is decided, and the middle is where
			// the answer actually moves. A straight line would be the expected-value
			// reading; this one says what a go/no-go call does.
			name: "probability",
			unit: "%",
			by: func(c conditions) (float64, bool) {
				return float64(c.precipitationProbability), true
			},
			points: []scalePoint{
				{0, 0.1},
				{20, 0.2},
				{50, 0.6},
				{80, 1.0},
			},
		},
	},
	{
		// Density altitude on a short grass strip. The wall is a formality at this
		// latitude -- EDWN has not reached 38C in three years -- and is kept so the table
		// has no factor that simply runs off the end.
		name:  "temperature",
		unit:  "C",
		value: func(c conditions) (float64, bool) { return c.temperature, true },
		curve: []anchor{
			{perfect, 25},
			{good, 30},
			{difficult, 35},
			{critical, 40},
		},
		wall: true,
	},
	{
		// Legal but not comfortable inside civil twilight; outside it, not legal at all.
		//
		// An ordinal, so it has one named band and a wall rather than four: there is no
		// continuous scale between day and night to place limits on. Difficult rather than
		// critical -- there used to be a weight here to keep twilight from reading as bad
		// as a critical crosswind, and now that weights are gone the honest way to say the
		// same thing is to pick the softer band outright.
		name:  "daylight",
		unit:  "",
		value: func(c conditions) (float64, bool) { return daylightOrdinal(c), true },
		curve: []anchor{
			{perfect, daylightDay},
			{difficult, daylightTwilight},
		},
		wall: true,
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

// evaluate returns the band a value falls in, and whether it is past the wall.
//
// An anchor names the band that ends at it, so a value is called "difficult" from the
// moment it leaves the good anchor until it reaches the difficult one. Past the last anchor
// the factor is a no-go if it has a wall, and otherwise stays named for the last band --
// severity was always a discrete lookup, never interpolated, so there is no ramp to protect
// here the way there used to be for cost.
func (f factor) evaluate(v float64) (sev severity, isNoGo bool) {
	// Normalise so the curve always runs in ascending order, and compare in that space.
	sign := 1.0
	if !f.worseHigher() {
		sign = -1.0
	}
	at := func(i int) float64 { return sign * f.curve[i].at }
	n := sign * v
	last := len(f.curve) - 1

	if n <= at(0) {
		return perfect, false
	}
	if n > at(last) {
		if f.wall {
			return noGo, true
		}
		// No wall: the severity stops rising rather than escalating further.
		return f.curve[last].severity, false
	}

	// Find the segment (at(i), at(i+1)] that holds the value; it takes the upper anchor's
	// name, which is the band that segment is climbing toward.
	i := 0
	for i+1 < last && n > at(i+1) {
		i++
	}
	return f.curve[i+1].severity, false
}

// scoreVFR scores one hour against vfrLimits.
//
// rating is the worst severity reached by any factor -- perfect unless something drags it
// down. known is false only when the hour could not be scored at all: a nil daylight
// window means there is no way to tell a CAVOK afternoon from the middle of the night, so
// the frontend shows "no data" rather than a misleading rating.
//
// factors lists every factor that reached worse than perfect, worst first, and is what the
// API hands the frontend to explain the rating. Every factor is evaluated regardless of
// whether an earlier one already reached no-go: two factors can both be past their wall in
// the same hour, and both are the reason, not whichever happened to come first in the
// table.
//
// visibilityKnown reports whether the model supplied a visibility for this hour. Those
// hours are still scored on the remaining factors; the caller presents them as estimates.
func scoreVFR(c conditions) (rating severity, known bool, factors []VfrFactor, visibilityKnown bool) {
	if c.daylight == nil {
		return perfect, false, nil, false
	}
	visibilityKnown = c.visibilityKM != nil

	slog.Debug("scoring vfr rating", "hour", c.time.Format(time.RFC822))

	rating = perfect
	// sev is kept alongside each VfrFactor only long enough to sort by it; the wire format
	// carries the severity as a string, not this internal ordinal.
	type rated struct {
		sev    severity
		factor VfrFactor
	}
	var collected []rated

	for _, f := range vfrLimits {
		v, ok := f.value(c)
		if !ok {
			continue
		}

		effective, scaleValue, scaled := f.effectiveValue(c, v)
		sev, isNoGo := f.evaluate(effective)
		if isNoGo {
			sev = noGo
		}
		if sev > rating {
			rating = sev
		}
		if sev == perfect {
			continue
		}

		vf := VfrFactor{Factor: f.name, Value: v, Unit: f.unit, Severity: sev.String()}
		if scaled {
			vf.Scale = &VfrScale{Name: f.scaledBy.name, Value: scaleValue, Unit: f.scaledBy.unit}
			slog.Debug("vfr factor", "factor", f.name, "value", v, "unit", f.unit,
				"severity", sev.String(), f.scaledBy.name, scaleValue)
		} else {
			slog.Debug("vfr factor", "factor", f.name, "value", v, "unit", f.unit, "severity", sev.String())
		}

		collected = append(collected, rated{sev, vf})
	}

	// Worst first, so the tooltip leads with the reason that dominates the rating. Ties
	// keep table order, which already puts cloud base and visibility ahead of the rest.
	sort.SliceStable(collected, func(i, j int) bool { return collected[i].sev > collected[j].sev })
	for _, r := range collected {
		factors = append(factors, r.factor)
	}

	return rating, true, factors, visibilityKnown
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

	ascending := f.worseHigher()
	for i := 1; i < len(f.curve); i++ {
		prev, cur := f.curve[i-1], f.curve[i]

		if cur.severity <= prev.severity {
			return fmt.Errorf("anchor %d (%s) does not come after %s", i, cur.severity, prev.severity)
		}
		// noGo is not a band a value can land in: it is what `wall` says about the space
		// past the last anchor, so it has no threshold of its own.
		if cur.severity == noGo {
			return fmt.Errorf("anchor %d is a no-go; set wall instead", i)
		}
		if ascending && cur.at <= prev.at {
			return fmt.Errorf("threshold %v at anchor %d does not rise above %v", cur.at, i, prev.at)
		}
		if !ascending && cur.at >= prev.at {
			return fmt.Errorf("threshold %v at anchor %d does not fall below %v", cur.at, i, prev.at)
		}
	}

	return f.scaledBy.validate(f.wall)
}

// validate checks a factor's scale. Scaling a factor that also carries a wall is refused
// rather than defined, because "does an unlikely deluge still end the hour" is a real
// question and should be answered deliberately, in the open, and not fall out of whichever
// discount happens to apply first.
func (s *scale) validate(wall bool) error {
	if s == nil {
		return nil
	}
	if wall {
		return fmt.Errorf("is scaled by %s and also carries a wall; pick one", s.name)
	}
	if len(s.points) < 2 {
		return fmt.Errorf("scale %q needs at least two points, has %d", s.name, len(s.points))
	}

	for i, p := range s.points {
		if p.weight < 0 || p.weight > 1 {
			return fmt.Errorf("scale %q: weight %v at point %d is outside 0..1", s.name, p.weight, i)
		}
		if i == 0 {
			continue
		}
		prev := s.points[i-1]
		if p.at <= prev.at {
			return fmt.Errorf("scale %q: %v at point %d does not rise above %v", s.name, p.at, i, prev.at)
		}
		if p.weight < prev.weight {
			return fmt.Errorf("scale %q: weight %v at point %d falls below %v -- a likelier "+
				"value must not discount less", s.name, p.weight, i, prev.weight)
		}
	}
	return nil
}
