import { test } from 'node:test';
import { deepStrictEqual } from 'node:assert';
import { visibleVfrIndices } from '../frontend/js/vfr-visible.js';

const point = x => ({ x, rating: 'good' });
const pixels = x => x;

test('keeps every badge when there is enough room, including unscored hours', () => {
    deepStrictEqual(visibleVfrIndices([point(10), {x: 30, rating: ''}, point(50)], pixels, 0, 60, 20), [0, 1, 2]);
});

test('skips overlapping badges, but measures from the last drawn badge', () => {
    deepStrictEqual(visibleVfrIndices([point(10), point(20), point(30), point(40)], pixels, 0, 50, 20), [0, 2]);
});

test('ignores offscreen and invalid points without letting them suppress visible ones', () => {
    const data = [point(-1), null, point(NaN), {x: 5}, point(1), point(21), point(101)];
    deepStrictEqual(visibleVfrIndices(data, pixels, 0, 100, 20), [4, 5]);
});

test('uses projected coordinates rather than assuming timestamps are pixels', () => {
    deepStrictEqual(visibleVfrIndices([point(1), point(2), point(3)], x => x * 10, 0, 40, 15), [0, 2]);
});
