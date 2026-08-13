import assert from 'node:assert/strict';
import test from 'node:test';

// Pinned before the module is imported, so the daylight-saving cases below are about
// Europe/Berlin rather than about whichever machine runs the suite. Date reads TZ lazily,
// so this has to happen before the first Date is constructed.
process.env.TZ = 'Europe/Berlin';

const {
    toEpochIntervals, visibleBands, NIGHT_FILL, TWILIGHT_FILL, DAY_FILL,
    dayBands, subtractIntervals, setBands, bands,
    DAY_START_HOUR_UTC, DAY_END_HOUR_UTC,
} = await import('../frontend/js/bands.js');

const T = s => Date.parse(s);

test('toEpochIntervals converts the wire format to epoch milliseconds', () => {
    const got = toEpochIntervals([
        { from: '2026-08-09T19:49:03Z', to: '2026-08-10T03:26:24Z' },
    ]);

    assert.deepEqual(got, [{ from: T('2026-08-09T19:49:03Z'), to: T('2026-08-10T03:26:24Z') }]);
});

// The payload omits night_periods entirely when nothing resolved, and an interval whose end
// is not after its start would draw a zero- or negative-width rectangle.
test('toEpochIntervals drops what it cannot use', () => {
    assert.deepEqual(toEpochIntervals(undefined), []);
    assert.deepEqual(toEpochIntervals([]), []);
    assert.deepEqual(toEpochIntervals([{ from: 'nonsense', to: '2026-08-10T03:00:00Z' }]), []);
    assert.deepEqual(toEpochIntervals([{ from: '2026-08-10T03:00:00Z', to: '2026-08-10T03:00:00Z' }]), []);
    assert.deepEqual(toEpochIntervals([null]), []);
});

test('visibleBands keeps a band wholly inside the range', () => {
    const bands = [{ from: 20, to: 30 }];
    assert.deepEqual(visibleBands(bands, 0, 100), [{ from: 20, to: 30 }]);
});

test('visibleBands drops a band wholly outside the range', () => {
    const bands = [{ from: 20, to: 30 }];
    assert.deepEqual(visibleBands(bands, 40, 100), []);
    assert.deepEqual(visibleBands(bands, 0, 10), []);
});

// The reason this clips rather than letting the canvas do it: zoomed to six hours of a
// seven-day forecast, most bands are off-screen and the visible ones extend far past the
// plot area in both directions.
test('visibleBands clips a band straddling either edge', () => {
    assert.deepEqual(visibleBands([{ from: -500, to: 30 }], 0, 100), [{ from: 0, to: 30 }]);
    assert.deepEqual(visibleBands([{ from: 80, to: 9999 }], 0, 100), [{ from: 80, to: 100 }]);
    assert.deepEqual(visibleBands([{ from: -500, to: 9999 }], 0, 100), [{ from: 0, to: 100 }]);
});

// A band touching the edge exactly has no width on screen and is not worth a draw call.
test('visibleBands drops a band that only touches the edge', () => {
    assert.deepEqual(visibleBands([{ from: 100, to: 200 }], 0, 100), []);
    assert.deepEqual(visibleBands([{ from: -50, to: 0 }], 0, 100), []);
});

// Chart.js hands scale.min and scale.max straight in, and they are not always sane -- a
// chart mid-layout can report an empty or inverted range.
test('visibleBands copes with a degenerate range', () => {
    assert.deepEqual(visibleBands([{ from: 20, to: 30 }], 100, 0), []);
    assert.deepEqual(visibleBands([{ from: 20, to: 30 }], 50, 50), []);
    assert.deepEqual(visibleBands(undefined, 0, 100), []);
});

// Low alpha on purpose: wind barbs, cloud symbols and the gridlines all draw over these.
// The bands sit behind wind barbs, cloud symbols and four line series, and everything drawn
// over them has to stay legible -- but they were also once so faint nobody could see them.
// The ceiling is what keeps the data readable; the ordering is what makes the two greys read
// as one ramp into the dark rather than as two unrelated marks.
test('the fills stay light enough to draw over, and the greys ramp', () => {
    const alpha = fill => Number(fill.match(/([\d.]+)\)$/)[1]);

    for (const [name, fill] of [['night', NIGHT_FILL], ['twilight', TWILIGHT_FILL],
                                ['day', DAY_FILL]]) {
        assert.ok(alpha(fill) > 0 && alpha(fill) <= 0.25, `${name} alpha ${alpha(fill)}`);
    }
    assert.ok(alpha(TWILIGHT_FILL) < alpha(NIGHT_FILL),
        'twilight must be lighter than the night it leads into');
});

