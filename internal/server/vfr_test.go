package server

import (
	"math"
	"testing"
	"time"
)

// scoringConditions is the baseline hour: midday, no cloud, calm, dry, mild, 60km
// visibility. It rates perfect, so any case below isolates one factor by changing one
// field.
//
// 60km rather than 40: the table's perfect limit for visibility is 50, so a 40km baseline
// would quietly mark every case in this file good and stop isolating anything.
func scoringConditions(t *testing.T) conditions {
	t.Helper()

	ts, err := time.Parse(time.RFC3339, midday+":00Z")
	if err != nil {
		t.Fatalf("bad fixture time: %v", err)
	}
	return conditions{
		time:         ts,
		daylight:     testDayLight(t),
		visibilityKM: ptrFloat(60),
		temperature:  18,
	}
}

// at returns the baseline with the hour moved, for the daylight cases.
func (c conditions) at(t *testing.T, timeStr string) conditions {
	t.Helper()

	ts, err := time.Parse(time.RFC3339, timeStr+":00Z")
	if err != nil {
		t.Fatalf("bad fixture time %q: %v", timeStr, err)
	}
	c.time = ts
	return c
}

// The shipped table has to satisfy the same rules init() enforces. init() would already
// have panicked before this test ran, but a failure here names the factor and the reason.
func TestVFRLimitsIsWellFormed(t *testing.T) {
	for _, f := range vfrLimits {
		if err := f.validate(); err != nil {
			t.Errorf("factor %q: %v", f.name, err)
		}
	}
}

