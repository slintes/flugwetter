package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// stubModelRunMeta points the metadata fetch at a function under the test's control and
// resets the tracker, which is package-level state shared between tests.
func stubModelRunMeta(t *testing.T, fn func(ctx context.Context, url string) (*modelRunMeta, error)) {
	t.Helper()

	original := fetchModelRunMetaFn
	fetchModelRunMetaFn = fn
	t.Cleanup(func() { fetchModelRunMetaFn = original })

	modelRuns.mutex.Lock()
	modelRuns.runs = nil
	modelRuns.consecutiveFails = 0
	modelRuns.lastSuccess = time.Time{}
	modelRuns.mutex.Unlock()
}

// metaAt builds a metadata document for a run initialized at the given unix time and
// available an hour later, which is roughly the real lag.
func metaAt(initialized int64) *modelRunMeta {
	return &modelRunMeta{
		LastRunInitialisationTime: initialized,
		LastRunAvailabilityTime:   initialized + 3600,
		UpdateIntervalSeconds:     10800,
	}
}

// The first poll has nothing to compare against, so every model reads as new -- which is
// what makes the caller refetch once at startup rather than serving whatever was cached.
func TestModelRuns_FirstPollReportsChange(t *testing.T) {
	stubModelRunMeta(t, func(context.Context, string) (*modelRunMeta, error) {
		return metaAt(1786082400), nil
	})

	if !modelRuns.poll(context.Background()) {
		t.Error("poll() = false on the first poll, want true")
	}

	runs, degraded := modelRuns.snapshot()
	if len(runs) != len(modelRunSources) {
		t.Fatalf("got %d runs, want %d", len(runs), len(modelRunSources))
	}
	if degraded {
		t.Error("degraded = true after a successful poll")
	}
}

// The common case, and the whole point: nothing has changed, so nothing is refetched.
func TestModelRuns_UnchangedPollReportsNoChange(t *testing.T) {
	stubModelRunMeta(t, func(context.Context, string) (*modelRunMeta, error) {
		return metaAt(1786082400), nil
	})

	modelRuns.poll(context.Background())

	if modelRuns.poll(context.Background()) {
		t.Error("poll() = true for an unchanged run, want false")
	}
}

func TestModelRuns_NewRunReportsChange(t *testing.T) {
	initialized := int64(1786082400)
	stubModelRunMeta(t, func(context.Context, string) (*modelRunMeta, error) {
		return metaAt(initialized), nil
	})

	modelRuns.poll(context.Background())

	// Three hours later, the next D2/EU cycle.
	initialized += 3 * 3600
	if !modelRuns.poll(context.Background()) {
		t.Error("poll() = false after the run advanced, want true")
	}

	if got, want := modelRuns.latestInitializedAt(), time.Unix(initialized, 0).UTC(); !got.Equal(want) {
		t.Errorf("latestInitializedAt() = %v, want %v", got, want)
	}
}

// Clocks and upstream bookkeeping are not guaranteed monotonic. A run time that moves
// backwards still means the data changed, and refetching is the safe response.
func TestModelRuns_RunMovingBackwardsStillCountsAsChange(t *testing.T) {
	initialized := int64(1786082400)
	stubModelRunMeta(t, func(context.Context, string) (*modelRunMeta, error) {
		return metaAt(initialized), nil
	})

	modelRuns.poll(context.Background())

	initialized -= 3 * 3600
	if !modelRuns.poll(context.Background()) {
		t.Error("poll() = false after the run moved backwards, want true")
	}
}

// A total failure must not read as "no new run for a while" -- that is indistinguishable
// from a working poller, and it is what the backstop TTL and the degraded flag exist for.
func TestModelRuns_TotalFailureReportsNoChangeAndDegrades(t *testing.T) {
	stubModelRunMeta(t, func(context.Context, string) (*modelRunMeta, error) {
		return nil, errors.New("metadata unreachable")
	})

	if modelRuns.poll(context.Background()) {
		t.Error("poll() = true when every model failed, want false")
	}
	if _, degraded := modelRuns.snapshot(); degraded {
		t.Error("degraded = true after a single failure — one blip is not a pattern")
	}

	modelRuns.poll(context.Background())
	if _, degraded := modelRuns.snapshot(); !degraded {
		t.Error("degraded = false after two consecutive failures, want true")
	}
}

