import assert from 'node:assert/strict';
import test from 'node:test';

import { barbComponents, isCalm, CALM_THRESHOLD_KT } from '../frontend/js/barbs.js';

// Wind barbs are read off the chart at a glance, so the decomposition has to be exactly
// the meteorological convention: a pennant per 50kt, a full barb per 10kt, a half barb for
// a remaining 5kt, and the speed rounded before any of that.
test('barbComponents follows the standard decomposition', () => {
    const cases = [
        [0, { pennants: 0, fullBarbs: 0, halfBarb: 0 }],
        [4, { pennants: 0, fullBarbs: 0, halfBarb: 0 }],
        [5, { pennants: 0, fullBarbs: 0, halfBarb: 1 }],
        [9, { pennants: 0, fullBarbs: 0, halfBarb: 1 }],
        [10, { pennants: 0, fullBarbs: 1, halfBarb: 0 }],
        [15, { pennants: 0, fullBarbs: 1, halfBarb: 1 }],
        [45, { pennants: 0, fullBarbs: 4, halfBarb: 1 }],
        [50, { pennants: 1, fullBarbs: 0, halfBarb: 0 }],
        [65, { pennants: 1, fullBarbs: 1, halfBarb: 1 }],
        [100, { pennants: 2, fullBarbs: 0, halfBarb: 0 }],
        [115, { pennants: 2, fullBarbs: 1, halfBarb: 1 }],
    ];

    for (const [speed, want] of cases) {
        assert.deepEqual(barbComponents(speed), want, `${speed} kn`);
    }
});

// The model reports fractional knots, and the rounding is to the nearest whole knot before
// the decomposition -- not to the nearest 5. So 14.5kt rounds up to 15 and gains its half
// barb, while 47.6kt rounds to 48 and still renders as four barbs and a half rather than a
// pennant.
test('barbComponents rounds to whole knots before decomposing', () => {
    assert.deepEqual(barbComponents(12.4), { pennants: 0, fullBarbs: 1, halfBarb: 0 });
    assert.deepEqual(barbComponents(14.5), { pennants: 0, fullBarbs: 1, halfBarb: 1 });
    assert.deepEqual(barbComponents(47.6), { pennants: 0, fullBarbs: 4, halfBarb: 1 });
    assert.deepEqual(barbComponents(49.5), { pennants: 1, fullBarbs: 0, halfBarb: 0 });
});

// Every decomposition must add back up to the rounded speed, or the barb is lying.
test('barbComponents is lossless to the nearest 5kt', () => {
    for (let speed = 0; speed <= 150; speed += 1) {
        const { pennants, fullBarbs, halfBarb } = barbComponents(speed);
        const shown = pennants * 50 + fullBarbs * 10 + halfBarb * 5;
        const remainder = speed - shown;
        assert.ok(remainder >= 0 && remainder < 5,
            `${speed} kn renders as ${shown} kn, off by ${remainder}`);
    }
});

test('isCalm switches at the documented threshold', () => {
    assert.equal(CALM_THRESHOLD_KT, 3);
    assert.equal(isCalm(0), true);
    assert.equal(isCalm(2.9), true);
    assert.equal(isCalm(3), false);
    assert.equal(isCalm(10), false);
});
