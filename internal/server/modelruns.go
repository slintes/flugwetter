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
// Open-Meteo's metadata API is small compared with the forecast. It recommends waiting
// ten minutes after availability because its redundant servers synchronize asynchronously.
// A failed poll retains the last known runs and the cache backstop remains available.

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
	modelRunPollInterval = 5 * time.Minute
	modelRunSettleDelay  = 10 * time.Minute
	modelRunRetryDelay   = 5 * time.Minute

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

// settledRuns returns only metadata old enough to have propagated. While a newer run is
// pending, retain the previous settled run from the cached forecast where available.
// Capture this before a request: metadata observed during it cannot describe the result.
func (t *modelRunTracker) settledRuns(now time.Time) []ModelRun {
	runs, _ := t.snapshot()
	previous := make(map[string]ModelRun)
	if entry, ok := cachedEntry(defaultAirport.Identifier); ok {
		for _, run := range entry.data.ModelRuns {
			previous[run.Model] = run
		}
	}
	settled := make([]ModelRun, 0, len(runs))
	for _, run := range runs {
		if now.Before(run.AvailableAt.Add(modelRunSettleDelay)) {
			old, ok := previous[run.Model]
			if !ok || now.Before(old.AvailableAt.Add(modelRunSettleDelay)) {
				continue
			}
			run = old
		}
		settled = append(settled, run)
	}
	return settled
}

// rucAvailability reports the RUC timestamp in the successfully cached default forecast,
// not the latest observation. A failed poll remains unknown for the browser's fallback.
func (t *modelRunTracker) rucAvailability() time.Time {
	t.mutex.RLock()
	ok := t.rucLastPollOK
	t.mutex.RUnlock()
	if ok {
		if entry, found := cachedEntry(defaultAirport.Identifier); found {
			for _, run := range entry.data.ModelRuns {
				if run.Model == rucModelName {
					return run.AvailableAt
				}
			}
		}
	}
	return time.Time{}
}

// nextRefresh compares observed runs with the last successfully cached default forecast.
// Each pending change remains pending across unchanged polls and failed refreshes.
func (t *modelRunTracker) nextRefresh(now time.Time) (time.Time, bool) {
	runs, _ := t.snapshot()
	cached := make(map[string]time.Time)
	if entry, ok := cachedEntry(defaultAirport.Identifier); ok {
		for _, run := range entry.data.ModelRuns {
			cached[run.Model] = run.AvailableAt
		}
	}
	var next time.Time
	for _, run := range runs {
		if cached[run.Model].Equal(run.AvailableAt) {
			continue
		}
		ready := run.AvailableAt.Add(modelRunSettleDelay)
		if ready.Before(now) {
			ready = now
		}
		if next.IsZero() || ready.Before(next) {
			next = ready
		}
	}
	return next, !next.IsZero()
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

// watchModelRuns polls independently of the one-shot refresh timer. A failed refresh
// retains the old cache and is retried after modelRunRetryDelay, without a busy loop.
func watchModelRuns(ctx context.Context) {
	ticker := time.NewTicker(modelRunPollInterval)
	defer ticker.Stop()
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var retryAfter time.Time
	for {
		next, pending := modelRuns.nextRefresh(time.Now())
		if pending {
			if next.Before(retryAfter) {
				next = retryAfter
			}
			timer.Reset(time.Until(next))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if pending && !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			pollModelRunsOnce(ctx)
		case <-timer.C:
			if err := runScheduledRefresh(ctx); err != nil {
				slog.Warn("scheduled forecast refresh failed", "error", redactedError(err))
				retryAfter = time.Now().Add(modelRunRetryDelay)
			} else {
				retryAfter = time.Time{}
			}
		}
	}
}

// A panic in the warmer must not end the watcher. Treat it like a failed fetch so the
// cached forecast survives and the normal retry delay applies.
func runScheduledRefresh(ctx context.Context) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic refreshing forecast: %v", recovered)
		}
	}()
	return refreshDefaultForecast(ctx)
}

// Recover a bad poll without ending the watcher; the next tick will try again.
func pollModelRunsOnce(ctx context.Context) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("panic polling model runs", "panic", recovered)
		}
	}()
	modelRuns.poll(ctx)
}