func TestModelRuns_SuccessClearsDegraded(t *testing.T) {
	fail := true
	stubModelRunMeta(t, func(context.Context, string) (*modelRunMeta, error) {
		if fail {
			return nil, errors.New("metadata unreachable")
		}
		return metaAt(1786082400), nil
	})

	modelRuns.poll(context.Background())
	modelRuns.poll(context.Background())
	if _, degraded := modelRuns.snapshot(); !degraded {
		t.Fatal("degraded = false, want true before the recovery")
	}

	fail = false
	modelRuns.poll(context.Background())

	if _, degraded := modelRuns.snapshot(); degraded {
		t.Error("degraded = true after a successful poll, want false")
	}
}

// One model being unreachable is not a reason to lose its last known run, nor to ignore the
// models that did answer.
func TestModelRuns_PartialFailureKeepsTheLastKnownRun(t *testing.T) {
	initialized := int64(1786082400)
	failing := ""
	stubModelRunMeta(t, func(_ context.Context, url string) (*modelRunMeta, error) {
		if failing != "" && url == failing {
			return nil, errors.New("metadata unreachable")
		}
		return metaAt(initialized), nil
	})

	modelRuns.poll(context.Background())

	failing = modelRunSources[1].url
	initialized += 3 * 3600
	if !modelRuns.poll(context.Background()) {
		t.Error("poll() = false, want true — the models that answered had a new run")
	}

	runs, degraded := modelRuns.snapshot()
	if degraded {
		t.Error("degraded = true when only one model failed")
	}
	if len(runs) != len(modelRunSources) {
		t.Fatalf("got %d runs, want %d — a failing model must keep its last known run", len(runs), len(modelRunSources))
	}
}

// A document that parses but carries no run times is a shape change, not a run at the epoch.
func TestFetchModelRunMeta_RejectsAMetaWithoutRunTimes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chunk_time_length":121,"update_interval_seconds":10800}`))
	}))
	t.Cleanup(srv.Close)

	if _, err := fetchModelRunMeta(context.Background(), srv.URL); err == nil {
		t.Error("fetchModelRunMeta() = nil error for a document with no run times")
	}
}

func TestFetchModelRunMeta_DecodesTheRealShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Trimmed from a real dwd_icon_d2 response.
		_, _ = w.Write([]byte(`{"chunk_time_length":121,"data_end_time":1786258800,
			"last_run_availability_time":1786087494,"last_run_initialisation_time":1786082400,
			"last_run_modification_time":1786087405,"temporal_resolution_seconds":3600,
			"update_interval_seconds":10800}`))
	}))
	t.Cleanup(srv.Close)

	meta, err := fetchModelRunMeta(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.LastRunInitialisationTime != 1786082400 {
		t.Errorf("LastRunInitialisationTime = %d, want 1786082400", meta.LastRunInitialisationTime)
	}
	if meta.LastRunAvailabilityTime != 1786087494 {
		t.Errorf("LastRunAvailabilityTime = %d, want 1786087494", meta.LastRunAvailabilityTime)
	}
}

