import assert from 'node:assert/strict';
import test from 'node:test';

import { toEpochMs } from '../frontend/js/time.js';

// The whole point of this helper: Open-Meteo sends naive timestamps under timezone=GMT, so
// they have to be read as UTC. Parsing them as local time shifts the entire forecast by the
// viewer's offset -- silently, and by a whole hour or more.
test('toEpochMs reads naive timestamps as UTC', () => {
    assert.equal(toEpochMs('2026-08-04T12:00'), Date.UTC(2026, 7, 4, 12, 0));
    assert.equal(toEpochMs('2026-01-01T00:00'), Date.UTC(2026, 0, 1, 0, 0));
    assert.equal(toEpochMs('2026-12-31T23:00'), Date.UTC(2026, 11, 31, 23, 0));
});

// Guards the specific regression the helper exists to prevent.
test('toEpochMs does not depend on the local timezone', () => {
    const naive = '2026-08-04T12:00';
    const asLocal = new Date(naive).getTime();
    const asUTC = toEpochMs(naive);

    // In any zone that is not UTC these differ; in UTC they agree. Either way the UTC
    // reading is the correct one.
    assert.equal(asUTC, Date.UTC(2026, 7, 4, 12, 0));
    if (new Date().getTimezoneOffset() !== 0) {
        assert.notEqual(asUTC, asLocal);
    }
});

test('toEpochMs spacing is exactly one hour per step', () => {
    const hours = ['2026-08-04T10:00', '2026-08-04T11:00', '2026-08-04T12:00'].map(toEpochMs);
    assert.equal(hours[1] - hours[0], 3600000);
    assert.equal(hours[2] - hours[1], 3600000);
});

// Crossing a DST boundary must not change the spacing: the timestamps are UTC, and the
// charts plot them on a UTC-derived axis. Central European DST ended on 2026-10-25.
test('toEpochMs is unaffected by a DST transition', () => {
    const before = toEpochMs('2026-10-25T00:00');
    const after = toEpochMs('2026-10-25T01:00');
    assert.equal(after - before, 3600000);
});
