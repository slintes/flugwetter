package server

import (
	"math"
	"testing"
	"time"
)

// scoringConditions is the baseline hour: midday, no cloud, calm, dry, mild, 40km
// visibility. It scores 100, so any case below isolates one factor by changing one field.
func scoringConditions(t *testing.T) conditions {
	t.Helper()

	ts, err := time.Parse(time.RFC3339, midday+":00Z")
	if err != nil {
		t.Fatalf("bad fixture time: %v", err)
	}
	return conditions{
		time:         ts,
		daylight:     testDayLight(t),
		visibilityKM: ptrFloat(40),
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
	}{
		{"a single anchor", []anchor{{perfect, 10, 0}}},
		{"no perfect anchor first", []anchor{{good, 10, 5}, {difficult, 20, 10}}},
		{"a perfect anchor that costs something", []anchor{{perfect, 10, 5}, {good, 20, 10}}},
		{"severities out of order", []anchor{{perfect, 10, 0}, {critical, 20, 30}, {good, 30, 40}}},
		{"a repeated severity", []anchor{{perfect, 10, 0}, {good, 20, 5}, {good, 30, 10}}},
		{"thresholds that turn around", []anchor{{perfect, 10, 0}, {good, 20, 5}, {difficult, 15, 10}}},
		{"a repeated threshold", []anchor{{perfect, 10, 0}, {good, 20, 5}, {difficult, 20, 10}}},
		{"costs that fall", []anchor{{perfect, 10, 0}, {good, 20, 15}, {difficult, 30, 10}}},
		{"an anchor after the no-go", []anchor{{perfect, 10, 0}, {noGo, 20, 0}, {critical, 30, 10}}},
		{"a no-go with an explicit cost", []anchor{{perfect, 10, 0}, {noGo, 20, 60}}},
		{"no room to ramp into the no-go", []anchor{{perfect, 10, 0}, {critical, 20, 100}, {noGo, 30, 0}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := factor{name: "test", curve: tc.curve}
			if err := f.validate(); err == nil {
				t.Error("validate() = nil, want an error")
			}
		})
	}
}