func TestGetStatus_ReportsTheLatestRun(t *testing.T) {
	withTestAirports(t)
	initialized := int64(1786082400)
	stubModelRunMeta(t, func(_ context.Context, url string) (*modelRunMeta, error) {
		// D2 is polled first and is the newest; the others are older cycles.
		switch url {
		case modelRunSources[0].url:
			return metaAt(initialized), nil
		case modelRunSources[1].url:
			return metaAt(initialized - 3*3600), nil
		default:
			return metaAt(initialized - 6*3600), nil
		}
	})
	modelRuns.poll(context.Background())

	rec := httptest.NewRecorder()
	getStatus(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	// Polled every few minutes; a cached answer would defeat the point of asking.
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var got StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if want := time.Unix(initialized, 0).UTC(); !got.LatestInitializedAt.Equal(want) {
		t.Errorf("LatestInitializedAt = %v, want the newest across the models (%v)", got.LatestInitializedAt, want)
	}
	if got.ModelRunsDegraded {
		t.Error("ModelRunsDegraded = true after a successful poll")
	}
}

// A snapshot ends up stamped into a cached payload that is shared, unlocked, by every
// goroutine serving it. If it shared storage with the tracker, the next poll could rewrite
// the run times inside a forecast that had already been served -- so a later run must not
// be able to reach through an earlier snapshot.
func TestModelRuns_SnapshotDoesNotShareStorageWithTheTracker(t *testing.T) {
	initialized := int64(1786082400)
	stubModelRunMeta(t, func(context.Context, string) (*modelRunMeta, error) {
		return metaAt(initialized), nil
	})

	modelRuns.poll(context.Background())

	// The aliasing check has to happen with no poll in between. A poll replaces the whole
	// slice, which orphans the array an earlier snapshot points at -- so writing through a
	// snapshot taken before one proves nothing either way.
	snap, _ := modelRuns.snapshot()
	if len(snap) == 0 {
		t.Fatal("snapshot is empty")
	}
	sentinel := time.Unix(1, 0).UTC()
	snap[0].InitializedAt = sentinel

	runs, _ := modelRuns.snapshot()
	if runs[0].InitializedAt.Equal(sentinel) {
		t.Error("writing to a snapshot reached the tracker: the two share storage")
	}

	// The other direction, which guards the poll path specifically: an earlier snapshot
	// must survive the tracker moving on. This is what a truncate-and-append in poll()
	// would break.
	before, _ := modelRuns.snapshot()
	was := before[0].InitializedAt

	initialized += 3 * 3600
	modelRuns.poll(context.Background())

	if !before[0].InitializedAt.Equal(was) {
		t.Errorf("an earlier snapshot changed from %v to %v when the tracker advanced",
			was, before[0].InitializedAt)
	}
}

// Model runs are global, so one arriving makes every airport's entry stale at the same
// instant -- invalidating only the airport being viewed would leave the other twelve
// serving the previous run for up to an hour.
func TestWeatherCache_InvalidateAllDropsEveryAirport(t *testing.T) {
	withTestAirports(t)

	cache.mutex.Lock()
	cache.entries["EDWN"] = &cacheEntry{data: &ProcessedWeatherData{}, timestamp: time.Now()}
	cache.entries["EDWG"] = &cacheEntry{data: &ProcessedWeatherData{}, timestamp: time.Now()}
	cache.mutex.Unlock()

	cache.invalidateAll()

	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	if len(cache.entries) != 0 {
		t.Errorf("%d entries survived invalidateAll, want 0", len(cache.entries))
	}
}

// cacheDuration is no longer the schedule -- a new model run is -- but it still has to stop
// an entry living forever when run detection is unavailable. Without this the poller
// failing silently would freeze the forecast rather than slowing it down.
func TestGetWeatherData_BackstopRefetchesBeyondTheTTL(t *testing.T) {
	withTestAirports(t)

	var fetches int
	stubFetchWeather(t, func(context.Context, Airport) (*ProcessedWeatherData, error) {
		fetches++
		return &ProcessedWeatherData{GeneratedAt: time.Now()}, nil
	})

	cache.mutex.Lock()
	stale := time.Now().Add(-2 * cacheDuration)
	cache.entries[testAirport.Identifier] = &cacheEntry{
		data:      &ProcessedWeatherData{GeneratedAt: stale},
		timestamp: stale,
	}
	cache.mutex.Unlock()

	if _, err := GetWeatherData(context.Background(), testAirport); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetches != 1 {
		t.Errorf("fetches = %d, want 1 — an entry past the backstop must be refetched", fetches)
	}

	// And the freshly stored entry is inside the window again, so the next call is served
	// from cache rather than refetching on every request.
	if _, err := GetWeatherData(context.Background(), testAirport); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetches != 1 {
		t.Errorf("fetches = %d, want 1 — a fresh entry must not be refetched", fetches)
	}
}

// The payload carries the runs it was built from, so the frontend can label the forecast
// without a second request.
func TestProcessWeatherData_StampsTheModelRuns(t *testing.T) {
	stubDayLight(t)
	initialized := int64(1786082400)
	stubModelRunMeta(t, func(context.Context, string) (*modelRunMeta, error) {
		return metaAt(initialized), nil
	})
	modelRuns.poll(context.Background())

	got := processWeatherData(context.Background(), hourlyFixture([]string{"2026-08-03T12:00"}), testAirport)

	if len(got.ModelRuns) != len(modelRunSources) {
		t.Fatalf("got %d model runs, want %d", len(got.ModelRuns), len(modelRunSources))
	}
	if got.ModelRuns[0].Model != modelRunSources[0].name {
		t.Errorf("ModelRuns[0].Model = %q, want %q", got.ModelRuns[0].Model, modelRunSources[0].name)
	}
	if want := time.Unix(initialized, 0).UTC(); !got.ModelRuns[0].InitializedAt.Equal(want) {
		t.Errorf("InitializedAt = %v, want %v", got.ModelRuns[0].InitializedAt, want)
	}
}
