import assert from 'node:assert/strict';
import test from 'node:test';

import { toEpochIntervals, visibleBands, NIGHT_FILL } from '../frontend/js/bands.js';

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

// Low alpha on purpose: wind barbs, cloud symbols and the gridlines all draw over this.
test('the night fill stays faint', () => {
    const alpha = Number(NIGHT_FILL.match(/([\d.]+)\)$/)[1]);
    assert.ok(alpha > 0 && alpha <= 0.15, `alpha ${alpha} should be subtle`);
});
