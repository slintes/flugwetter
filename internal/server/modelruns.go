package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Model run tracking.
//
// ICON-D2 RUC runs hourly, ICON-D2 and ICON-EU every three hours, and ICON global every
// six. Polling metadata rather than the full forecast lets us react to each run cheaply.
//
// Open-Meteo publishes each model's run times at a small metadata document -- 598 bytes
// against 63.7 KB for the forecast itself. Polling that instead turns "refetch every 15
// minutes" into "refetch when there is something new", and gives the frontend a timestamp
// that means something: the forecast's own age rather than the age of our copy of it.
//
// These URLs are not part of Open-Meteo's documented v1 surface, so everything here is
// built to degrade: a failure keeps the last known runs, leaves the cache alone, and lets
// the backstop TTL in weather.go do what the clock used to do.

// Tracked models behind `models=icon_seamless`. RUC supplies the near term, D2 extends
// it, and EU and global cover the tail. Open-Meteo also uses other datasets; these run
// times are a useful summary, not complete provenance for each forecast value.
var modelRunSources = []struct {
	name string // as reported to the frontend
	url  string
}{
	{"icon_d2", "https://api.open-meteo.com/data/dwd_icon_d2/static/meta.json"},
	{"icon_eu", "https://api.open-meteo.com/data/dwd_icon_eu/static/meta.json"},
	{"icon_global", "https://api.open-meteo.com/data/dwd_icon/static/meta.json"},
	{rucModelName, "https://api.open-meteo.com/data/dwd_icon_d2_ruc_native/static/meta.json"},
}

const rucModelName = "icon_d2_ruc"

const (
	// Poll frequently enough to notice the hourly RUC run without fetching the forecast.
	modelRunPollInterval = 15 * time.Minute

	// One failed poll is a blip -- a transient DNS or TLS error would otherwise flap the
	// warning on and off. Two in a row is a pattern worth showing.
	modelRunFailuresBeforeDegraded = 2
)

// modelRunMeta is the shape of one metadata document. Only three fields are read; the
// document also carries grid geometry and chunking details that are none of our business.
//
// "Initialisation" is spelt Open-Meteo's way here and nowhere else. This struct has to
// match their JSON exactly, while everything we name ourselves follows the -ize spelling
// the rest of the codebase uses (initializeCharts). The seam is deliberate, and it is this
// struct.
type modelRunMeta struct {
	LastRunInitialisationTime int64 `json:"last_run_initialisation_time"`
	LastRunAvailabilityTime   int64 `json:"last_run_availability_time"`
	UpdateIntervalSeconds     int64 `json:"update_interval_seconds"`
}

// modelRunTracker holds what the last poll found. It is read by request handlers and
// written by the poll goroutine, hence the mutex.
type modelRunTracker struct {
	mutex sync.RWMutex

	runs                []ModelRun
	lastSuccess         time.Time
	consecutiveFails    int
	rucConsecutiveFails int
	rucLastPollOK       bool
}

var modelRuns = &modelRunTracker{}

// snapshot returns the known runs and whether run detection is currently degraded.
//
// Degraded means two consecutive total failures or RUC failures, not one transient error.
// A single RUC failure still makes its availability unknown to the browser, activating the
// fallback without immediately raising a visible warning.
// The runs are cloned, not returned directly. A slice is a header over shared storage, so
// handing out t.runs would let any later in-place write -- t.runs[i] = x, or an append that
// fits in the existing capacity -- rewrite the run times inside payloads that were already
// cached and served, from a goroutine holding no lock the readers respect. Today poll()
// happens to allocate a fresh array every time, which makes that safe by accident; three
// entries are far too few to keep that as an invariant somebody has to remember.
func (t *modelRunTracker) snapshot() (runs []ModelRun, degraded bool) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	return slices.Clone(t.runs), t.consecutiveFails >= modelRunFailuresBeforeDegraded ||
		t.rucConsecutiveFails >= modelRunFailuresBeforeDegraded
}

// latestInitializedAt returns the newest tracked initialization time. It is a summary,
// not the provenance of every hour or variable in the blended forecast.
func (t *modelRunTracker) latestInitializedAt() time.Time {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	var latest time.Time
	for _, run := range t.runs {
		if run.InitializedAt.After(latest) {
			latest = run.InitializedAt
		}
	}
	return latest
}

// rucAvailability returns zero if the latest RUC poll failed, even if an older run is
// retained for display. This lets the browser fall back to a timer rather than mistake
// an unreachable RUC source for an unchanged run indefinitely.
func (t *modelRunTracker) rucAvailability() time.Time {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	if t.rucLastPollOK {
		for _, run := range t.runs {
			if run.Model == rucModelName {
				return run.AvailableAt
			}
		}
	}
	return time.Time{}
}

