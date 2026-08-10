package server

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// The fixture is a trimmed copy of a real briefing; see the comment at the top of it for
// what each area in it is there to exercise.
func aupFixture(t *testing.T) string {
	t.Helper()

	page, err := os.ReadFile("testdata/aup_briefing.html")
	if err != nil {
		t.Fatalf("failed to read the AUP fixture: %v", err)
	}
	return string(page)
}

// stubAUP points the fetch at a function under the test's control and resets the tracker,
// which is package-level state shared between tests.
func stubAUP(t *testing.T, fn func(ctx context.Context, from, to time.Time) (string, error)) {
	t.Helper()

	original := fetchAUPFn
	fetchAUPFn = fn
	t.Cleanup(func() { fetchAUPFn = original })

	restrictions.mutex.Lock()
	restrictions.areas = nil
	restrictions.fetchedAt = time.Time{}
	restrictions.consecutiveFails = 0
	restrictions.mutex.Unlock()
}

func findArea(areas []RestrictedArea, name string) *RestrictedArea {
	for i := range areas {
		if areas[i].Name == name {
			return &areas[i]
		}
	}
	return nil
}

func TestParseAUP_ReadsNamesWindowsAndLimits(t *testing.T) {
	areas := parseAUP(aupFixture(t))

	area := findArea(areas, "ED-R112A")
	if area == nil {
		t.Fatalf("ED-R112A missing from %d parsed areas", len(areas))
	}
	if len(area.Windows) != 3 {
		t.Fatalf("got %d windows for ED-R112A, want 3 — one per <tr class=\"validity\">", len(area.Windows))
	}

	want := RestrictionWindow{
		From:  time.Date(2026, 8, 11, 6, 0, 0, 0, time.UTC),
		To:    time.Date(2026, 8, 11, 22, 0, 0, 0, time.UTC),
		Lower: "GND",
		Upper: "A010",
	}
	if got := area.Windows[0]; got != want {
		t.Errorf("first window = %+v, want %+v", got, want)
	}
}

// The same area is listed once per FIR that borders it. Repeating it would draw the polygon
// twice and list every window twice in the popup.
func TestParseAUP_MergesAnAreaListedUnderSeveralFIRs(t *testing.T) {
	areas := parseAUP(aupFixture(t))

	var seen int
	for _, area := range areas {
		if area.Name == "ED-R37A" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("ED-R37A appears %d times, want 1", seen)
	}

	area := findArea(areas, "ED-R37A")
	if len(area.Windows) != 2 {
		t.Fatalf("got %d windows for ED-R37A, want 2 — the duplicate window must not repeat", len(area.Windows))
	}
	if got, want := area.Windows[1].Lower, "A010"; got != want {
		t.Errorf("second window lower limit = %q, want %q", got, want)
	}
}

// One unreadable timestamp costs that window, not the area it is in.
func TestParseAUP_DropsAMalformedWindowButKeepsTheArea(t *testing.T) {
	area := findArea(parseAUP(aupFixture(t)), "ED-R204")
	if area == nil {
		t.Fatal("ED-R204 was dropped for one bad timestamp")
	}
	if len(area.Windows) != 1 {
		t.Fatalf("got %d windows, want 1 — the unparseable one must be skipped", len(area.Windows))
	}
}

// An area with no activity is not activity of unknown extent: it must not reach the payload,
// where its presence alone would paint the map red.
func TestParseAUP_SkipsAnAreaWithNoWindows(t *testing.T) {
	if area := findArea(parseAUP(aupFixture(t)), "ED-R305"); area != nil {
		t.Errorf("ED-R305 has no windows but was kept: %+v", area)
	}
}

func TestParseAUP_IgnoresTablesThatAreNotAirspaces(t *testing.T) {
	for _, area := range parseAUP(aupFixture(t)) {
		if area.Name == "" {
			t.Error("an unnamed area was parsed — a non-airspace table leaked through")
		}
	}
}

// A page with nothing in it yields no areas rather than an error. It is also what a shape
// change upstream looks like, which is why poll() keeps the previous set on top of this.
func TestParseAUP_HandlesAPageWithNoAirspaces(t *testing.T) {
	if areas := parseAUP("<html><body>no plan published</body></html>"); len(areas) != 0 {
		t.Errorf("got %d areas from an empty page, want 0", len(areas))
	}
}

func TestParseAUPPolygon_ConvertsDMSToDecimalDegrees(t *testing.T) {
	// 52°26'07"N 007°20'10"E, the first point of ED-R37A.
	points := parseAUPPolygon("522607N0072010E-522607S0072010W")
	if len(points) != 2 {
		t.Fatalf("got %d points, want 2", len(points))
	}

	const wantLat, wantLon = 52 + 26.0/60 + 7.0/3600, 7 + 20.0/60 + 10.0/3600
	if math.Abs(points[0][0]-wantLat) > 1e-9 || math.Abs(points[0][1]-wantLon) > 1e-9 {
		t.Errorf("got %v, want [%v %v]", points[0], wantLat, wantLon)
	}
	// The hemisphere letters are the only thing carrying the sign.
	if math.Abs(points[1][0]+wantLat) > 1e-9 || math.Abs(points[1][1]+wantLon) > 1e-9 {
		t.Errorf("got %v, want [%v %v]", points[1], -wantLat, -wantLon)
	}
}

