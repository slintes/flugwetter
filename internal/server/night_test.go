package server

import (
	"context"
	"testing"
	"time"
)

// dayWindow builds a daylight window for one date with civil twilight at the given hours.
func dayWindow(t *testing.T, date string, twilightBegin, twilightEnd string) *SunriseSunsetResponse {
	t.Helper()

	parse := func(hm string) time.Time {
		ts, err := time.Parse(time.RFC3339, date+"T"+hm+":00Z")
		if err != nil {
			t.Fatalf("bad fixture time %q: %v", hm, err)
		}
		return ts
	}

	resp := &SunriseSunsetResponse{Status: "OK"}
	resp.Parsed.CivilTwilightBegin = parse(twilightBegin)
	resp.Parsed.CivilTwilightEnd = parse(twilightEnd)
	resp.Parsed.Sunrise = parse(twilightBegin)
	resp.Parsed.Sunset = parse(twilightEnd)
	return resp
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad fixture time %q: %v", s, err)
	}
	return ts
}

// The ordinary case: three days of forecast produce the night it opens in, the two whole
// nights between the days, and the night it runs off the end of.
func TestNightIntervals_OneIntervalPerNight(t *testing.T) {
	daylight := map[string]*SunriseSunsetResponse{
		"2026-08-09": dayWindow(t, "2026-08-09", "03:24", "19:49"),
		"2026-08-10": dayWindow(t, "2026-08-10", "03:26", "19:46"),
		"2026-08-11": dayWindow(t, "2026-08-11", "03:28", "19:44"),
	}

	got := nightIntervals(daylight,
		mustTime(t, "2026-08-09T00:00:00Z"), mustTime(t, "2026-08-12T00:00:00Z"))

	want := []Interval{
		{From: mustTime(t, "2026-08-09T00:00:00Z"), To: mustTime(t, "2026-08-09T03:24:00Z")},
		{From: mustTime(t, "2026-08-09T19:49:00Z"), To: mustTime(t, "2026-08-10T03:26:00Z")},
		{From: mustTime(t, "2026-08-10T19:46:00Z"), To: mustTime(t, "2026-08-11T03:28:00Z")},
		{From: mustTime(t, "2026-08-11T19:44:00Z"), To: mustTime(t, "2026-08-12T00:00:00Z")},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d intervals, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].From.Equal(want[i].From) || !got[i].To.Equal(want[i].To) {
			t.Errorf("interval %d = %v..%v, want %v..%v",
				i, got[i].From, got[i].To, want[i].From, want[i].To)
		}
	}
}

// The first and last nights are cut off by the forecast window rather than by dawn or dusk,
// and must not extend past it -- a band running off the end of the data would shade an area
// the charts have nothing to show in.
func TestNightIntervals_ClampedToTheWindow(t *testing.T) {
	daylight := map[string]*SunriseSunsetResponse{
		"2026-08-09": dayWindow(t, "2026-08-09", "03:24", "19:49"),
	}

	// A window starting after dawn and ending before dusk contains no night at all.
	if got := nightIntervals(daylight,
		mustTime(t, "2026-08-09T08:00:00Z"), mustTime(t, "2026-08-09T17:00:00Z")); len(got) != 0 {
		t.Errorf("got %+v, want no intervals for a window entirely in daylight", got)
	}

	// One starting before dawn gets the opening night, clipped at both ends.
	got := nightIntervals(daylight,
		mustTime(t, "2026-08-09T02:00:00Z"), mustTime(t, "2026-08-09T21:00:00Z"))
	if len(got) != 2 {
		t.Fatalf("got %d intervals, want 2: %+v", len(got), got)
	}
	if !got[0].From.Equal(mustTime(t, "2026-08-09T02:00:00Z")) {
		t.Errorf("first interval starts %v, want the window start", got[0].From)
	}
	if !got[1].To.Equal(mustTime(t, "2026-08-09T21:00:00Z")) {
		t.Errorf("last interval ends %v, want the window end", got[1].To)
	}
}