// One window per local day, each starting and ending on the configured local hour. Asserted
// through getHours() rather than absolute epochs, so the shape holds in any timezone.
test('dayBands gives one window per UTC day', () => {
    const got = dayBands(T('2026-08-09T00:00:00Z'), T('2026-08-12T00:00:00Z'));

    assert.ok(got.length >= 3 && got.length <= 4, `got ${got.length} windows`);
    for (const band of got) {
        assert.equal(new Date(band.from).getUTCHours(), DAY_START_HOUR_UTC);
        assert.equal(new Date(band.to).getUTCHours(), DAY_END_HOUR_UTC);
    }
});

test('dayBands clips the first and last window to the span', () => {
    // Span opens mid-window and closes mid-window.
    const from = T('2026-08-09T14:00:00Z');
    const to = T('2026-08-10T12:00:00Z');
    const got = dayBands(from, to);

    assert.equal(got.length, 2);
    assert.equal(got[0].from, from, 'first window starts where the span does, not at 0800Z');
    assert.equal(got[0].to, Date.UTC(2026, 7, 9, DAY_END_HOUR_UTC));
    assert.equal(got[1].from, Date.UTC(2026, 7, 10, DAY_START_HOUR_UTC));
    assert.equal(got[1].to, to, 'last window ends where the span does, not at 1800Z');
});

test('dayBands returns nothing for a span entirely at night', () => {
    const from = T('2026-08-09T21:00:00Z');
    const to = T('2026-08-10T07:00:00Z');

    assert.deepEqual(dayBands(from, to), []);
    assert.deepEqual(dayBands(NaN, to), []);
    assert.deepEqual(dayBands(to, from), []);
});

// The AIP publishes the window in UTC, so the window is fixed there and moves on the local
// clock instead: 0800Z is 09:00 in CET and 10:00 in CEST, which is when the field opens.
// Writing it as fixed local hours is the bug this replaced -- right all summer, an hour late
// all winter.
test('dayBands is fixed in UTC and moves on the local clock', () => {
    const winter = dayBands(T('2026-03-28T00:00:00Z'), T('2026-03-28T23:59:00Z'));
    const summer = dayBands(T('2026-03-30T00:00:00Z'), T('2026-03-30T23:59:00Z'));

    assert.equal(new Date(winter[0].from).toISOString(), '2026-03-28T08:00:00.000Z');
    assert.equal(new Date(winter[0].to).toISOString(), '2026-03-28T18:00:00.000Z');
    assert.equal(new Date(summer[0].from).toISOString(), '2026-03-30T08:00:00.000Z');
    assert.equal(new Date(summer[0].to).toISOString(), '2026-03-30T18:00:00.000Z');

    // Europe/Berlin, pinned at the top of this file.
    assert.equal(new Date(winter[0].from).getHours(), 9, 'CET: opens at 09:00 local');
    assert.equal(new Date(summer[0].from).getHours(), 10, 'CEST: opens at 10:00 local');
});

test('subtractIntervals splits a base interval around a hole inside it', () => {
    const got = subtractIntervals([{ from: 0, to: 100 }], [{ from: 40, to: 60 }]);
    assert.deepEqual(got, [{ from: 0, to: 40 }, { from: 60, to: 100 }]);
});

test('subtractIntervals truncates at either edge and removes what is covered', () => {
    assert.deepEqual(subtractIntervals([{ from: 0, to: 100 }], [{ from: -50, to: 30 }]),
        [{ from: 30, to: 100 }]);
    assert.deepEqual(subtractIntervals([{ from: 0, to: 100 }], [{ from: 70, to: 150 }]),
        [{ from: 0, to: 70 }]);
    assert.deepEqual(subtractIntervals([{ from: 0, to: 100 }], [{ from: -10, to: 110 }]), []);
});

test('subtractIntervals leaves the base alone when nothing overlaps', () => {
    const base = [{ from: 0, to: 100 }];
    assert.deepEqual(subtractIntervals(base, []), base);
    assert.deepEqual(subtractIntervals(base, [{ from: 200, to: 300 }]), base);
    assert.deepEqual(subtractIntervals(base, undefined), base);
});