func TestFactorValidate_RejectsMalformedCurves(t *testing.T) {
	tests := []struct {
		name  string
		curve []anchor
		wall  bool
		scale *scale
	}{
		{name: "a single anchor", curve: []anchor{{perfect, 10}}},
		{name: "no perfect anchor first", curve: []anchor{{good, 10}, {difficult, 20}}},
		{name: "severities out of order", curve: []anchor{{perfect, 10}, {critical, 20}, {good, 30}}},
		{name: "a repeated severity", curve: []anchor{{perfect, 10}, {good, 20}, {good, 30}}},
		{name: "thresholds that turn around", curve: []anchor{{perfect, 10}, {good, 20}, {difficult, 15}}},
		{name: "a repeated threshold", curve: []anchor{{perfect, 10}, {good, 20}, {difficult, 20}}},

		// The wall is a property of the factor, not a band a value lands in: a no-go
		// anchor would need a threshold, and then there would be two ways to say the same
		// thing and no reason to expect them to agree.
		{name: "a no-go anchor", curve: []anchor{{perfect, 10}, {noGo, 20}}},

		// A scale discounts an accumulating value. Scaling a wall would mean deciding, by
		// side effect, whether an unlikely deluge still ends the hour.
		{
			name:  "a scale on a factor that also has a wall",
			curve: []anchor{{perfect, 10}, {good, 20}}, wall: true,
			scale: &scale{name: "probability", points: []scalePoint{{0, 0.2}, {100, 1}}},
		},
		{
			name:  "a scale with a single point",
			curve: []anchor{{perfect, 10}, {good, 20}},
			scale: &scale{name: "probability", points: []scalePoint{{0, 0.2}}},
		},
		{
			name:  "scale points that run backwards",
			curve: []anchor{{perfect, 10}, {good, 20}},
			scale: &scale{name: "probability", points: []scalePoint{{0, 0.2}, {100, 0.5}, {50, 1}}},
		},
		{
			name:  "a weight above 1",
			curve: []anchor{{perfect, 10}, {good, 20}},
			scale: &scale{name: "probability", points: []scalePoint{{0, 0.2}, {100, 1.5}}},
		},
		{
			name:  "a weight below 0",
			curve: []anchor{{perfect, 10}, {good, 20}},
			scale: &scale{name: "probability", points: []scalePoint{{0, -0.1}, {100, 1}}},
		},
		{
			// Otherwise an hour's rain gets more forgivable as it gets likelier.
			name:  "a weight that dips",
			curve: []anchor{{perfect, 10}, {good, 20}},
			scale: &scale{name: "probability", points: []scalePoint{{0, 0.5}, {50, 0.3}, {100, 1}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := factor{name: "test", curve: tc.curve, wall: tc.wall, scaledBy: tc.scale}
			if err := f.validate(); err == nil {
				t.Error("validate() = nil, want an error")
			}
		})
	}
}

func TestScaleWeight(t *testing.T) {
	s := &scale{
		name:   "probability",
		unit:   "%",
		points: []scalePoint{{0, 0.15}, {30, 0.25}, {50, 0.55}, {70, 0.85}, {100, 1.0}},
	}

	tests := []struct {
		name  string
		value float64
		want  float64
	}{
		{"at the first point", 0, 0.15},
		{"below the first point clamps", -20, 0.15},
		{"at an inner point", 50, 0.55},
		{"at the last point", 100, 1.0},
		{"above the last point clamps", 140, 1.0},
		{"interpolated across a shallow segment", 15, 0.20},
		{"interpolated across a steep segment", 60, 0.70},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.weight(tc.value); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("weight(%v) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// An unknown probability is not a low one. Discounting a factor because the data is missing
// would understate exactly the hours nothing is known about -- the same principle that keeps
// a missing visibility from scoring as a clear one.
func TestFactorEffectiveValue_MissingDataDoesNotDiscount(t *testing.T) {
	f := factor{
		name:  "test",
		curve: []anchor{{perfect, 0}, {good, 10}},
		scaledBy: &scale{
			name:   "probability",
			by:     func(conditions) (float64, bool) { return 0, false },
			points: []scalePoint{{0, 0.1}, {100, 1}},
		},
	}

	effective, _, scaled := f.effectiveValue(scoringConditions(t), 8)

	if effective != 8 {
		t.Errorf("effective = %v, want the raw value unchanged when the scaling value is unavailable", effective)
	}
	if scaled {
		t.Error("scaled = true, want false so the breakdown does not claim a scale it did not apply")
	}
}

// A likely value is discounted less; certainty (weight 1) must leave the value untouched.
func TestFactorEffectiveValue_DiscountsTowardThePerfectAnchor(t *testing.T) {
	f := factor{
		name:  "test",
		curve: []anchor{{perfect, 0}, {good, 10}, {critical, 20}},
		scaledBy: &scale{
			name:   "probability",
			by:     func(conditions) (float64, bool) { return 25, true },
			points: []scalePoint{{0, 0.1}, {100, 1}},
		},
	}

	// weight(25) interpolates between (0, 0.1) and (100, 1) -> 0.1 + 25/100*0.9 = 0.325
	effective, scaleValue, scaled := f.effectiveValue(scoringConditions(t), 20)

	if !scaled {
		t.Fatal("scaled = false, want true when the scaling value is available")
	}
	if scaleValue != 25 {
		t.Errorf("scaleValue = %v, want 25", scaleValue)
	}
	if want := 20 * 0.325; math.Abs(effective-want) > 1e-9 {
		t.Errorf("effective = %v, want %v (perfect anchor is 0, so this reduces to value*weight)", effective, want)
	}
}

// Every anchor in the shipped table must report exactly the severity it names. This and the
// test below are driven off vfrLimits rather than a copy of it, so retuning the table does
// not mean rewriting them -- only a curve that no longer passes through its own anchors
// fails.
func TestFactorEvaluate_HitsItsAnchors(t *testing.T) {
	for _, f := range vfrLimits {
		for i, a := range f.curve {
			sev, isNoGo := f.evaluate(a.at)

			if isNoGo {
				t.Errorf("%s at %v %s: isNoGo = true, want false -- an anchor is a band, not the wall", f.name, a.at, f.unit)
			}
			// An anchor names the band that ends at it -- except the perfect one, where
			// there is no band below to be in.
			want := a.severity
			if i == 0 {
				want = perfect
			}
			if sev != want {
				t.Errorf("%s at %v %s: severity = %s, want %s", f.name, a.at, f.unit, sev, want)
			}
		}
	}
}

// A value strictly between two anchors takes the upper (worse) one's severity -- the band
// it is climbing toward, not the one it left.
func TestFactorEvaluate_BetweenAnchorsTakesTheUpperSeverity(t *testing.T) {
	for _, f := range vfrLimits {
		for i := 0; i+1 < len(f.curve); i++ {
			lo, hi := f.curve[i], f.curve[i+1]
			mid := (lo.at + hi.at) / 2

			sev, isNoGo := f.evaluate(mid)

			if isNoGo {
				t.Errorf("%s at %v %s: isNoGo = true short of the wall", f.name, mid, f.unit)
			}
			if sev != hi.severity {
				t.Errorf("%s at %v %s: severity = %s, want %s", f.name, mid, f.unit, sev, hi.severity)
			}
		}
	}
}

// A factor without a wall stops at its last anchor's severity instead of escalating
// further, rather than extrapolating past a category that was never given a name.
//
// Driven off curves declared here rather than through scoreVFR: which factors carry a wall
// is a tuning decision, and this is testing the mechanism rather than the table.
func TestFactorEvaluate_ClampsBeyondTheLastAnchor(t *testing.T) {
	tests := []struct {
		name  string
		curve []anchor
		value float64
	}{
		{
			name:  "worse as the value rises",
			curve: []anchor{{perfect, 10}, {good, 20}, {difficult, 30}},
			value: 1000,
		},
		{
			name:  "worse as the value falls",
			curve: []anchor{{perfect, 30}, {good, 20}, {difficult, 10}},
			value: -1000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := factor{name: "test", unit: "x", curve: tc.curve}

			sev, isNoGo := f.evaluate(tc.value)

			if isNoGo {
				t.Error("isNoGo = true for a factor with no wall")
			}
			if sev != difficult {
				t.Errorf("severity = %s, want difficult (the last anchor's, not an extrapolation)", sev)
			}
		})
	}
}

// The fourth limit is the wall, so the boundary itself is the worst *scoring* value rather
// than the first no-go. Without this the table has two readings -- "critical at 16kn" and
// "no-go at 16kn" -- and no reason to expect a future retune to keep them apart.
func TestScoreVFR_TheLastLimitScoresRatherThanEndsTheHour(t *testing.T) {
	tests := []struct {
		name    string
		with    func(c conditions) conditions
		want    severity
		wantWhy string
	}{
		{
			name: "exactly at the crosswind limit",
			with: func(c conditions) conditions { c.crosswind = 16; return c },
			want: critical,
		},
		{
			name: "just past the crosswind limit",
			with: func(c conditions) conditions { c.crosswind = 16.1; return c },
			want: noGo, wantWhy: "crosswind",
		},
		{
			name: "exactly at the ceiling limit",
			with: func(c conditions) conditions { c.cloudBaseFL = ptrInt(10); return c },
			want: critical,
		},
		{
			name: "just past the ceiling limit",
			with: func(c conditions) conditions { c.cloudBaseFL = ptrInt(9); return c },
			want: noGo, wantWhy: "cloud base",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rating, _, factors, _ := scoreVFR(tc.with(scoringConditions(t)))

			if rating != tc.want {
				t.Errorf("rating = %s, want %s", rating, tc.want)
			}
			if tc.wantWhy == "" {
				return
			}
			if len(factors) != 1 || factors[0].Factor != tc.wantWhy {
				t.Errorf("factors = %+v, want just %q", factors, tc.wantWhy)
			}
		})
	}
}

// The calibration, in the terms the table is tuned in. The anchor and between-anchor tests
// above already walk every segment of every curve, so this only carries what they cannot
// show: one mid-band value per factor, the guard on precipitation probability, and what a
// multi-factor hour rates as.
func TestScoreVFR_Calibration(t *testing.T) {
	tests := []struct {
		name       string
		with       func(c conditions) conditions
		want       severity
		wantFactor string // only checked when non-empty
	}{
		{
			name: "a clear midday hour rates perfect",
			with: func(c conditions) conditions { return c },
			want: perfect,
		},
		{
			name:       "a ceiling at FL25 is good",
			with:       func(c conditions) conditions { c.cloudBaseFL = ptrInt(25); return c },
			want:       good,
			wantFactor: "cloud base",
		},
		{
			name:       "12km of visibility is difficult",
			with:       func(c conditions) conditions { c.visibilityKM = ptrFloat(12); return c },
			want:       difficult,
			wantFactor: "visibility",
		},
		{
			name:       "18kn of wind is difficult",
			with:       func(c conditions) conditions { c.windSpeed = 18; return c },
			want:       difficult,
			wantFactor: "wind",
		},
		{
			// The two wind factors overlap on every hour, but crosswind is the one that
			// decides whether you land: the same 14kn that is unremarkable as total wind
			// is already critical here.
			name:       "14kn of crosswind is critical",
			with:       func(c conditions) conditions { c.crosswind = 14; return c },
			want:       critical,
			wantFactor: "crosswind",
		},
		{
			// Precipitation is one factor: what would fall, discounted by how likely it is
			// to fall, before its severity is read. At 0% the discount is as low as it
			// goes (0.1, never a full discount to nothing), so 2mm/h reads as barely
			// non-perfect rather than as the 2mm/h it might never become.
			name:       "2mm/h at 0 percent is barely good",
			with:       func(c conditions) conditions { c.precipitation, c.precipitationProbability = 2, 0; return c },
			want:       good,
			wantFactor: "precipitation",
		},
		{
			// The same amount reads worse once it is more likely to actually happen.
			name:       "2mm/h at 70 percent is critical",
			with:       func(c conditions) conditions { c.precipitation, c.precipitationProbability = 2, 70; return c },
			want:       critical,
			wantFactor: "precipitation",
		},
		{
			// No wall on precipitation: an unlikely deluge is not a decision worth
			// shipping, so the severity stops at critical instead of forcing a no-go.
			name: "20mm/h at 100 percent is critical, not a no-go",
			with: func(c conditions) conditions { c.precipitation, c.precipitationProbability = 20, 100; return c },
			want: critical,
		},
		{
			name:       "32C is difficult",
			with:       func(c conditions) conditions { c.temperature = 32; return c },
			want:       difficult,
			wantFactor: "temperature",
		},
		{
			// Difficult rather than critical: legal but uncomfortable, and no longer
			// worth as much as a critical crosswind now that there is no weight to say so
			// with a smaller number.
			name:       "an hour inside civil twilight is difficult",
			with:       func(c conditions) conditions { return c.at(t, "2026-08-03T03:00") },
			want:       difficult,
			wantFactor: "daylight",
		},
		{
			// Nothing here is past a wall; the hour rates on its worst factor alone, not an
			// accumulation of the others. Three factors sit exactly at their good anchor;
			// visibility alone reaches critical, and that is what the hour rates.
			name: "a multi-factor hour rates its single worst factor, not an accumulation",
			with: func(c conditions) conditions {
				c.cloudBaseFL = ptrInt(25)   // good
				c.windSpeed = 10             // good
				c.crosswind = 5              // good
				c.visibilityKM = ptrFloat(5) // critical
				return c
			},
			want:       critical,
			wantFactor: "visibility",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rating, _, factors, _ := scoreVFR(tc.with(scoringConditions(t)))

			if rating != tc.want {
				t.Errorf("rating = %s, want %s", rating, tc.want)
			}
			if tc.wantFactor == "" {
				return
			}
			if len(factors) == 0 || factors[0].Factor != tc.wantFactor {
				t.Errorf("factors = %+v, want %q worst-first", factors, tc.wantFactor)
			}
		})
	}
}

// One factor past its limit ends the hour, whatever the rest of the weather is doing.
//
// Each case has to leave every other factor clear of its own limit, so the reported factor
// is unambiguously the one that triggered it.
func TestScoreVFR_NoGos(t *testing.T) {
	tests := []struct {
		name   string
		with   func(c conditions) conditions
		factor string
	}{
		{
			name:   "a ceiling below the limit",
			with:   func(c conditions) conditions { c.cloudBaseFL = ptrInt(8); return c },
			factor: "cloud base",
		},
		{
			name:   "visibility below the limit",
			with:   func(c conditions) conditions { c.visibilityKM = ptrFloat(3); return c },
			factor: "visibility",
		},
		{
			name:   "wind past the limit",
			with:   func(c conditions) conditions { c.windSpeed = 32; return c },
			factor: "wind",
		},
		{
			name:   "crosswind past the limit",
			with:   func(c conditions) conditions { c.crosswind = 20; return c },
			factor: "crosswind",
		},
		{
			name:   "heat past the limit",
			with:   func(c conditions) conditions { c.temperature = 41; return c },
			factor: "temperature",
		},
		{
			name:   "after civil twilight",
			with:   func(c conditions) conditions { return c.at(t, "2026-08-03T21:00") },
			factor: "daylight",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rating, known, factors, _ := scoreVFR(tc.with(scoringConditions(t)))

			if !known {
				t.Fatal("known = false, want true -- this hour was scored, just badly")
			}
			if rating != noGo {
				t.Errorf("rating = %s, want no-go", rating)
			}
			// The reason has to survive to the frontend, or a no-go is indistinguishable
			// from an hour that merely had one difficult factor among several.
			if len(factors) != 1 {
				t.Fatalf("factors = %+v, want exactly the one that ended the hour", factors)
			}
			if factors[0].Factor != tc.factor {
				t.Errorf("factor = %q, want %q", factors[0].Factor, tc.factor)
			}
			if factors[0].Severity != noGo.String() {
				t.Errorf("severity = %q, want %q", factors[0].Severity, noGo.String())
			}
		})
	}
}