// A date whose daylight lookup failed is absent from the map. Bridging the gap would mean
// claiming a dawn nobody knows: an unshaded night reads as missing data, a shaded day reads
// as a fact.
func TestNightIntervals_GapInTheDatesIsNotBridged(t *testing.T) {
	daylight := map[string]*SunriseSunsetResponse{
		"2026-08-09": dayWindow(t, "2026-08-09", "03:24", "19:49"),
		// 2026-08-10 failed to resolve.
		"2026-08-11": dayWindow(t, "2026-08-11", "03:28", "19:44"),
	}

	got := nightIntervals(daylight,
		mustTime(t, "2026-08-09T12:00:00Z"), mustTime(t, "2026-08-11T23:00:00Z"))

	for _, interval := range got {
		if interval.From.Before(mustTime(t, "2026-08-10T00:00:00Z")) &&
			interval.To.After(mustTime(t, "2026-08-10T12:00:00Z")) {
			t.Errorf("interval %v..%v spans the unresolved day", interval.From, interval.To)
		}
	}

	// Only the last night, which is bounded by a window that was resolved.
	if len(got) != 1 {
		t.Fatalf("got %d intervals, want 1: %+v", len(got), got)
	}
	if !got[0].From.Equal(mustTime(t, "2026-08-11T19:44:00Z")) {
		t.Errorf("interval starts %v, want the last resolved dusk", got[0].From)
	}
}

func TestNightIntervals_NoDaylightAtAll(t *testing.T) {
	if got := nightIntervals(nil,
		mustTime(t, "2026-08-09T00:00:00Z"), mustTime(t, "2026-08-10T00:00:00Z")); got != nil {
		t.Errorf("got %+v, want nil when no date could be resolved", got)
	}
}

// The window runs to the end of the last hour, not its start. A forecast whose final entry
// is 23:00 covers up to 24:00, and stopping at 23:00 left that hour unshaded on a night it
// plainly was one.
func TestForecastWindow_CoversTheWholeLastHour(t *testing.T) {
	from, to := forecastWindow([]string{
		"2026-08-09T00:00", "2026-08-09T01:00", "2026-08-09T23:00",
	})

	if want := mustTime(t, "2026-08-09T00:00:00Z"); !from.Equal(want) {
		t.Errorf("from = %v, want %v", from, want)
	}
	if want := mustTime(t, "2026-08-10T00:00:00Z"); !to.Equal(want) {
		t.Errorf("to = %v, want %v -- the last hour must be covered, not just started", to, want)
	}
}

// An unparseable timestamp must not drag the window back to the zero time, which would
// shade the entire chart.
func TestForecastWindow_SkipsUnparseableTimes(t *testing.T) {
	from, to := forecastWindow([]string{"not-a-time", "2026-08-09T10:00", "also-bad"})

	if want := mustTime(t, "2026-08-09T10:00:00Z"); !from.Equal(want) {
		t.Errorf("from = %v, want %v", from, want)
	}
	if want := mustTime(t, "2026-08-09T11:00:00Z"); !to.Equal(want) {
		t.Errorf("to = %v, want %v", to, want)
	}
}

// The assertion that keeps the band and the score from drifting apart: they are both
// derived from civil twilight, so every hour the score zeroes for daylight must fall inside
// a night band, and no hour that scores above zero may.
func TestProcessWeatherData_NightBandsAgreeWithTheScore(t *testing.T) {
	stubDayLight(t)

	var times []string
	for day := 3; day <= 4; day++ {
		for h := 0; h < 24; h++ {
			times = append(times, timeAt(day, h))
		}
	}

	got := processWeatherData(context.Background(), hourlyFixture(times), testAirport)

	inNight := func(ts time.Time) bool {
		for _, interval := range got.NightPeriods {
			if !ts.Before(interval.From) && ts.Before(interval.To) {
				return true
			}
		}
		return false
	}

	for _, point := range got.VfrData {
		ts, err := hourTime(point.Time)
		if err != nil {
			t.Fatalf("unparseable time in output: %q", point.Time)
		}

		night := false
		for _, penalty := range point.Penalties {
			if penalty.Factor == "daylight" && penalty.Severity == noGo.String() {
				night = true
			}
		}

		if night && !inNight(ts) {
			t.Errorf("%s scores 0 for daylight but is not inside a night band", point.Time)
		}
		if !night && point.Probability > 0 && inNight(ts) {
			t.Errorf("%s scores %d but is inside a night band", point.Time, point.Probability)
		}
	}
}

func timeAt(day, hour int) string {
	return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC).Format("2006-01-02T15:04")
}
