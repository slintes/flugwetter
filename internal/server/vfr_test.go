package server

import (
	"math"
	"testing"
	"time"
)

// scoringConditions is the baseline hour: midday, no cloud, calm, dry, mild, 60km
// visibility. It scores 100, so any case below isolates one factor by changing one field.
//
// 60km rather than 40: the table's perfect limit for visibility is 50, so a 40km baseline
// would quietly charge every case in this file a point and stop isolating anything.
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
		name   string
		curve  []anchor
		weight float64
		wall   bool
		scale  *scale
	}{
		{name: "a single anchor", curve: []anchor{{perfect, 10}}, weight: 1},
		{name: "no perfect anchor first", curve: []anchor{{good, 10}, {difficult, 20}}, weight: 1},
		{name: "severities out of order", curve: []anchor{{perfect, 10}, {critical, 20}, {good, 30}}, weight: 1},
		{name: "a repeated severity", curve: []anchor{{perfect, 10}, {good, 20}, {good, 30}}, weight: 1},
		{name: "thresholds that turn around", curve: []anchor{{perfect, 10}, {good, 20}, {difficult, 15}}, weight: 1},
		{name: "a repeated threshold", curve: []anchor{{perfect, 10}, {good, 20}, {difficult, 20}}, weight: 1},
		{name: "no weight at all", curve: []anchor{{perfect, 10}, {good, 20}}},
		{name: "a negative weight", curve: []anchor{{perfect, 10}, {good, 20}}, weight: -1},

		// The wall is a property of the factor, not a band a value lands in: a no-go
		// anchor would need a threshold, and then there would be two ways to say the same
		// thing and no reason to expect them to agree.
		{name: "a no-go anchor", curve: []anchor{{perfect, 10}, {noGo, 20}}, weight: 1},

		// A scale multiplies an accumulating cost. Scaling a wall would mean deciding, by
		// side effect, whether an unlikely deluge still ends the hour.
		{
			name:   "a scale on a factor that also has a wall",
			curve:  []anchor{{perfect, 10}, {good, 20}},
			weight: 1, wall: true,
			scale: &scale{name: "probability", points: []scalePoint{{0, 0.2}, {100, 1}}},
		},
		{
			name:   "a scale with a single point",
			curve:  []anchor{{perfect, 10}, {good, 20}},
			weight: 1,
			scale:  &scale{name: "probability", points: []scalePoint{{0, 0.2}}},
		},
		{
			name:   "scale points that run backwards",
			curve:  []anchor{{perfect, 10}, {good, 20}},
			weight: 1,
			scale:  &scale{name: "probability", points: []scalePoint{{0, 0.2}, {100, 0.5}, {50, 1}}},
		},
		{
			name:   "a weight above 1",
			curve:  []anchor{{perfect, 10}, {good, 20}},
			weight: 1,
			scale:  &scale{name: "probability", points: []scalePoint{{0, 0.2}, {100, 1.5}}},
		},
		{
			name:   "a weight below 0",
			curve:  []anchor{{perfect, 10}, {good, 20}},
			weight: 1,
			scale:  &scale{name: "probability", points: []scalePoint{{0, -0.1}, {100, 1}}},
		},
		{
			// Otherwise an hour gets cheaper as its rain gets likelier.
			name:   "a weight that dips",
			curve:  []anchor{{perfect, 10}, {good, 20}},
			weight: 1,
			scale:  &scale{name: "probability", points: []scalePoint{{0, 0.5}, {50, 0.3}, {100, 1}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := factor{name: "test", curve: tc.curve, weight: tc.weight, wall: tc.wall, scaledBy: tc.scale}
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

// An unknown probability is not a low one. Discounting a penalty because the data is
// missing would understate exactly the hours nothing is known about -- the same principle
// that keeps a missing visibility from scoring as a clear one.
func TestFactorWeightFor_MissingDataDoesNotDiscount(t *testing.T) {
	f := factor{
		name:   "test",
		curve:  []anchor{{perfect, 0}, {good, 10}},
		weight: 1,
		scaledBy: &scale{
			name:   "probability",
			by:     func(conditions) (float64, bool) { return 0, false },
			points: []scalePoint{{0, 0.1}, {100, 1}},
		},
	}

	w, _, scaled := f.weightFor(scoringConditions(t))

	if w != 1 {
		t.Errorf("weight = %v, want 1 when the scaling value is unavailable", w)
	}
	if scaled {
		t.Error("scaled = true, want false so the breakdown does not claim a scale it did not apply")
	}
}

// Every anchor in the shipped table must cost exactly the ladder times the factor's weight.
// This and the ramp test below are driven off vfrLimits rather than a copy of it, so
// retuning the table does not mean rewriting them -- only a curve that no longer passes
// through its own anchors fails.
func TestFactorEvaluate_HitsItsAnchors(t *testing.T) {
	for _, f := range vfrLimits {
		for i, a := range f.curve {
			cost, sev, isNoGo := f.evaluate(a.at)

			if isNoGo {
				t.Errorf("%s at %v %s: isNoGo = true, want false -- an anchor is a band, not the wall", f.name, a.at, f.unit)
			}
			if want := severityCost[a.severity] * f.weight; math.Abs(cost-want) > 1e-9 {
				t.Errorf("%s at %v %s: cost = %v, want %v", f.name, a.at, f.unit, cost, want)
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

// The point of the curve model: halfway between two anchors costs halfway between their
// costs, rather than the whole of the upper one. This is what the stepped ladder got wrong.
func TestFactorEvaluate_RampsBetweenAnchors(t *testing.T) {
	for _, f := range vfrLimits {
		costAt := func(i int) float64 { return severityCost[f.curve[i].severity] * f.weight }

		for i := 0; i+1 < len(f.curve); i++ {
			lo, hi := f.curve[i], f.curve[i+1]
			mid := (lo.at + hi.at) / 2

			cost, sev, isNoGo := f.evaluate(mid)

			if isNoGo {
				t.Errorf("%s at %v %s: isNoGo = true short of the wall", f.name, mid, f.unit)
			}
			if want := (costAt(i) + costAt(i+1)) / 2; math.Abs(cost-want) > 1e-9 {
				t.Errorf("%s at %v %s: cost = %v, want %v (midpoint of %v and %v)",
					f.name, mid, f.unit, cost, want, costAt(i), costAt(i+1))
			}
			if want := hi.severity; sev != want {
				t.Errorf("%s at %v %s: severity = %s, want %s", f.name, mid, f.unit, sev, want)
			}
		}
	}
}

// A factor without a wall stops rising at its last anchor instead of extrapolating -- the
// ladder this replaced multiplied without bound, so 45C cost 51 points on its own.
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
			f := factor{name: "test", unit: "x", curve: tc.curve, weight: 1}

			cost, sev, isNoGo := f.evaluate(tc.value)

			if isNoGo {
				t.Error("isNoGo = true for a factor with no wall")
			}
			if want := severityCost[difficult]; cost != want {
				t.Errorf("cost = %v, want %v (the last anchor's cost, not an extrapolation)", cost, want)
			}
			if sev != difficult {
				t.Errorf("severity = %s, want difficult", sev)
			}
		})
	}
}

// The fourth limit is the wall, so the boundary itself is the worst scoring value rather
// than the first no-go. Without this the table has two readings -- "critical at 15kn" and
// "no-go at 15kn" -- and no reason to expect a future retune to keep them apart.
func TestScoreVFR_TheLastLimitScoresRatherThanEndsTheHour(t *testing.T) {
	tests := []struct {
		name    string
		with    func(c conditions) conditions
		want    int
		wantWhy string
	}{
		{
			name: "exactly at the crosswind limit",
			with: func(c conditions) conditions { c.crosswind, c.crosswindGusts = 15, 15; return c },
			want: 50,
		},
		{
			name: "just past the crosswind limit",
			with: func(c conditions) conditions { c.crosswind, c.crosswindGusts = 15.1, 15.1; return c },
			want: 0, wantWhy: "crosswind",
		},
		{
			name: "exactly at the ceiling limit",
			with: func(c conditions) conditions { c.cloudBaseFL = ptrInt(10); return c },
			want: 50,
		},
		{
			name: "just past the ceiling limit",
			with: func(c conditions) conditions { c.cloudBaseFL = ptrInt(9); return c },
			want: 0, wantWhy: "cloud base",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, penalties, _ := scoreVFR(tc.with(scoringConditions(t)))

			if got != tc.want {
				t.Errorf("probability = %d, want %d", got, tc.want)
			}
			if tc.wantWhy == "" {
				return
			}
			if len(penalties) != 1 || penalties[0].Factor != tc.wantWhy {
				t.Errorf("penalties = %+v, want just %q -- a no-go is the reason, not one of several",
					penalties, tc.wantWhy)
			}
		})
	}
}

// The calibration, in the terms the table is tuned in. The anchor and ramp tests above
// already walk every segment of every curve, so this only carries what they cannot show:
// one mid-band value per factor, the guard on precipitation probability, and what happens
// when penalties pile up.
func TestScoreVFR_Calibration(t *testing.T) {
	tests := []struct {
		name string
		with func(c conditions) conditions
		want int
	}{
		{
			name: "a clear midday hour scores full marks",
			with: func(c conditions) conditions { return c },
			want: 100,
		},
		{
			name: "a ceiling at FL25 costs 1",
			with: func(c conditions) conditions { c.cloudBaseFL = ptrInt(25); return c },
			want: 99,
		},
		{
			name: "12km of visibility costs 18",
			with: func(c conditions) conditions { c.visibilityKM = ptrFloat(12); return c },
			want: 82,
		},
		{
			name: "12kn of wind costs 4",
			with: func(c conditions) conditions { c.windSpeed = 12; return c },
			want: 96,
		},
		{
			// Seven times what the same speed costs as total wind: the two fire together
			// on every hour, and this is the one that decides whether you land.
			name: "12kn of crosswind costs 29",
			with: func(c conditions) conditions { c.crosswind, c.crosswindGusts = 12, 12; return c },
			want: 71,
		},
		{
			name: "a 12kn gust spread costs 29",
			with: func(c conditions) conditions { c.crosswind, c.crosswindGusts = 2, 14; return c },
			want: 71,
		},
		{
			// The hour that prompted the curve model: 3.02kn steady crosswind gusting
			// 7.27kn. The stepped ladder charged a flat 5 points for that 4.25kn spread
			// and scored an otherwise perfect afternoon at 95.
			name: "the gust spread that started this costs 1",
			with: func(c conditions) conditions { c.crosswind, c.crosswindGusts = 3.02, 7.27; return c },
			want: 99,
		},
		// Precipitation is one factor: what would fall, scaled by how likely it is to fall.
		// These are the corners of that interaction -- the same amount at two
		// probabilities, and two amounts at the same probability.
		{
			name: "2mm/h at 30 percent costs 9",
			with: func(c conditions) conditions {
				c.precipitation, c.precipitationProbability = 2, 30
				return c
			},
			want: 91,
		},
		{
			// Two and a half times the cost of the same rain at 30%: the probability
			// swings hardest through the middle of its range, where the decision turns.
			name: "2mm/h at 70 percent costs 23",
			with: func(c conditions) conditions {
				c.precipitation, c.precipitationProbability = 2, 70
				return c
			},
			want: 77,
		},
		{
			// The limits are where this airfield's rain lives: 1mm/h is the 90th
			// percentile of its wet hours, and at four fifths certainty it is a real cost
			// rather than the rounding error the old 8mm/h difficult limit made of it.
			name: "1mm/h at 80 percent costs 15",
			with: func(c conditions) conditions {
				c.precipitation, c.precipitationProbability = 1, 80
				return c
			},
			want: 85,
		},
		{
			// Past the last anchor the cost stops rising: precipitation has no wall,
			// because scaling one by a probability would decide by side effect whether an
			// unlikely deluge ends the hour. Certain heavy rain costs half the score and
			// leaves the rest to the factors that come with it.
			name: "15mm/h at 100 percent costs 50, not the whole hour",
			with: func(c conditions) conditions {
				c.precipitation, c.precipitationProbability = 15, 100
				return c
			},
			want: 50,
		},
		{
			name: "32C costs 15",
			with: func(c conditions) conditions { c.temperature = 32; return c },
			want: 85,
		},
		{
			// Weight 0.5 on an ordinal with one band: twilight is a real cost, but not the
			// one a critical crosswind carries.
			name: "an hour inside civil twilight costs 25",
			with: func(c conditions) conditions { return c.at(t, "2026-08-03T03:00") },
			want: 75,
		},
		{
			// Nothing here is past a wall; the hour is lost to the sum of them, and the
			// score clamps rather than going negative.
			name: "penalties accumulate and clamp at zero",
			with: func(c conditions) conditions {
				c.cloudBaseFL = ptrInt(13)
				c.windSpeed, c.crosswind, c.crosswindGusts = 25, 9, 9
				c.visibilityKM = ptrFloat(8)
				return c
			},
			want: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, _ := scoreVFR(tc.with(scoringConditions(t)))

			if got != tc.want {
				t.Errorf("probability = %d, want %d", got, tc.want)
			}
		})
	}
}

// One factor past its limit ends the hour, whatever the rest of the weather is doing.
//
// Each case has to leave every factor *earlier* in the table clear of its own limit, since
// the first no-go short-circuits the rest of the scoring.
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
			with:   func(c conditions) conditions { c.crosswind, c.crosswindGusts = 20, 20; return c },
			factor: "crosswind",
		},
		{
			// A wide spread off a light steady crosswind: the crosswind factor sees
			// nothing, and this one carries the hour on its own.
			name:   "a gust spread past the limit",
			with:   func(c conditions) conditions { c.crosswind, c.crosswindGusts = 2, 32; return c },
			factor: "crosswind gust spread",
		},
		{
			name:   "heat past the limit",
			with:   func(c conditions) conditions { c.temperature = 40; return c },
			factor: "temperature",
		},
		{
			name:   "before civil twilight",
			with:   func(c conditions) conditions { return c.at(t, "2026-08-03T01:00") },
			factor: "daylight",
		},
		{
			name:   "after civil twilight",
			with:   func(c conditions) conditions { return c.at(t, "2026-08-03T21:00") },
			factor: "daylight",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prob, penalties, _ := scoreVFR(tc.with(scoringConditions(t)))

			if prob != 0 {
				t.Errorf("probability = %d, want 0", prob)
			}
			// The reason has to survive to the frontend, or a 0 is indistinguishable
			// from an hour that merely accumulated a lot of small penalties.
			if len(penalties) != 1 {
				t.Fatalf("penalties = %+v, want exactly the one that ended the hour", penalties)
			}
			if penalties[0].Factor != tc.factor {
				t.Errorf("penalty factor = %q, want %q", penalties[0].Factor, tc.factor)
			}
			if penalties[0].Severity != noGo.String() {
				t.Errorf("penalty severity = %q, want %q", penalties[0].Severity, noGo.String())
			}
		})
	}
}