// Two factors can each be past their own wall in the same hour. Both are the reason -- not
// whichever happened to come first in the table, which is what a short-circuiting evaluator
// would report instead.
func TestScoreVFR_TwoFactorsPastTheirWallAreBothReported(t *testing.T) {
	c := scoringConditions(t)
	c.windSpeed = 32   // past the wind wall
	c.crosswind = 2    // clear
	c.temperature = 41 // past the temperature wall

	rating, _, factors, _ := scoreVFR(c)

	if rating != noGo {
		t.Fatalf("rating = %s, want no-go", rating)
	}
	if len(factors) != 2 {
		t.Fatalf("factors = %+v, want both wind and temperature", factors)
	}
	got := map[string]bool{factors[0].Factor: true, factors[1].Factor: true}
	if !got["wind"] || !got["temperature"] {
		t.Errorf("factors = %+v, want wind and temperature, both no-go", factors)
	}
	for _, f := range factors {
		if f.Severity != noGo.String() {
			t.Errorf("%s severity = %q, want %q", f.Factor, f.Severity, noGo.String())
		}
	}
}

// Open-Meteo drops visibility beyond the ICON-EU horizon, which is the tail of every
// forecast. The regression this guards: nil visibility used to force the rating to
// "unknown", discarding every other factor for the ~41 tail hours.
func TestScoreVFR_UnknownVisibilityStillScores(t *testing.T) {
	c := scoringConditions(t)
	c.visibilityKM = nil
	c.cloudBaseFL = ptrInt(25)

	rating, known, factors, visKnown := scoreVFR(c)

	if visKnown {
		t.Error("visibilityKnown = true, want false when visibility is nil")
	}
	if !known {
		t.Error("known = false, want true -- the hour was still scored on what it had")
	}
	if rating != good {
		t.Errorf("rating = %s, want good (the cloud base factor must still apply)", rating)
	}
	if len(factors) != 1 || factors[0].Factor != "cloud base" {
		t.Errorf("factors = %+v, want just cloud base", factors)
	}
}

