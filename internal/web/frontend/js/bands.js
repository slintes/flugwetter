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
    day: [],
};

// The daytime window, in local hours. A fixed pair of numbers, the same for every airfield,
// and deliberately *not* anyone's opening hours -- those carry sunset caps, lunch breaks and
// PPR, which is why they are text under the picker rather than a rectangle here.
export const DAY_START_HOUR = 10;
export const DAY_END_HOUR = 20;

// setBands converts the payload's night intervals to epoch milliseconds once, at load,
// rather than parsing dates inside a draw hook that runs on every frame of a pan, and
// derives the daytime band over the same span.
//
// `daytime` is false for every airfield but the home one: the green window describes a
// personal flying habit, not a property of the airspace, so it has no business over another
// field's charts. The caller decides -- this module knows nothing about which airfield is
// selected, which is what keeps it testable outside a browser.
//
// Night is subtracted from day rather than drawn over it. Both fills are translucent, so
// overlapping them would blend into a third colour exactly where a reader most wants to
// know which one applies -- a winter afternoon. Disjoint bands also make the draw order
// irrelevant.
export function setBands(nightIntervals, from, to, { daytime = true } = {}) {
    bands.night = toEpochIntervals(nightIntervals);
    bands.day = daytime ? subtractIntervals(dayBands(from, to), bands.night) : [];
}

// dayBands returns one DAY_START_HOUR..DAY_END_HOUR interval per local day that intersects
// [from, to], clipped to it.
//
// Built from local wall-clock components, which is what makes the daylight-saving shift fall
// out rather than be computed: 10:00 local is 08:00Z in summer and 09:00Z in winter, and the
// band lands under the axis label either way because the axis is local too.
export function dayBands(from, to) {
    if (!Number.isFinite(from) || !Number.isFinite(to) || !(to > from)) {
        return [];
    }

    const out = [];
    // Start from local midnight of the first day, so the first window is not missed when the
    // span opens after 10:00.
    const cursor = new Date(from);
    cursor.setHours(0, 0, 0, 0);

    for (; cursor.getTime() <= to; cursor.setDate(cursor.getDate() + 1)) {
        const y = cursor.getFullYear();
        const m = cursor.getMonth();
        const d = cursor.getDate();

        const start = Math.max(new Date(y, m, d, DAY_START_HOUR).getTime(), from);
        const end = Math.min(new Date(y, m, d, DAY_END_HOUR).getTime(), to);
        if (end > start) {
            out.push({ from: start, to: end });
        }
    }
    return out;
}

// subtractIntervals removes every hole from every base interval, splitting one in two where
// a hole falls inside it.
//
// Both inputs are assumed sorted and non-overlapping among themselves, which is what the
// backend sends and what dayBands produces.
export function subtractIntervals(base, holes) {
    if (!Array.isArray(base)) {
        return [];
    }
    if (!Array.isArray(holes) || holes.length === 0) {
        return base.slice();
    }

    const out = [];
    for (const interval of base) {
        let pieces = [{ from: interval.from, to: interval.to }];
        for (const hole of holes) {
            const next = [];
            for (const piece of pieces) {
                if (hole.to <= piece.from || hole.from >= piece.to) {
                    next.push(piece);
                    continue;
                }
                if (hole.from > piece.from) {
                    next.push({ from: piece.from, to: hole.from });
                }
                if (hole.to < piece.to) {
                    next.push({ from: hole.to, to: piece.to });
                }
            }
            pieces = next;
        }
        out.push(...pieces);
    }
    return out;
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

// Kept next to the bands themselves: this is the only place the shades are decided, and
// they have to stay light. Wind barbs, cloud symbols and the gridlines all draw on top.
export const NIGHT_FILL = 'rgba(100, 116, 139, 0.10)';
export const DAY_FILL = 'rgba(34, 197, 94, 0.12)';