// Open-Meteo drops visibility beyond the ICON-EU horizon, which is the tail of every
// forecast. The regression this guards: nil visibility used to force the score to -1,
// discarding every other factor for the ~41 tail hours.
func TestScoreVFR_UnknownVisibilityStillScores(t *testing.T) {
	c := scoringConditions(t)
	c.visibilityKM = nil
	c.cloudBaseFL = ptrInt(25)

	prob, _, known := scoreVFR(c)

	if known {
		t.Error("visibilityKnown = true, want false when visibility is nil")
	}
	if prob != 99 {
		t.Errorf("probability = %d, want 99 (the cloud base penalty must still apply)", prob)
	}
}

func TestScoreVFR_KnownVisibility(t *testing.T) {
	prob, penalties, known := scoreVFR(scoringConditions(t))

	if !known {
		t.Error("visibilityKnown = false, want true when visibility is present")
	}
	if prob != 100 {
		t.Errorf("probability = %d, want 100", prob)
	}
	if len(penalties) != 0 {
		t.Errorf("penalties = %+v, want none on a clear hour", penalties)
	}
}

// A date resolveDaylight could not look up is absent from its map, so the hour arrives here
// with a nil window. Scoring it on the remaining factors would report a CAVOK afternoon and
// the middle of the night identically, so it must yield "no data" instead.
func TestScoreVFR_NoDaylightWindow(t *testing.T) {
	c := scoringConditions(t)
	c.daylight = nil

	prob, penalties, known := scoreVFR(c)

	if prob != -1 {
		t.Errorf("probability = %d, want -1 without a daylight window", prob)
	}
	if known {
		t.Error("visibilityKnown = true, want false when no score was computed")
	}
	if penalties != nil {
		t.Errorf("penalties = %+v, want none when no score was computed", penalties)
	}
}

