// The reload decision, and how a model run is written.
//
// The backend refetches the forecast when a model announces a new run rather than on a
// clock, so the frontend asks a ~130 byte endpoint whether anything changed instead of
// re-fetching 63KB of forecast every quarter hour.
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

// shouldReload decides whether to pull the full forecast again.
//
// When both run times are known the comparison is exact: reload if the backend is on a
// different run from the one on screen. When either is unknown -- no successful metadata
// poll, an older payload, a malformed response -- it falls back to the clock, which is what
// the frontend did before any of this existed. Falling back to reloading too often is a
// wasted request; failing to reload leaves stale weather on screen, so the fallback errs
// towards the former.
export function shouldReload(renderedRun, latestRun, ageMs, maxAgeMs) {
    if (!isKnownRun(renderedRun) || !isKnownRun(latestRun)) {
        return ageMs >= maxAgeMs;
    }
    return latestRun !== renderedRun;
}

// latestModelRun picks the run the UI labels the forecast with: the newest across the
// models behind `icon_seamless`.
//
// That is the ICON-D2 run, which covers roughly the first 46 hours. The tail of the chart
// comes from EU and global runs that are three and six hours older, so the label is the
// optimistic reading -- right for the part of the forecast anyone is looking at, and the
// full set is in the payload for anything that wants to be more careful.
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