func TestScoreVFR_KnownVisibility(t *testing.T) {
	rating, known, factors, visKnown := scoreVFR(scoringConditions(t))

	if !visKnown {
		t.Error("visibilityKnown = false, want true when visibility is present")
	}
	if !known {
		t.Error("known = false, want true")
	}
	if rating != perfect {
		t.Errorf("rating = %s, want perfect", rating)
	}
	if len(factors) != 0 {
		t.Errorf("factors = %+v, want none on a clear hour", factors)
	}
}

// A date resolveDaylight could not look up is absent from its map, so the hour arrives here
// with a nil window. Scoring it on the remaining factors would report a CAVOK afternoon and
// the middle of the night identically, so it must yield "not scored" instead.
func TestScoreVFR_NoDaylightWindow(t *testing.T) {
	c := scoringConditions(t)
	c.daylight = nil

	rating, known, factors, visKnown := scoreVFR(c)

	if known {
		t.Error("known = true, want false without a daylight window")
	}
	if rating != perfect {
		t.Errorf("rating = %s, want its zero value when nothing was scored", rating)
	}
	if visKnown {
		t.Error("visibilityKnown = true, want false when no rating was computed")
	}
	if factors != nil {
		t.Errorf("factors = %+v, want none when no rating was computed", factors)
	}
}