// The breakdown is what answers "why is this hour 47%?" in the tooltip, so it has to lead
// with the factor that did the damage and account for the whole gap from 100.
func TestScoreVFR_BreakdownExplainsTheScore(t *testing.T) {
	c := scoringConditions(t)
	c.crosswind, c.crosswindGusts = 12, 12
	c.windSpeed = 12
	c.temperature = 32

	prob, penalties, _ := scoreVFR(c)

	if len(penalties) != 3 {
		t.Fatalf("penalties = %+v, want one per factor that cost something", penalties)
	}

	total := 0
	for i, p := range penalties {
		if p.Cost <= 0 {
			t.Errorf("penalties[%d] = %+v, want only factors that cost something", i, p)
		}
		if i > 0 && penalties[i-1].Cost < p.Cost {
			t.Errorf("penalties are not worst-first: %+v", penalties)
		}
		if p.Unit == "" {
			t.Errorf("penalties[%d] = %+v, want the unit the value is in", i, p)
		}
		total += p.Cost
	}

	if want := 100 - prob; total != want {
		t.Errorf("penalties total %d, want %d -- the breakdown must add up to the score", total, want)
	}
	if penalties[0].Factor != "crosswind" {
		t.Errorf("penalties[0].Factor = %q, want the dominant factor first", penalties[0].Factor)
	}
}

