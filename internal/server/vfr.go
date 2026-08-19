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

// anchor is one point on a factor's curve: at `at`, in the factor's unit, the factor has
// reached this severity. What that costs comes from the ladder below, not from the anchor.
type anchor struct {
	severity severity
	at       float64
}

// severityCost is the ladder every factor is scored against. A severity means the same
// thing everywhere; only where a factor reaches it, and how heavily it counts, are the
// factor's own business.
//
// The shape is deliberate: "good" is nearly free, because an hour that is merely good on
// four counts is still a fine hour and the previous table nibbled it down to 75. From
// there it steepens, so that one factor at its critical limit dominates the score rather
// than being outvoted by three minor ones.
var severityCost = map[severity]float64{
	perfect:   0,
	good:      1,
	difficult: 15,
	critical:  50,
}

// noGoPenaltyCost is what the breakdown reports for the factor that ended the hour. It is
// not subtracted from anything -- a no-go returns 0% outright -- and the frontend renders
// it as a reason rather than as a subtraction.
const noGoPenaltyCost = 100

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

	// weight multiplies the ladder for this factor. 1.0 means "a critical value here is
	// what critical means"; above it the factor outranks its peers. It is the only place
	// one factor is allowed to matter more than another, which is what keeps the severity
	// words comparable across the table.
	weight float64

	// wall makes the last anchor a no-go: past it the hour scores 0 outright. Without it
	// the cost stops rising at the last anchor instead of ending the hour -- precipitation
	// is the case, because its cost is scaled by a probability and a no-go multiplied by
	// an unlikely forecast is not a decision anyone should ship.
	wall bool

	// scaledBy, when set, multiplies this factor's cost by how likely its value is to
	// materialise. The curve stays in the factor's own unit, so its anchors keep reading
	// as "this much, if it happens", and the scale answers "and how likely is that".
	//
	// Precipitation is the case this exists for: amount and probability are not
	// independent. 2mm/h at 20% or at 60% is much the same hour; 15mm/h at 20% or at 60%
	// is not, and no penalty added to the amount can express that, because the amount
	// plays no part in what the addition costs.
	scaledBy *scale
}

