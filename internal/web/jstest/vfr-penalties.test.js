import assert from 'node:assert/strict';
import test from 'node:test';

import { formatPenalties } from '../frontend/js/vfr-penalties.js';

test('an hour with nothing against it says so', () => {
    assert.deepEqual(formatPenalties({ probability: 100 }), ['nothing against it']);
    assert.deepEqual(formatPenalties({ probability: 100, penalties: [] }), ['nothing against it']);
});

// -1 is "could not be scored", not "scored badly" -- the daylight window for that date was
// never resolved. Listing no penalties for it would read as a clear hour.
test('an unscored hour is not reported as a clear one', () => {
    assert.deepEqual(formatPenalties({ probability: -1 }), ['no forecast for this hour']);
    assert.deepEqual(formatPenalties(undefined), ['no forecast for this hour']);
});

test('a penalty names the factor, its value and what it cost', () => {
    const lines = formatPenalties({
        probability: 98,
        penalties: [{ factor: 'crosswind gusts', value: 4.244104822, unit: 'kn', severity: 'good', cost: 2 }],
    });

    assert.deepEqual(lines, ['crosswind gusts 4.2 kn — good, −2']);
});

test('penalties are listed in the order the backend sent them', () => {
    const lines = formatPenalties({
        probability: 63,
        penalties: [
            { factor: 'cloud base', value: 22, unit: 'FL', severity: 'difficult', cost: 19 },
            { factor: 'temperature', value: 31.94, unit: 'C', severity: 'good', cost: 12 },
            { factor: 'wind', value: 16, unit: 'kn', severity: 'good', cost: 6 },
        ],
    });

    assert.deepEqual(lines, [
        'cloud base 22 FL — difficult, −19',
        'temperature 31.9 C — good, −12',
        'wind 16 kn — good, −6',
    ]);
});

// A no-go ended the hour by itself. Writing it as a cost would invite adding it to the
// others, and there are no others -- the backend sends it alone.
test('a no-go is not written as a subtraction', () => {
    const lines = formatPenalties({
        probability: 0,
        penalties: [{ factor: 'visibility', value: 3.2, unit: 'km', severity: 'no-go', cost: 100 }],
    });

    assert.deepEqual(lines, ['visibility 3.2 km — no-go']);
});

// Precipitation is charged for what would fall times how likely it is to fall, so the cost
// alone does not say which hour you are looking at: near-certain drizzle and an unlikely
// downpour can land on the same number.
test('a scaled penalty shows what scaled it', () => {
    const lines = formatPenalties({
        probability: 75,
        penalties: [{
            factor: 'precipitation', value: 3.2, unit: 'mm/h',
            severity: 'difficult', cost: 25,
            scale: { name: 'probability', value: 88, unit: '%' },
        }],
    });

    assert.deepEqual(lines, ['precipitation 3.2 mm/h at 88% — difficult, −25']);
});

test('an unscaled penalty is unchanged by the scale support', () => {
    const lines = formatPenalties({
        probability: 93,
        penalties: [{ factor: 'wind', value: 12, unit: 'kn', severity: 'good', cost: 7 }],
    });

    assert.deepEqual(lines, ['wind 12 kn — good, −7']);
});

// Daylight is an ordinal (day / twilight / night), so it arrives without a unit and its
// number would mean nothing to a reader.
test('a factor without a unit shows no value', () => {
    const lines = formatPenalties({
        probability: 0,
        penalties: [{ factor: 'daylight', value: 2, unit: '', severity: 'no-go', cost: 100 }],
    });

    assert.deepEqual(lines, ['daylight — no-go']);
});