// The breakdown is what answers "why is this hour difficult?" in the tooltip, so it has to
// lead with the factor that dominates the rating and list every factor that is not perfect,
// not only the worst one.
func TestScoreVFR_BreakdownExplainsTheRating(t *testing.T) {
	c := scoringConditions(t)
	c.crosswind = 14   // critical
	c.windSpeed = 12   // difficult
	c.temperature = 26 // good

	rating, _, factors, _ := scoreVFR(c)

	if rating != critical {
		t.Fatalf("rating = %s, want critical", rating)
	}
	if len(factors) != 3 {
		t.Fatalf("factors = %+v, want one per factor that is not perfect", factors)
	}
	for _, f := range factors {
		if f.Unit == "" {
			t.Errorf("%+v: want the unit the value is in", f)
		}
	}
	if factors[0].Factor != "crosswind" {
		t.Errorf("factors[0].Factor = %q, want the dominant factor first", factors[0].Factor)
	}
	if factors[0].Severity != critical.String() {
		t.Errorf("factors[0].Severity = %q, want %q", factors[0].Severity, critical.String())
	}
}

// A discounted factor's severity alone does not identify the hour: near-certain drizzle and
// an unlikely downpour can land on the same band by design if the numbers are chosen badly.
// The breakdown has to carry what discounted it, or the tooltip cannot tell those two apart.
//
// The inputs are the wettest hour in the month of EDWN data this calibration was checked
// against, so this doubles as the one real-world case in the suite.
func TestScoreVFR_BreakdownCarriesTheScale(t *testing.T) {
	c := scoringConditions(t)
	c.precipitation, c.precipitationProbability = 3.2, 88

	rating, _, factors, _ := scoreVFR(c)

	if rating != critical {
		t.Errorf("rating = %s, want critical", rating)
	}
	if len(factors) != 1 {
		t.Fatalf("factors = %+v, want just the precipitation one", factors)
	}
	got := factors[0]

	if got.Scale == nil {
		t.Fatal("Scale = nil, want the probability that discounted this factor")
	}
	if got.Scale.Name != "probability" || got.Scale.Value != 88 || got.Scale.Unit != "%" {
		t.Errorf("Scale = %+v, want probability 88%%", *got.Scale)
	}
	if got.Value != 3.2 {
		t.Errorf("Value = %v, want the amount in the factor's own unit, unscaled", got.Value)
	}
}

// Only a factor that declares a scale gets one, so an unscaled factor must not sprout an
// empty object in the JSON.
func TestScoreVFR_UnscaledFactorsCarryNoScale(t *testing.T) {
	c := scoringConditions(t)
	c.windSpeed = 12

	_, _, factors, _ := scoreVFR(c)

	if len(factors) != 1 {
		t.Fatalf("factors = %+v, want just the wind one", factors)
	}
	if factors[0].Scale != nil {
		t.Errorf("Scale = %+v, want nil on a factor with no scale", *factors[0].Scale)
	}
}