// scale modulates a factor's cost by a second quantity.
//
// The weight is a curve of its own rather than a formula, so the shape is data: a straight
// line is the expected-cost reading, and anything else is a deliberate risk attitude that
// gets retuned the same way every other number in this file does.
type scale struct {
	name string // "probability" -- for the breakdown
	unit string // "%"

	// by extracts the modulating value. ok=false means no scaling at all, i.e. weight 1.
	// Missing data must never discount a penalty: an unknown probability is not a low one.
	by func(c conditions) (v float64, ok bool)

	// points are ordered by `at`, with the weight interpolated between them and clamped
	// outside them -- the same shape rule the cost curves follow.
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

// weightFor resolves the multiplier for one hour, including the unscaled and missing-data
// cases. The second return is the modulating value, for the breakdown.
func (f factor) weightFor(c conditions) (w float64, v float64, scaled bool) {
	if f.scaledBy == nil {
		return 1, 0, false
	}
	v, ok := f.scaledBy.by(c)
	if !ok {
		return 1, 0, false
	}
	return f.scaledBy.weight(v), v, true
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
// A factor is four thresholds and one weight. Read `{difficult, 20}` as "at FL20 this
// factor has become difficult"; what difficult costs is severityCost, the same everywhere,
// times the factor's weight. Costs ramp linearly between anchors, and `wall: true` makes
// the last anchor a no-go -- the value that ends the hour outright.
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
			{perfect, 50},
			{good, 25},
			{difficult, 20},
			{critical, 10},
		},
		weight: 1.0,
		wall:   true,
	},
	{
		// Open-Meteo drops visibility beyond the ICON-EU horizon, which is the tail of
		// every forecast. Those hours are scored on everything else and flagged as
		// estimates rather than being penalised for the gap.
		//
		// The heaviest factor in the table: it is the one that decides whether the ground
		// is in sight, and the model's own visibility already carries the effect of rain
		// and mist, so it is doing more work than its single number suggests.
		name: "visibility",
		unit: "km",
		value: func(c conditions) (float64, bool) {
			if c.visibilityKM == nil {
				return 0, false
			}
			return *c.visibilityKM, true
		},
		curve: []anchor{
			{perfect, 50},
			{good, 30},
			{difficult, 10},
			{critical, 5},
		},
		weight: 1.3,
		wall:   true,
	},
	{
		// Total wind overlaps crosswind on every hour: a strong wind straight down the
		// runway is not the problem a strong crosswind is, so its limits sit wider apart
		// and the two are left to add rather than one being discounted.
		name:  "wind",
		unit:  "kn",
		value: func(c conditions) (float64, bool) { return c.windSpeed, true },
		curve: []anchor{
			{perfect, 5},
			{good, 10},
			{difficult, 20},
			{critical, 30},
		},
		weight: 1.0,
		wall:   true,
	},
	{
		// Crosswind is against the best runway end -- crosswindComponent takes the
		// minimum over the airport's runway headings.
		name:  "crosswind",
		unit:  "kn",
		value: func(c conditions) (float64, bool) { return c.crosswind, true },
		curve: []anchor{
			{perfect, 2},
			{good, 5},
			{difficult, 10},
			{critical, 15},
		},
		weight: 1.0,
		wall:   true,
	},
	{
		// What costs points is the gust's margin over the steady crosswind, not its
		// absolute value: 5 gusting 15 is harder to land in than a steady 15. A wide
		// spread is a sign of heavy gusting in its own right, whatever the steady
		// crosswind is doing, which is why this carries a wall of its own.
		name:  "crosswind gust spread",
		unit:  "kn",
		value: func(c conditions) (float64, bool) { return c.crosswindGusts - c.crosswind, true },
		curve: []anchor{
			{perfect, 2},
			{good, 5},
			{difficult, 10},
			{critical, 15},
		},
		weight: 1.0,
		wall:   true,
	},
	{
		// The anchors are what the rain is worth if it falls; the scale then asks how
		// likely that is. No wall, because a no-go multiplied by an unlikely forecast is
		// not a decision worth shipping -- the cost stops rising instead.
		//
		// The limits are where this airfield's rain actually lives. Three years of ERA5 at
		// EDWN: half of all wet hours are below 0.2mm/h, 1.0 is the 90th percentile
		// (~280 h/year), and 4.0 is moderate-and-above (~20 h/year). The old table put its
		// worst anchor at 20mm/h, which has never once occurred here, so every real rain
		// hour scored as barely wet.
		name:  "precipitation",
		unit:  "mm/h",
		value: func(c conditions) (float64, bool) { return c.precipitation, true },
		curve: []anchor{
			{perfect, 0},
			{good, 0.2},
			{difficult, 1.0},
			{critical, 4.0},
		},
		weight: 1.0,
		scaledBy: &scale{
			// Open-Meteo's precipitation_probability is the ensemble's P(>0.1mm in the
			// hour), while the amount comes from the deterministic run -- so the two can
			// disagree, and a month at EDWN contains 2.7mm/h forecast at 3%.
			//
			// The weights are decision-shaped rather than a straight line: below 30% the
			// hour is not in question, above 70% it is decided, and the middle is where
			// the answer actually moves. A straight line would be the expected-cost
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
			{good, 28},
			{difficult, 32},
			{critical, 38},
		},
		weight: 1.0,
		wall:   true,
	},
	{
		// Legal but not comfortable inside civil twilight; outside it, not legal at all.
		//
		// An ordinal, so it has one named band and a wall rather than four: there is no
		// continuous scale between day and night to place limits on. Weight 0.5 puts
		// twilight at 25 -- a real cost, but not the one a critical crosswind carries.
		name:  "daylight",
		unit:  "",
		value: func(c conditions) (float64, bool) { return daylightOrdinal(c), true },
		curve: []anchor{
			{perfect, daylightDay},
			{critical, daylightTwilight},
		},
		weight: 0.5,
		wall:   true,
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
// moment it leaves the good anchor until it reaches the difficult one. Past the last anchor
// the hour is a no-go if the factor has a wall, and otherwise stays at the last band's cost.
//
// The cost is returned unrounded and unscaled: scoreVFR applies any scale and rounds once,
// because rounding twice would stop the breakdown adding up to the score on screen.
func (f factor) evaluate(v float64) (cost float64, sev severity, isNoGo bool) {
	// Normalise so the curve always runs in ascending order, and compare in that space.
	sign := 1.0
	if !f.worseHigher() {
		sign = -1.0
	}
	at := func(i int) float64 { return sign * f.curve[i].at }
	costAt := func(i int) float64 { return severityCost[f.curve[i].severity] * f.weight }
	n := sign * v
	last := len(f.curve) - 1

	if n <= at(0) {
		return 0, perfect, false
	}
	if n > at(last) {
		if f.wall {
			return 0, noGo, true
		}
		// No wall: the curve stops rising rather than extrapolating.
		return costAt(last), f.curve[last].severity, false
	}

	// Find the segment (at(i), at(i+1)] that holds the value, and ramp across it.
	i := 0
	for i+1 < last && n > at(i+1) {
		i++
	}
	frac := (n - at(i)) / (at(i+1) - at(i))
	cost = costAt(i) + frac*(costAt(i+1)-costAt(i))

	return cost, f.curve[i+1].severity, false
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

		raw, sev, isNoGo := f.evaluate(v)
		if isNoGo {
			slog.Debug("vfr no-go", "factor", f.name, "value", v, "unit", f.unit)
			return 0, []VfrPenalty{{Factor: f.name, Value: v, Unit: f.unit, Severity: sev.String(), Cost: noGoPenaltyCost}}, visibilityKnown
		}

		// A scale never applies to a no-go -- validate() rejects a factor carrying both --
		// so the weight is only ever reached here, on an accumulating cost.
		w, scaleValue, scaled := f.weightFor(c)
		cost := int(math.Round(raw * w))
		if cost <= 0 {
			continue
		}

		penalty := VfrPenalty{
			Factor: f.name, Value: v, Unit: f.unit, Severity: sev.String(), Cost: cost,
		}
		if scaled {
			penalty.Scale = &VfrScale{Name: f.scaledBy.name, Value: scaleValue, Unit: f.scaledBy.unit}
			slog.Debug("vfr penalty applied", "factor", f.name, "value", v, "unit", f.unit,
				"severity", sev.String(), "cost", cost, f.scaledBy.name, scaleValue, "weight", w)
		} else {
			slog.Debug("vfr penalty applied",
				"factor", f.name, "value", v, "unit", f.unit, "severity", sev.String(), "cost", cost)
		}

		penalties = append(penalties, penalty)
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
	if f.weight <= 0 {
		return fmt.Errorf("weight must be positive, is %v", f.weight)
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
// multiplication happens to run first.
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
				"value must not cost less", s.name, p.weight, i, prev.weight)
		}
	}
	return nil
}
