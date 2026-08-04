package main

import "testing"

// The VFR ladder is the core domain logic and, by design, one long function of inline
// rules (see CLAUDE.md). That makes a table the only practical way to pin it: each case
// isolates a single rule by leaving every other input at its no-penalty value.
//
// All cases use midday, which is inside both civil twilight and sunrise..sunset, so
// daylight itself costs nothing unless the case says otherwise.
func TestCalculateVFRProbability_PenaltyLadder(t *testing.T) {
	tests := []struct {
		name       string
		cloudBase  *int
		wind       float64
		crosswind  float64
		gusts      float64
		visibility *float64
		temp       TemperaturePoint
		timeStr    string
		want       int
	}{
		{
			name: "clear day scores full marks",
			temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 100,
		},

		// Cloud base, in flight levels. Below FL10 is a hard no-go, covered in HardNoGo.
		{
			name:      "cloud base FL12 costs 50",
			cloudBase: ptrInt(12), temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 50,
		},
		{
			name:      "cloud base FL17 costs 25",
			cloudBase: ptrInt(17), temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 75,
		},
		{
			name:      "cloud base FL22 costs 10",
			cloudBase: ptrInt(22), temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 90,
		},
		{
			name:      "cloud base FL27 costs 5",
			cloudBase: ptrInt(27), temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 95,
		},
		{
			name:      "cloud base FL35 is free",
			cloudBase: ptrInt(35), temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 100,
		},

		// Total wind. Multipliers are deliberately mild because crosswind is penalised
		// separately and the two would otherwise compound.
		{
			name: "wind 16kn costs its excess over 10",
			wind: 16, temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 94, // sigWind 6, x1
		},
		{
			name: "wind 22kn costs double its excess",
			wind: 22, temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 76, // sigWind 12, x2
		},
		{
			name: "wind 30kn costs triple its excess",
			wind: 30, temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 40, // sigWind 20, x3
		},

		// Crosswind, against the best runway end. Note there is no hard no-go here, only
		// an escalating penalty -- see TestCalculateVFRProbability_CrosswindHasNoHardNoGo.
		{
			name:      "crosswind 8kn costs its excess over 5",
			crosswind: 8, gusts: 8, temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 97, // sigCrosswind 3, x1
		},
		{
			name:      "crosswind 12kn costs double its excess",
			crosswind: 12, gusts: 12, temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 86, // sigCrosswind 7, x2
		},
		{
			name:      "crosswind 18kn costs five times its excess",
			crosswind: 18, gusts: 18, temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 35, // sigCrosswind 13, x5
		},

		// Gust penalties key off the gust's margin over the steady crosswind, not its
		// absolute value: a steady 15kn is easier to fly than 5 gusting 15.
		{
			name:  "gust margin over 3kt costs 5",
			gusts: 5, temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 95,
		},
		{
			name:  "gust margin over 5kt costs 10",
			gusts: 9, temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 90,
		},
		{
			name:  "gust margin over 10kt costs 20",
			gusts: 12, temp: TemperaturePoint{Temperature: 18}, visibility: ptrFloat(40), timeStr: midday,
			want: 80,
		},

		// Visibility. Below 5km is a hard no-go, covered in HardNoGo.
		{
			name:       "visibility 8km costs 50",
			visibility: ptrFloat(8), temp: TemperaturePoint{Temperature: 18}, timeStr: midday,
			want: 50,
		},
		{
			name:       "visibility 15km costs 20",
			visibility: ptrFloat(15), temp: TemperaturePoint{Temperature: 18}, timeStr: midday,
			want: 80,
		},
		{
			name:       "visibility 25km costs 10",
			visibility: ptrFloat(25), temp: TemperaturePoint{Temperature: 18}, timeStr: midday,
			want: 90,
		},

		// Precipitation amount, and above 2mm the probability compounds it.
		{
			name:       "a trace of rain costs 5",
			visibility: ptrFloat(40), timeStr: midday,
			temp: TemperaturePoint{Temperature: 18, Precipitation: 0.4},
			want: 95,
		},
		{
			name:       "1mm costs 10",
			visibility: ptrFloat(40), timeStr: midday,
			temp: TemperaturePoint{Temperature: 18, Precipitation: 1.5},
			want: 90,
		},
		{
			name:       "3mm at 90 percent costs 15 plus 20",
			visibility: ptrFloat(40), timeStr: midday,
			temp: TemperaturePoint{Temperature: 18, Precipitation: 3, PrecipitationProbability: 90},
			want: 65,
		},
		{
			name:       "3mm at 50 percent costs 15 plus 10",
			visibility: ptrFloat(40), timeStr: midday,
			temp: TemperaturePoint{Temperature: 18, Precipitation: 3, PrecipitationProbability: 50},
			want: 75,
		},
		{
			name:       "10mm costs 25 plus the probability penalty",
			visibility: ptrFloat(40), timeStr: midday,
			temp: TemperaturePoint{Temperature: 18, Precipitation: 10, PrecipitationProbability: 70},
			want: 60,
		},

		// Heat, which matters for density altitude on a short grass strip.
		{
			name:       "32C costs 3 per degree over 28",
			visibility: ptrFloat(40), timeStr: midday,
			temp: TemperaturePoint{Temperature: 32},
			want: 88,
		},

		// Twilight: legal but not comfortable, so it is a penalty rather than a no-go.
		{
			name:       "before sunrise but inside civil twilight costs 30",
			visibility: ptrFloat(40), temp: TemperaturePoint{Temperature: 18}, timeStr: "2026-08-03T03:00",
			want: 70,
		},

		// Penalties accumulate and the result is clamped rather than going negative.
		{
			name:      "everything at once clamps at zero",
			cloudBase: ptrInt(12), wind: 30, crosswind: 18, gusts: 30,
			visibility: ptrFloat(8), timeStr: midday,
			temp: TemperaturePoint{Temperature: 35, Precipitation: 10, PrecipitationProbability: 90},
			want: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := calculateVFRProbability(
				testDayLight(t), tc.cloudBase, tc.wind, tc.crosswind, tc.gusts,
				tc.visibility, tc.temp, tc.timeStr)

			if got != tc.want {
				t.Errorf("probability = %d, want %d", got, tc.want)
			}
		})
	}
}

// Documents a mismatch between the code and CLAUDE.md rather than asserting an intention.
//
// CLAUDE.md states that calculateVFRProbability "returns 0 ... when crosswind >= 20kt".
// It does not: the function has exactly three hard-zero returns -- outside civil twilight,
// cloud base below FL10, and visibility below 5km. Crosswind only ever accumulates a
// penalty, so 20kn straight across the runway scores 15 rather than 0.
//
// This test pins what the code actually does. If the documented no-go is the intended
// behaviour, the rule needs adding to calculateVFRProbability and this test inverting; if
// not, CLAUDE.md needs correcting. Either way it should be a deliberate decision, so
// nothing here changes the score.
func TestCalculateVFRProbability_CrosswindHasNoHardNoGo(t *testing.T) {
	got, _ := calculateVFRProbability(
		testDayLight(t), nil, 20, 20, 20, ptrFloat(40),
		TemperaturePoint{Temperature: 18}, midday)

	// 100 - 75 (crosswind, sig 15 x5) - 10 (total wind, sig 10 x1) = 15.
	if got != 15 {
		t.Errorf("probability = %d, want 15 — change this only alongside a deliberate scoring change", got)
	}
}