// What "night wins" actually means: the two fills are translucent, so any overlap would
// blend into a third colour precisely on a winter afternoon, where a reader most needs to
// know which one applies.
test('the day and night bands never overlap', () => {
    setBands(
        [{ from: '2026-08-09T19:49:00Z', to: '2026-08-10T03:26:00Z' },
         { from: '2026-08-10T19:46:00Z', to: '2026-08-11T03:28:00Z' }],
        T('2026-08-09T00:00:00Z'), T('2026-08-11T12:00:00Z'));

    assert.ok(bands.day.length > 0, 'expected some daytime band');
    for (const day of bands.day) {
        for (const night of bands.night) {
            assert.ok(night.to <= day.from || night.from >= day.to,
                `day ${JSON.stringify(day)} overlaps night ${JSON.stringify(night)}`);
        }
    }
});

// The green window describes a personal flying habit at the home field, not a property of
// the airspace, so it must not appear over any other airfield's charts. Night is about the
// sun and stays either way.
test('the daytime band is suppressed away from the home airfield', () => {
    const night = [{ from: '2026-08-09T19:49:00Z', to: '2026-08-10T03:26:00Z' }];
    const from = T('2026-08-09T00:00:00Z');
    const to = T('2026-08-10T12:00:00Z');

    setBands(night, from, to, { daytime: false });
    assert.deepEqual(bands.day, [], 'no green away from home');
    assert.equal(bands.night.length, 1, 'night is unaffected');

    setBands(night, from, to, { daytime: true });
    assert.ok(bands.day.length > 0, 'green returns at home');
});

// Omitting the flag keeps the band, so a caller that has not been updated does not silently
// lose it.
test('the daytime band defaults to on', () => {
    setBands([], T('2026-08-09T00:00:00Z'), T('2026-08-10T00:00:00Z'));
    assert.ok(bands.day.length > 0);
});

// A degraded daylight lookup sends no night periods. The daytime band is computed from the
// span, not from them, so it must survive that.
test('the day band survives a payload with no night periods', () => {
    setBands(undefined, T('2026-08-09T00:00:00Z'), T('2026-08-10T00:00:00Z'));

    assert.deepEqual(bands.night, []);
    assert.ok(bands.day.length > 0, 'the daytime band must not vanish with the night data');
});

// Night > restricted > day. An activation running past sunset is the case that decides it:
// the part after dark belongs to night, and only the daylight part stays red.
test('the restricted band is clipped by night', () => {
    setBands(
        [{ from: '2026-08-11T19:45:00Z', to: '2026-08-12T03:30:00Z' }],
        T('2026-08-11T00:00:00Z'), T('2026-08-12T12:00:00Z'),
        { restricted: [{ from: '2026-08-11T17:00:00Z', to: '2026-08-11T22:00:00Z' }] });

    assert.equal(bands.restricted.length, 1);
    assert.equal(new Date(bands.restricted[0].to).toISOString(), '2026-08-11T19:45:00.000Z');
});

// The real EDWN case: an activation in the middle of the day leaves green either side of it
// rather than a blended band across it.
test('a midday activation splits the day band in two', () => {
    setBands([], T('2026-08-11T00:00:00Z'), T('2026-08-12T00:00:00Z'),
        { restricted: [{ from: '2026-08-11T11:00:00Z', to: '2026-08-11T13:00:00Z' }] });

    assert.equal(bands.day.length, 2, 'the green band must be interrupted, not overpainted');
    assert.equal(new Date(bands.day[0].to).toISOString(), '2026-08-11T11:00:00.000Z');
    assert.equal(new Date(bands.day[1].from).toISOString(), '2026-08-11T13:00:00.000Z');
});

// Three translucent fills overlapping would produce blended colours that mean nothing, in
// exactly the place a reader needs an unambiguous answer.
test('no two bands ever overlap', () => {
    setBands(
        [{ from: '2026-08-10T19:47:00Z', to: '2026-08-11T03:27:00Z' },
         { from: '2026-08-11T19:45:00Z', to: '2026-08-12T03:30:00Z' }],
        T('2026-08-10T00:00:00Z'), T('2026-08-12T12:00:00Z'),
        { restricted: [{ from: '2026-08-11T07:00:00Z', to: '2026-08-11T15:00:00Z' },
                       { from: '2026-08-11T18:00:00Z', to: '2026-08-12T06:00:00Z' }],
          twilight: [{ from: '2026-08-10T19:10:00Z', to: '2026-08-10T19:47:00Z' },
                     { from: '2026-08-11T19:08:00Z', to: '2026-08-11T19:45:00Z' }] });

    const all = [
        ...bands.night.map(b => ({ ...b, kind: 'night' })),
        ...bands.twilight.map(b => ({ ...b, kind: 'twilight' })),
        ...bands.restricted.map(b => ({ ...b, kind: 'restricted' })),
        ...bands.day.map(b => ({ ...b, kind: 'day' })),
    ];
    assert.ok(bands.twilight.length > 0 && bands.restricted.length > 0 && bands.day.length > 0,
        'expected all four kinds');

    for (const a of all) {
        for (const b of all) {
            if (a === b) continue;
            assert.ok(a.to <= b.from || a.from >= b.to,
                `${a.kind} ${JSON.stringify(a)} overlaps ${b.kind} ${JSON.stringify(b)}`);
        }
    }
});