// poll fetches every model's metadata and reports whether any of them announced a run that
// was not there before.
//
// A partial failure is still worth acting on: if D2 has a new run and EU is unreachable,
// the forecast has changed and should be refetched. Repeated RUC failures also degrade the
// tracker because the browser relies on that source for timely reloads.
func (t *modelRunTracker) poll(ctx context.Context) (changed bool) {
	fetched := make([]ModelRun, 0, len(modelRunSources))
	var failures int
	var rucPollOK bool

	for _, source := range modelRunSources {
		meta, err := fetchModelRunMetaFn(ctx, source.url)
		if err != nil {
			failures++
			slog.Warn("model run metadata unavailable", "model", source.name, "error", redactedError(err))
			continue
		}

		if source.name == rucModelName {
			rucPollOK = true
		}
		fetched = append(fetched, ModelRun{
			Model:         source.name,
			InitializedAt: time.Unix(meta.LastRunInitialisationTime, 0).UTC(),
			AvailableAt:   time.Unix(meta.LastRunAvailabilityTime, 0).UTC(),
		})
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.rucLastPollOK = rucPollOK
	if rucPollOK {
		t.rucConsecutiveFails = 0
	} else {
		t.rucConsecutiveFails++
	}

	if failures == len(modelRunSources) {
		t.consecutiveFails++
		return false
	}
	t.consecutiveFails = 0
	t.lastSuccess = time.Now()

	previous := make(map[string]time.Time, len(t.runs))
	for _, run := range t.runs {
		previous[run.Model] = run.AvailableAt
	}

	for _, run := range fetched {
		// Inequality rather than After: a run time that appears to move backwards means
		// something upstream changed, and refetching is the safe response to that.
		if was, seen := previous[run.Model]; !seen || !was.Equal(run.AvailableAt) {
			changed = true
			slog.Info("new model run",
				"model", run.Model,
				"initialized", run.InitializedAt.Format(time.RFC3339),
				"available", run.AvailableAt.Format(time.RFC3339))
		}
	}

	// Merge rather than replace, so a model that failed this round keeps its last known
	// run instead of vanishing from the UI.
	merged := make(map[string]ModelRun, len(t.runs))
	for _, run := range t.runs {
		merged[run.Model] = run
	}
	for _, run := range fetched {
		merged[run.Model] = run
	}

	// A fresh slice, never a reuse of the old backing array: snapshot() hands this out to
	// payloads that outlive the poll, and truncate-and-append would rewrite their contents
	// from under them.
	updated := make([]ModelRun, 0, len(modelRunSources))
	for _, source := range modelRunSources {
		if run, ok := merged[source.name]; ok {
			updated = append(updated, run)
		}
	}
	t.runs = updated

	return changed
}

// fetchModelRunMetaFn indirects the metadata call so tests can stub the network.
var fetchModelRunMetaFn = fetchModelRunMeta

func fetchModelRunMeta(ctx context.Context, url string) (*modelRunMeta, error) {
	body, err := getJSON(ctx, url)
	if err != nil {
		return nil, err
	}

	var meta modelRunMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("failed to decode model run metadata: %w", err)
	}

	// A document that parses but carries no run time is a shape change, not a run. Treated
	// as a failure so the backstop keeps refetching rather than the poller concluding that
	// nothing has happened since 1970.
	if meta.LastRunInitialisationTime == 0 || meta.LastRunAvailabilityTime == 0 {
		return nil, fmt.Errorf("model run metadata has no run times")
	}

	return &meta, nil
}

// watchModelRuns polls until ctx is cancelled, invalidating the weather cache whenever a
// model announces a new run.
//
// Invalidation is wholesale because model runs are global: a new run makes every airport's
// entry stale at the same instant. Only the default airport is re-warmed, for the same
// reason it is the only one warmed at startup -- refetching thirteen airfields nobody is
// looking at is exactly the waste this change exists to remove.
func watchModelRuns(ctx context.Context) {
	ticker := time.NewTicker(modelRunPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pollModelRunsOnce(ctx)
		}
	}
}

// pollModelRunsOnce is one tick's work, pulled out of watchModelRuns so a panic can be
// recovered without ending the poller for good. This is the only goroutine that would
// otherwise take the whole process down with it -- an http.Handler panic is recovered per
// request by loggingMiddleware, but nothing else catches one here.
func pollModelRunsOnce(ctx context.Context) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("panic polling model runs", "panic", recovered)
		}
	}()

	if modelRuns.poll(ctx) {
		cache.invalidateAll()
		slog.Info("model runs advanced, refreshing the default airport",
			"airport", defaultAirport.Identifier)
		_, _ = GetWeatherData(ctx, defaultAirport)
	}
}
