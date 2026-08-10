import assert from 'node:assert/strict';
import test from 'node:test';

// Pinned before the module is imported, so the daylight-saving cases below are about
// Europe/Berlin rather than about whichever machine runs the suite. Date reads TZ lazily,
// so this has to happen before the first Date is constructed.
process.env.TZ = 'Europe/Berlin';

const {
    toEpochIntervals, visibleBands, NIGHT_FILL, DAY_FILL,
    dayBands, subtractIntervals, setBands, bands,
    DAY_START_HOUR, DAY_END_HOUR,
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
test('the fills stay faint', () => {
    for (const [name, fill] of [['night', NIGHT_FILL], ['day', DAY_FILL]]) {
        const alpha = Number(fill.match(/([\d.]+)\)$/)[1]);
        assert.ok(alpha > 0 && alpha <= 0.15, `${name} alpha ${alpha} should be subtle`);
    }
});

// One window per local day, each starting and ending on the configured local hour. Asserted
// through getHours() rather than absolute epochs, so the shape holds in any timezone.
test('dayBands gives one local-hours window per day', () => {
    const got = dayBands(T('2026-08-09T00:00:00Z'), T('2026-08-12T00:00:00Z'));

    assert.ok(got.length >= 3 && got.length <= 4, `got ${got.length} windows`);
    for (const band of got) {
        assert.equal(new Date(band.from).getHours(), DAY_START_HOUR);
        assert.equal(new Date(band.to).getHours(), DAY_END_HOUR);
    }
});

test('dayBands clips the first and last window to the span', () => {
    // Span opens mid-window and closes mid-window.
    const from = new Date(2026, 7, 9, 14, 0).getTime();
    const to = new Date(2026, 7, 10, 12, 0).getTime();
    const got = dayBands(from, to);

    assert.equal(got.length, 2);
    assert.equal(got[0].from, from, 'first window starts where the span does, not at 10:00');
    assert.equal(got[0].to, new Date(2026, 7, 9, DAY_END_HOUR).getTime());
    assert.equal(got[1].from, new Date(2026, 7, 10, DAY_START_HOUR).getTime());
    assert.equal(got[1].to, to, 'last window ends where the span does, not at 20:00');
});

test('dayBands returns nothing for a span entirely at night', () => {
    const from = new Date(2026, 7, 9, 21, 0).getTime();
    const to = new Date(2026, 7, 10, 7, 0).getTime();

    assert.deepEqual(dayBands(from, to), []);
    assert.deepEqual(dayBands(NaN, to), []);
    assert.deepEqual(dayBands(to, from), []);
});

// The reason the window is local rather than UTC: it has to sit under the axis label, and
// the axis is local. So in UTC terms it moves an hour at the clock change.
test('dayBands follows the daylight-saving change', () => {
    const winter = dayBands(new Date(2026, 2, 28, 0, 0).getTime(), new Date(2026, 2, 28, 23, 59).getTime());
    const summer = dayBands(new Date(2026, 2, 30, 0, 0).getTime(), new Date(2026, 2, 30, 23, 59).getTime());

    assert.equal(new Date(winter[0].from).toISOString(), '2026-03-28T09:00:00.000Z');
    assert.equal(new Date(winter[0].to).toISOString(), '2026-03-28T19:00:00.000Z');
    assert.equal(new Date(summer[0].from).toISOString(), '2026-03-30T08:00:00.000Z');
    assert.equal(new Date(summer[0].to).toISOString(), '2026-03-30T18:00:00.000Z');
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
                       { from: '2026-08-11T18:00:00Z', to: '2026-08-12T06:00:00Z' }] });

    const all = [
        ...bands.night.map(b => ({ ...b, kind: 'night' })),
        ...bands.restricted.map(b => ({ ...b, kind: 'restricted' })),
        ...bands.day.map(b => ({ ...b, kind: 'day' })),
    ];
    assert.ok(bands.restricted.length > 0 && bands.day.length > 0, 'expected all three kinds');

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
test('the restricted band defaults to empty', () => {
    setBands([], T('2026-08-11T00:00:00Z'), T('2026-08-12T00:00:00Z'));
    assert.deepEqual(bands.restricted, []);

    setBands([], T('2026-08-11T00:00:00Z'), T('2026-08-12T00:00:00Z'), { restricted: [] });
    assert.deepEqual(bands.restricted, []);
});