// Away from home there is no red either: ED-R 37A surrounds EDWN and says nothing about
// anywhere else. The caller passes an empty list, and an unset option must mean the same.
// Twilight is the approach to night, so where they meet night keeps it -- otherwise the two
// greys would overlap into a third one at exactly the boundary between them.
test('the twilight band is clipped by night', () => {
    setBands(
        [{ from: '2026-08-11T19:45:00Z', to: '2026-08-12T03:30:00Z' }],
        T('2026-08-11T00:00:00Z'), T('2026-08-12T12:00:00Z'),
        { twilight: [{ from: '2026-08-11T19:10:00Z', to: '2026-08-11T19:49:00Z' }] });

    assert.equal(bands.twilight.length, 1);
    assert.equal(new Date(bands.twilight[0].to).toISOString(), '2026-08-11T19:45:00.000Z');
});

// The reason twilight is worth drawing at all, beyond the light: EDWN's winter hours close
// at "1800/SS", whichever comes first, and in December that is sunset. The bands are
// disjoint, so green ending where dusk begins *is* that cap -- no sunset in the payload and
// no parsing of the hours string.
test('the day band ends at sunset when dusk falls inside it', () => {
    setBands(
        [{ from: '2026-12-21T16:05:00Z', to: '2026-12-22T07:00:00Z' }],
        T('2026-12-21T00:00:00Z'), T('2026-12-22T00:00:00Z'),
        { twilight: [{ from: '2026-12-21T15:25:00Z', to: '2026-12-21T16:05:00Z' }] });

    assert.equal(bands.day.length, 1);
    assert.equal(new Date(bands.day[0].from).toISOString(), '2026-12-21T08:00:00.000Z');
    assert.equal(new Date(bands.day[0].to).toISOString(), '2026-12-21T15:25:00.000Z',
        'the green band must stop at sunset, not run to 1800Z');
});

// Summer is the other half of the same rule: sunset is past the published close, so the
// close is what ends the band.
test('the day band ends at 1800Z when sunset is later', () => {
    setBands([], T('2026-06-21T00:00:00Z'), T('2026-06-22T00:00:00Z'),
        { twilight: [{ from: '2026-06-21T19:55:00Z', to: '2026-06-21T20:45:00Z' }] });

    assert.equal(new Date(bands.day[0].to).toISOString(), '2026-06-21T18:00:00.000Z');
});

// Twilight is a property of the sky, not of whose home field it is: it stays when the two
// bands that describe a personal window do not.
test('twilight is drawn away from the home airfield', () => {
    setBands(
        [{ from: '2026-08-11T19:45:00Z', to: '2026-08-12T03:30:00Z' }],
        T('2026-08-11T00:00:00Z'), T('2026-08-12T00:00:00Z'),
        { daytime: false, twilight: [{ from: '2026-08-11T19:10:00Z', to: '2026-08-11T19:45:00Z' }] });

    assert.deepEqual(bands.day, []);
    assert.equal(bands.twilight.length, 1);
});

test('the twilight band defaults to empty', () => {
    setBands([], T('2026-08-11T00:00:00Z'), T('2026-08-12T00:00:00Z'));
    assert.deepEqual(bands.twilight, []);
});

test('the restricted band defaults to empty', () => {
    setBands([], T('2026-08-11T00:00:00Z'), T('2026-08-12T00:00:00Z'));
    assert.deepEqual(bands.restricted, []);

    setBands([], T('2026-08-11T00:00:00Z'), T('2026-08-12T00:00:00Z'), { restricted: [] });
    assert.deepEqual(bands.restricted, []);
});