// Every anchor in the shipped table must cost exactly what it says. This and the ramp test
// below are driven off vfrLimits rather than a copy of it, so retuning the table does not
// mean rewriting them -- only a curve that no longer passes through its own anchors fails.
func TestFactorEvaluate_HitsItsAnchors(t *testing.T) {
	for _, f := range vfrLimits {
		for i, a := range f.curve {
			cost, sev, isNoGo := f.evaluate(a.at)

			if a.severity == noGo {
				if !isNoGo {
					t.Errorf("%s at %v %s: isNoGo = false, want true", f.name, a.at, f.unit)
				}
				continue
			}
			if isNoGo {
				t.Errorf("%s at %v %s: isNoGo = true, want false", f.name, a.at, f.unit)
			}
			if want := int(math.Round(a.cost)); cost != want {
				t.Errorf("%s at %v %s: cost = %d, want %d", f.name, a.at, f.unit, cost, want)
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
		costAt := func(i int) float64 {
			if f.curve[i].severity == noGo {
				return noGoCost
			}
			return f.curve[i].cost
		}

		for i := 0; i+1 < len(f.curve); i++ {
			lo, hi := f.curve[i], f.curve[i+1]
			mid := (lo.at + hi.at) / 2

			cost, sev, isNoGo := f.evaluate(mid)

			if isNoGo {
				t.Errorf("%s at %v %s: isNoGo = true short of the no-go threshold", f.name, mid, f.unit)
			}
			if want := int(math.Round((costAt(i) + costAt(i+1)) / 2)); cost != want {
				t.Errorf("%s at %v %s: cost = %d, want %d (midpoint of %v and %v)",
					f.name, mid, f.unit, cost, want, costAt(i), costAt(i+1))
			}
			// Approaching a no-go is still named for the band before it: a wall is not
			// a band, and "no-go" would claim the hour is already lost.
			want := hi.severity
			if want == noGo {
				want = lo.severity
			}
			if sev != want {
				t.Errorf("%s at %v %s: severity = %s, want %s", f.name, mid, f.unit, sev, want)
			}
		}
	}
}

// A factor without a no-go stops rising at its last anchor instead of extrapolating -- the
// ladder this replaced multiplied without bound, so 45C cost 51 points on its own.
//
// Driven off curves declared here rather than through scoreVFR: whether any factor in the
// shipped table lacks a no-go is a tuning decision, and this is testing the mechanism that
// would catch such a factor if one were added.
func TestFactorEvaluate_ClampsBeyondTheLastAnchor(t *testing.T) {
	tests := []struct {
		name  string
		curve []anchor
		value float64
	}{
		{
			name:  "worse as the value rises",
			curve: []anchor{{perfect, 10, 0}, {good, 20, 15}, {difficult, 30, 50}},
			value: 1000,
		},
		{
			name:  "worse as the value falls",
			curve: []anchor{{perfect, 30, 0}, {good, 20, 15}, {difficult, 10, 50}},
			value: -1000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := factor{name: "test", unit: "x", curve: tc.curve}

			cost, sev, isNoGo := f.evaluate(tc.value)

			if isNoGo {
				t.Error("isNoGo = true for a curve with no no-go anchor")
			}
			if cost != 50 {
				t.Errorf("cost = %d, want 50 (the last anchor's cost, not an extrapolation)", cost)
			}
			if sev != difficult {
				t.Errorf("severity = %s, want difficult", sev)
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
			name: "a ceiling at FL25 costs 3",
			with: func(c conditions) conditions { c.cloudBaseFL = ptrInt(25); return c },
			want: 97,
		},
		{
			name: "12km of visibility costs 16",
			with: func(c conditions) conditions { c.visibilityKM = ptrFloat(12); return c },
			want: 84,
		},
		{
			name: "12kn of wind costs 7",
			with: func(c conditions) conditions { c.windSpeed = 12; return c },
			want: 93,
		},
		{
			name: "12kn of crosswind costs 28",
			with: func(c conditions) conditions { c.crosswind, c.crosswindGusts = 12, 12; return c },
			want: 72,
		},
		{
			name: "a 12kn gust spread costs 7",
			with: func(c conditions) conditions { c.crosswind, c.crosswindGusts = 2, 14; return c },
			want: 93,
		},
		{
			// The hour that prompted the rework: 3.02kn steady crosswind gusting 7.27kn.
			// The stepped ladder charged a flat 5 points for that 4.24kn spread and scored
			// an otherwise perfect afternoon at 95.
			name: "the gust spread that started this is now free",
			with: func(c conditions) conditions { c.crosswind, c.crosswindGusts = 3.02, 7.27; return c },
			want: 100,
		},
		{
			name: "6mm/h of rain costs 13",
			with: func(c conditions) conditions { c.precipitation = 6; return c },
			want: 87,
		},
		{
			name: "the same rain at 80 percent costs a further 15",
			with: func(c conditions) conditions {
				c.precipitation, c.precipitationProbability = 6, 80
				return c
			},
			want: 72,
		},
		{
			// Below 2mm/h the probability is not worth a penalty of its own: a 90% chance
			// of a millimetre is not a reason to stay on the ground.
			name: "a 90 percent chance of 1.5mm/h costs only the amount",
			with: func(c conditions) conditions {
				c.precipitation, c.precipitationProbability = 1.5, 90
				return c
			},
			want: 97,
		},
		{
			name: "32C costs 18",
			with: func(c conditions) conditions { c.temperature = 32; return c },
			want: 82,
		},
		{
			name: "an hour inside civil twilight costs 30",
			with: func(c conditions) conditions { return c.at(t, "2026-08-03T03:00") },
			want: 70,
		},
		{
			// Nothing here is past its limit; the hour is lost to the sum of them. The
			// score clamps rather than going negative.
			name: "penalties accumulate and clamp at zero",
			with: func(c conditions) conditions {
				c.cloudBaseFL = ptrInt(13)
				c.windSpeed, c.crosswind, c.crosswindGusts = 28, 17, 25
				c.visibilityKM = ptrFloat(8)
				c.temperature, c.precipitation, c.precipitationProbability = 35, 4, 90
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
			factor: "crosswind gusts",
		},
		{
			name:   "rain past the limit",
			with:   func(c conditions) conditions { c.precipitation = 30; return c },
			factor: "precipitation",
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
	if prob != 97 {
		t.Errorf("probability = %d, want 97 (the cloud base penalty must still apply)", prob)
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