// A scaled penalty's cost alone does not identify the hour: near-certain drizzle and an
// unlikely downpour land on similar numbers by design. The breakdown has to carry what
// scaled it, or the tooltip cannot tell those two apart.
//
// The inputs are the wettest hour in the month of EDWN data this calibration was checked
// against, so this doubles as the one real-world case in the suite.
func TestScoreVFR_BreakdownCarriesTheScale(t *testing.T) {
	c := scoringConditions(t)
	c.precipitation, c.precipitationProbability = 3.2, 88

	prob, penalties, _ := scoreVFR(c)

	if prob != 59 {
		t.Errorf("probability = %d, want 59", prob)
	}

	if len(penalties) != 1 {
		t.Fatalf("penalties = %+v, want just the precipitation one", penalties)
	}
	got := penalties[0]

	if got.Scale == nil {
		t.Fatal("Scale = nil, want the probability that scaled this penalty")
	}
	if got.Scale.Name != "probability" || got.Scale.Value != 88 || got.Scale.Unit != "%" {
		t.Errorf("Scale = %+v, want probability 88%%", *got.Scale)
	}
	if got.Value != 3.2 {
		t.Errorf("Value = %v, want the amount in the factor's own unit, unscaled", got.Value)
	}
}

// Only a factor that declares a scale gets one, so an unscaled penalty must not sprout an
// empty object in the JSON.
func TestScoreVFR_UnscaledPenaltiesCarryNoScale(t *testing.T) {
	c := scoringConditions(t)
	c.windSpeed = 12

	_, penalties, _ := scoreVFR(c)

	if len(penalties) != 1 {
		t.Fatalf("penalties = %+v, want just the wind one", penalties)
	}
	if penalties[0].Scale != nil {
		t.Errorf("Scale = %+v, want nil on a factor with no scale", *penalties[0].Scale)
	}
}
