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
    twilight: [],
    restricted: [],
    day: [],
};

// The home airfield's published operating hours, in UTC, because that is the unit the AIP
// publishes in: EDWN is `SUM 0800-1800/SS+30; WIN 0800-1800/SS`, the same pair year round.
//
// UTC and not local, which is the bug this replaced. The window was written as 10:00-20:00
// local, which is what 0800-1800Z looks like in summer -- and an hour late all winter, green
// until 20:00 on a field that shut at 19:00.
//
// Hard coded rather than parsed out of `opening_hours`: that string carries sunset caps,
// lunch breaks and PPR, none of which is a rectangle. The one cap that does bite is the
// winter `/SS`, and the twilight band takes care of it -- the bands are disjoint, so green
// ending where dusk begins *is* "until sunset". This is one airfield's window, which is why
// isHomeAirport() gates it.
export const DAY_START_HOUR_UTC = 8;
export const DAY_END_HOUR_UTC = 18;

// setBands converts the payload's night intervals to epoch milliseconds once, at load,
// rather than parsing dates inside a draw hook that runs on every frame of a pan, and
// derives the daytime band over the same span.
//
// `daytime` is false for every airfield but the home one: the green window describes a
// personal flying habit, not a property of the airspace, so it has no business over another
// field's charts. The caller decides -- this module knows nothing about which airfield is
// selected, which is what keeps it testable outside a browser.
//
// Precedence is night > twilight > restricted > day, applied by subtraction rather than by
// draw order. Four translucent fills overlapping would produce blended colours that mean
// nothing, and the place they meet -- a winter afternoon with an activation running into
// dusk -- is exactly where a reader needs an unambiguous answer. Disjoint bands also make
// the draw order irrelevant.
//
// Twilight sits next to night because it is the same phenomenon one step weaker, so the
// greys read as one ramp into the dark rather than as two unrelated marks.
export function setBands(nightIntervals, from, to,
    { daytime = true, restricted = [], twilight = [] } = {}) {
    bands.night = toEpochIntervals(nightIntervals);
    bands.twilight = subtractIntervals(toEpochIntervals(twilight), bands.night);

    const dark = bands.night.concat(bands.twilight);
    bands.restricted = subtractIntervals(toEpochIntervals(restricted), dark);

    const covered = dark.concat(bands.restricted);
    bands.day = daytime ? subtractIntervals(dayBands(from, to), covered) : [];
}

// dayBands returns one DAY_START_HOUR_UTC..DAY_END_HOUR_UTC interval per UTC day that
// intersects [from, to], clipped to it.
//
// Built from UTC components, so the window is fixed where the AIP fixes it and the
// daylight-saving shift falls out on the display side instead: 0800Z draws under the 10:00
// tick in summer and the 09:00 tick in winter, which is where the airfield actually opens.
export function dayBands(from, to) {
    if (!Number.isFinite(from) || !Number.isFinite(to) || !(to > from)) {
        return [];
    }

    const out = [];
    // Start from UTC midnight of the first day, so the first window is not missed when the
    // span opens after 08:00Z.
    const cursor = new Date(from);
    cursor.setUTCHours(0, 0, 0, 0);

    for (; cursor.getTime() <= to; cursor.setUTCDate(cursor.getUTCDate() + 1)) {
        const y = cursor.getUTCFullYear();
        const m = cursor.getUTCMonth();
        const d = cursor.getUTCDate();

        const start = Math.max(Date.UTC(y, m, d, DAY_START_HOUR_UTC), from);
        const end = Math.min(Date.UTC(y, m, d, DAY_END_HOUR_UTC), to);
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
// Half the night's weight, same hue: dusk is the approach to it, not a separate condition.
export const TWILIGHT_FILL = 'rgba(100, 116, 139, 0.05)';
export const DAY_FILL = 'rgba(34, 197, 94, 0.12)';
export const RESTRICTED_FILL = 'rgba(239, 68, 68, 0.12)';
