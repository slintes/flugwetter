// The shaded background bands, and the arithmetic that decides what is visible.
//
// The backend sends night as intervals rather than per-hour flags, so the edges land on
// civil twilight instead of snapping to the hour. It is the same boundary the VFR score
// zeroes an hour against, which is what keeps the grey and the zero from ever disagreeing.
//
// Deliberately free of Chart.js and of the DOM, so internal/web/jstest/ can run the
// clipping under node --test -- the same rule viewport.js, time.js and status.js follow.

// The bands currently on screen, filled by api.js on load and read by the backgroundBands
// plugin at draw time. A mutable module object rather than an exported array, for the same
// reason the `charts` registry is one: the plugin holds the reference from the start.
export const bands = {
    night: [],
};

// setNightBands converts the payload's intervals to epoch milliseconds once, at load,
// rather than parsing dates inside a draw hook that runs on every frame of a pan.
export function setNightBands(intervals) {
    bands.night = toEpochIntervals(intervals);
}

// toEpochIntervals maps the wire format to {from, to} in epoch ms, dropping anything
// unparseable or empty. RFC3339 with an explicit Z, so Date can be trusted with it --
// unlike the naive hourly timestamps, which is what toEpochMs exists for.
export function toEpochIntervals(intervals) {
    if (!Array.isArray(intervals)) {
        return [];
    }

    return intervals
        .map(interval => ({
            from: Date.parse(interval && interval.from),
            to: Date.parse(interval && interval.to),
        }))
        .filter(interval => Number.isFinite(interval.from)
            && Number.isFinite(interval.to)
            && interval.to > interval.from);
}

// visibleBands clips the intervals to the range currently on the x axis and drops the ones
// that fall outside it entirely.
//
// Clipping rather than letting the canvas do it: a seven-day forecast zoomed to six hours
// leaves most bands off-screen, and a rectangle spanning millions of pixels is a way to
// find out which browsers still have integer overflow bugs in their rasteriser.
export function visibleBands(intervals, min, max) {
    if (!Array.isArray(intervals) || !(max > min)) {
        return [];
    }

    const clipped = [];
    for (const interval of intervals) {
        const from = Math.max(interval.from, min);
        const to = Math.min(interval.to, max);
        if (to > from) {
            clipped.push({ from, to });
        }
    }
    return clipped;
}

// Kept next to the bands themselves: this is the only place the shade is decided, and it
// has to stay light. Wind barbs, cloud symbols and the gridlines all draw on top of it.
export const NIGHT_FILL = 'rgba(100, 116, 139, 0.10)';