func TestParseAUPPolygon_SkipsUnreadablePoints(t *testing.T) {
	points := parseAUPPolygon("522607N0072010E-nonsense-522600N0072010E")
	if len(points) != 2 {
		t.Errorf("got %d points, want 2 — a bad point must not take the polygon with it", len(points))
	}
}

func TestRestrictions_PollReplacesTheSet(t *testing.T) {
	page := aupFixture(t)
	stubAUP(t, func(context.Context, time.Time, time.Time) (string, error) { return page, nil })

	restrictions.poll(context.Background())

	areas, fetchedAt, degraded := restrictions.snapshot()
	if len(areas) != 3 {
		t.Fatalf("got %d areas, want 3", len(areas))
	}
	if fetchedAt.IsZero() {
		t.Error("fetchedAt is zero after a successful poll")
	}
	if degraded {
		t.Error("degraded = true after a successful poll")
	}
	if got := restrictions.windowsFor("ED-R37A"); len(got) != 2 {
		t.Errorf("windowsFor(ED-R37A) returned %d windows, want 2", len(got))
	}
	if got := restrictions.windowsFor("ED-R999"); got != nil {
		t.Errorf("windowsFor() on an unknown area = %v, want nil", got)
	}
}

// Stale activity times are useful; an empty list reads as "nothing is active", which is a
// different and much more dangerous claim.
func TestRestrictions_FailedPollKeepsTheLastKnownSetAndDegrades(t *testing.T) {
	page := aupFixture(t)
	fail := false
	stubAUP(t, func(context.Context, time.Time, time.Time) (string, error) {
		if fail {
			return "", errors.New("AUP unreachable")
		}
		return page, nil
	})

	restrictions.poll(context.Background())
	before, fetchedAt, _ := restrictions.snapshot()

	fail = true
	restrictions.poll(context.Background())

	after, stillFetchedAt, degraded := restrictions.snapshot()
	if len(after) != len(before) {
		t.Fatalf("got %d areas after a failed poll, want the %d from before", len(after), len(before))
	}
	if !stillFetchedAt.Equal(fetchedAt) {
		t.Error("fetchedAt moved on a failed poll — it dates the data, not the attempt")
	}
	if degraded {
		t.Error("degraded = true after a single failure — one blip is not a pattern")
	}

	restrictions.poll(context.Background())
	if _, _, degraded := restrictions.snapshot(); !degraded {
		t.Error("degraded = false after two consecutive failures, want true")
	}

	fail = false
	restrictions.poll(context.Background())
	if _, _, degraded := restrictions.snapshot(); degraded {
		t.Error("degraded = true after a successful poll, want false")
	}
}

func TestGetRestrictions_ServesTheParsedPlan(t *testing.T) {
	page := aupFixture(t)
	stubAUP(t, func(context.Context, time.Time, time.Time) (string, error) { return page, nil })
	restrictions.poll(context.Background())

	rec := httptest.NewRecorder()
	getRestrictions(rec, httptest.NewRequest(http.MethodGet, "/api/restrictions", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got RestrictionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	area := findArea(got.Areas, "ED-R37A")
	if area == nil {
		t.Fatalf("ED-R37A missing from the payload (%d areas)", len(got.Areas))
	}
	if len(area.Polygon) == 0 {
		t.Error("the payload carries no polygon — the map has nothing to draw")
	}
	if got.Degraded {
		t.Error("Degraded = true after a successful poll")
	}
}

// The frontend iterates the array. A null would throw before it could draw anything.
func TestGetRestrictions_EncodesAnEmptyArrayBeforeTheFirstPoll(t *testing.T) {
	stubAUP(t, func(context.Context, time.Time, time.Time) (string, error) {
		return "", errors.New("not polled yet")
	})

	rec := httptest.NewRecorder()
	getRestrictions(rec, httptest.NewRequest(http.MethodGet, "/api/restrictions", nil))

	var got struct {
		Areas []RestrictedArea `json:"areas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.Areas == nil {
		t.Error("areas encoded as null, want []")
	}
}

// The bug modelRunTracker.snapshot already documents, with more slices to get wrong: a
// snapshot sharing the tracker's storage lets the next poll rewrite what a request is
// midway through encoding.
func TestRestrictions_SnapshotDoesNotShareStorage(t *testing.T) {
	page := aupFixture(t)
	stubAUP(t, func(context.Context, time.Time, time.Time) (string, error) { return page, nil })

	restrictions.poll(context.Background())
	areas, _, _ := restrictions.snapshot()
	if len(areas) == 0 || len(areas[0].Windows) == 0 || len(areas[0].Polygon) == 0 {
		t.Fatal("the fixture must yield at least one area with windows and a polygon")
	}

	// Mutate the copy the way an encoder never would, but a caller might. Only the slices
	// are worth checking: a struct field is copied by the assignment either way.
	areas[0].Windows[0].Upper = "clobbered"
	areas[0].Polygon[0] = [2]float64{0, 0}

	fresh, _, _ := restrictions.snapshot()
	if fresh[0].Windows[0].Upper == "clobbered" || fresh[0].Polygon[0] == ([2]float64{0, 0}) {
		t.Error("the tracker handed out its own storage — snapshot() must clone the slices, not the header")
	}
}
