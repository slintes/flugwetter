import assert from 'node:assert/strict';
import test from 'node:test';

import { formatBreakdown } from '../frontend/js/vfr-breakdown.js';

test('an hour with nothing against it says so', () => {
    assert.deepEqual(formatBreakdown({ rating: 'perfect' }), ['nothing against it']);
    assert.deepEqual(formatBreakdown({ rating: 'perfect', factors: [] }), ['nothing against it']);
});

// Empty is "could not be scored", not "scored badly" -- the daylight window for that date
// was never resolved. Listing no factors for it would read as a clear hour.
test('an unscored hour is not reported as a clear one', () => {
    assert.deepEqual(formatBreakdown({ rating: '' }), ['no forecast for this hour']);
    assert.deepEqual(formatBreakdown(undefined), ['no forecast for this hour']);
});

test('a factor names itself, its value and its severity', () => {
    const lines = formatBreakdown({
        rating: 'good',
        factors: [{ factor: 'crosswind', value: 4.244104822, unit: 'kn', severity: 'good' }],
    });

    assert.deepEqual(lines, ['crosswind 4.2 kn — good']);
});

test('factors are listed in the order the backend sent them', () => {
    const lines = formatBreakdown({
        rating: 'difficult',
        factors: [
            { factor: 'cloud base', value: 22, unit: 'FL', severity: 'difficult' },
            { factor: 'temperature', value: 31.94, unit: 'C', severity: 'good' },
            { factor: 'wind', value: 16, unit: 'kn', severity: 'good' },
        ],
    });

    assert.deepEqual(lines, [
        'cloud base 22 FL — difficult',
        'temperature 31.9 C — good',
        'wind 16 kn — good',
    ]);
});

// A no-go reads the same as any other severity now -- there is no cost left that would
// invite adding it to the others.
test('a no-go factor reads like any other', () => {
    const lines = formatBreakdown({
        rating: 'no-go',
        factors: [{ factor: 'visibility', value: 3.2, unit: 'km', severity: 'no-go' }],
    });

    assert.deepEqual(lines, ['visibility 3.2 km — no-go']);
});

// Two factors can each be past their own wall in the same hour, and both are the reason --
// not whichever happened to come first in the table.
test('two no-go factors are both listed', () => {
    const lines = formatBreakdown({
        rating: 'no-go',
        factors: [
            { factor: 'wind', value: 32, unit: 'kn', severity: 'no-go' },
            { factor: 'temperature', value: 40, unit: 'C', severity: 'no-go' },
        ],
    });

    assert.deepEqual(lines, ['wind 32 kn — no-go', 'temperature 40 C — no-go']);
});

// Precipitation is judged on what would fall times how likely it is to fall, so the
// severity alone does not say which hour you are looking at: near-certain drizzle and an
// unlikely downpour can land on the same band.
test('a scaled factor shows what discounted it', () => {
    const lines = formatBreakdown({
        rating: 'difficult',
        factors: [{
            factor: 'precipitation', value: 3.2, unit: 'mm/h',
            severity: 'difficult',
            scale: { name: 'probability', value: 88, unit: '%' },
        }],
    });

    assert.deepEqual(lines, ['precipitation 3.2 mm/h at 88% — difficult']);
});

test('an unscaled factor is unchanged by the scale support', () => {
    const lines = formatBreakdown({
        rating: 'good',
        factors: [{ factor: 'wind', value: 12, unit: 'kn', severity: 'good' }],
    });

    assert.deepEqual(lines, ['wind 12 kn — good']);
});

// Daylight is an ordinal (day / twilight / night), so it arrives without a unit and its
// number would mean nothing to a reader.
test('a factor without a unit shows no value', () => {
    const lines = formatBreakdown({
        rating: 'no-go',
        factors: [{ factor: 'daylight', value: 2, unit: '', severity: 'no-go' }],
    });

    assert.deepEqual(lines, ['daylight — no-go']);
});

// gustWarning is judged on absolute gust readings, not anything in factors -- see
// gustWarning in internal/server/weather.go -- so it can appear on a clear hour, and its
// line is appended after the rating's own lines rather than replacing them.
test('a gust warning on an otherwise clear hour is appended, not swapped in', () => {
    const lines = formatBreakdown({
        rating: 'perfect',
        gustWarning: { windGusts: 24, crosswindGusts: 6 },
    });

    assert.deepEqual(lines, ['nothing against it', 'gusts 24 kn, crosswind gusts 6 kn — check before you fly']);
});

test('a gust warning is appended after the factor breakdown', () => {
    const lines = formatBreakdown({
        rating: 'difficult',
        factors: [{ factor: 'cloud base', value: 22, unit: 'FL', severity: 'difficult' }],
        gustWarning: { windGusts: 12.6, crosswindGusts: 11.04 },
    });

    assert.deepEqual(lines, [
        'cloud base 22 FL — difficult',
        'gusts 12.6 kn, crosswind gusts 11 kn — check before you fly',
    ]);
});

test('no gust warning adds nothing', () => {
    const lines = formatBreakdown({ rating: 'good', factors: [{ factor: 'wind', value: 8, unit: 'kn', severity: 'good' }] });

    assert.deepEqual(lines, ['wind 8 kn — good']);
});
