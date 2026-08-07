import assert from 'node:assert/strict';
import test from 'node:test';

import { shouldReload, isKnownRun, latestModelRun, formatModelRun } from '../frontend/js/status.js';

const MAX_AGE = 15 * 60 * 1000;
const RUN_06Z = '2026-08-07T06:00:00Z';
const RUN_09Z = '2026-08-07T09:00:00Z';

// The point of the whole mechanism: the common answer is "no", and it costs one small
// request rather than re-fetching the forecast.
test('an unchanged run does not reload', () => {
    assert.equal(shouldReload(RUN_06Z, RUN_06Z, 0, MAX_AGE), false);
    // Still no, however long the page has been open -- the forecast has not changed.
    assert.equal(shouldReload(RUN_06Z, RUN_06Z, 6 * 60 * 60 * 1000, MAX_AGE), false);
});

test('a new run reloads immediately', () => {
    assert.equal(shouldReload(RUN_06Z, RUN_09Z, 0, MAX_AGE), true);
});

// Go encodes a zero time.Time this way, and /api/status reports it before any metadata poll
// has succeeded. Read as a date it is the year 1, which would compare as "different from
// what is on screen" and reload on every single poll.
test('the Go zero time is not a run', () => {
    assert.equal(isKnownRun('0001-01-01T00:00:00Z'), false);
    assert.equal(isKnownRun(''), false);
    assert.equal(isKnownRun(null), false);
    assert.equal(isKnownRun(undefined), false);
    assert.equal(isKnownRun(RUN_06Z), true);
});

// With either side unknown there is nothing to compare, so the decision falls back to the
// clock -- which is what the frontend did before any of this existed.
test('an unknown run falls back to the clock', () => {
    for (const unknown of [null, undefined, '', '0001-01-01T00:00:00Z']) {
        assert.equal(shouldReload(RUN_06Z, unknown, 0, MAX_AGE), false,
            `fresh page should not reload (latest=${unknown})`);
        assert.equal(shouldReload(RUN_06Z, unknown, MAX_AGE, MAX_AGE), true,
            `stale page should reload (latest=${unknown})`);
        assert.equal(shouldReload(unknown, RUN_06Z, MAX_AGE, MAX_AGE), true,
            `stale page should reload (rendered=${unknown})`);
    }
});

// A failed poll passes null in, and getting stuck showing an ageing forecast is the worse
// failure of the two available.
test('a failed status poll still refreshes once the data is old', () => {
    assert.equal(shouldReload(RUN_06Z, null, MAX_AGE + 1, MAX_AGE), true);
});

test('latestModelRun picks the newest across the models', () => {
    const runs = [
        { model: 'icon_d2', initialized_at: RUN_06Z },
        { model: 'icon_eu', initialized_at: '2026-08-07T03:00:00Z' },
        { model: 'icon_global', initialized_at: '2026-08-07T00:00:00Z' },
    ];

    assert.equal(latestModelRun(runs), RUN_06Z);
});

// An older payload, or one served while run detection was down, carries no runs at all.
test('latestModelRun copes with a payload that has none', () => {
    assert.equal(latestModelRun(undefined), null);
    assert.equal(latestModelRun([]), null);
    assert.equal(latestModelRun([{ model: 'icon_d2', initialized_at: '0001-01-01T00:00:00Z' }]), null);
});

// UTC on purpose: model runs are named by their UTC hour everywhere they are discussed, and
// 06Z shown as 08:00 local is a forecast that looks two hours fresher than it is.
test('formatModelRun labels the run in UTC', () => {
    assert.equal(formatModelRun(RUN_06Z), 'Model run 06:00 UTC');
    assert.equal(formatModelRun('2026-08-07T00:00:00Z'), 'Model run 00:00 UTC');
});

test('formatModelRun renders nothing for an unknown run', () => {
    assert.equal(formatModelRun(null), '');
    assert.equal(formatModelRun('0001-01-01T00:00:00Z'), '');
    assert.equal(formatModelRun('not-a-date'), '');
});
