// The reload decision, and how a model run is written.
//
// The backend refetches when a tracked model changes. The frontend watches RUC's hourly
// availability time rather than refetching the full forecast on every poll.
//
// Deliberately free of the DOM and of Chart.js, so internal/web/jstest/ can run it under
// node --test -- the same rule viewport.js, barbs.js and time.js follow.

// Go encodes a zero time.Time as this, which is what /api/status reports before any
// metadata poll has succeeded. It is a value, not an absence, so it has to be recognised
// explicitly or "unknown" reads as "a run from the year 1".
const ZERO_TIME = '0001-01-01T00:00:00Z';

export function isKnownRun(run) {
    return typeof run === 'string' && run !== '' && run !== ZERO_TIME;
}

// shouldReload compares the RUC availability timestamp in the forecast to the latest one
// reported by status. Initialization alone cannot detect a reissued run. If either side
// is unknown, fall back to the clock rather than leave the forecast stale indefinitely.
export function shouldReload(renderedRun, latestRun, ageMs, maxAgeMs) {
    if (!isKnownRun(renderedRun) || !isKnownRun(latestRun)) {
        return ageMs >= maxAgeMs;
    }
    return latestRun !== renderedRun;
}

// shouldReloadPage decides whether the page itself is out of date -- a deployment happened
// and this tab is running frontend code the server no longer serves.
//
// The forecast reload above cannot cover this: a deployment does not change the model run,
// so a tab left open across one keeps its old JS indefinitely. That is harmless while the
// wire format holds and breaks the page the moment a field is renamed.
//
// Both commits must be known and differ. An unstamped build reports "unknown" on both sides,
// which compares equal and reloads nothing -- right, because a developer running `go run .`
// is the one case where reloading the page under them is pure nuisance.
export function shouldReloadPage(loadedCommit, servedCommit) {
    if (!loadedCommit || !servedCommit) {
        return false;
    }
    return loadedCommit !== servedCommit;
}

// rucAvailability extracts the availability time stamped into a forecast, not the newest
// initialization time: a different model can have the same or a newer initialization.
export function rucAvailability(runs) {
    if (!Array.isArray(runs)) return null;
    const ruc = runs.find(run => run && run.model === 'icon_d2_ruc');
    return ruc && isKnownRun(ruc.available_at) ? ruc.available_at : null;
}

// latestModelRun is the UI's summary of the newest tracked initialization. The blended
// forecast can contain other models and older runs, especially further into the future.
export function latestModelRun(runs) {
    if (!Array.isArray(runs) || runs.length === 0) {
        return null;
    }

    let latest = null;
    for (const run of runs) {
        if (!run || !isKnownRun(run.initialized_at)) {
            continue;
        }
        if (latest === null || run.initialized_at > latest) {
            latest = run.initialized_at;
        }
    }
    return latest;
}

// formatModelRun renders the label. UTC rather than local time on purpose: model runs are
// named by their UTC hour everywhere they are discussed, and 06Z read as 08:00 local is a
// forecast that looks two hours fresher than it is.
export function formatModelRun(run) {
    if (!isKnownRun(run)) {
        return '';
    }

    const at = new Date(run);
    if (Number.isNaN(at.getTime())) {
        return '';
    }

    const hh = String(at.getUTCHours()).padStart(2, '0');
    const mm = String(at.getUTCMinutes()).padStart(2, '0');
    return `Model run ${hh}:${mm} UTC`;
}
