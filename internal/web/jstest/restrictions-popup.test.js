import assert from 'node:assert/strict';
import test from 'node:test';

process.env.TZ = 'Europe/Berlin';

const { formatRestrictedAreaPopup } = await import('../frontend/js/restrictions.js');

test('formats a name and one line per window', () => {
    const area = {
        name: 'ED-R37A',
        windows: [
            { from: '2026-08-11T07:00:00Z', to: '2026-08-11T15:00:00Z', lower: 'GND', upper: 'A050' },
        ],
    };

    const got = formatRestrictedAreaPopup(area);

    assert.equal(got.name, 'ED-R37A');
    assert.equal(got.lines.length, 1);
    assert.equal(got.lines[0], 'Tue 11 Aug 07:00–15:00Z · GND–A050');
});

// The reason this function exists as a separate, DOM-free step: the AUP name and limits come
// from an undocumented third-party endpoint's regex-parsed HTML, and the caller (airports.js)
// only ever assigns the fields returned here to textContent. A markup payload in either field
// must come back as inert text, not something a caller could be tricked into treating as
// trusted markup.
test('a markup payload in the name comes back as plain text, not markup', () => {
    const area = { name: '<img src=x onerror=alert(1)>', windows: [] };

    const got = formatRestrictedAreaPopup(area);

    assert.equal(got.name, '<img src=x onerror=alert(1)>');
    assert.equal(got.lines.length, 0);
});

test('a markup payload in a limit reaches the line as plain text', () => {
    const area = {
        name: 'ED-R900',
        windows: [
            { from: '2026-08-11T07:00:00Z', to: '2026-08-11T09:00:00Z', lower: '<script>', upper: 'A010' },
        ],
    };

    const got = formatRestrictedAreaPopup(area);

    assert.equal(got.lines.length, 1);
    assert.match(got.lines[0], /<script>–A010$/);
});

test('an area with no windows produces no lines', () => {
    const got = formatRestrictedAreaPopup({ name: 'ED-R305', windows: [] });
    assert.deepEqual(got.lines, []);
});

test('a window with no limits omits the separator rather than leaving it dangling', () => {
    const area = {
        name: 'ED-R1',
        windows: [{ from: '2026-08-11T07:00:00Z', to: '2026-08-11T09:00:00Z' }],
    };

    const got = formatRestrictedAreaPopup(area);

    assert.equal(got.lines.length, 1);
    assert.ok(!got.lines[0].includes('·'));
});

test('tolerates a malformed area object rather than throwing', () => {
    assert.doesNotThrow(() => formatRestrictedAreaPopup({}));
    assert.doesNotThrow(() => formatRestrictedAreaPopup({ name: 'X' }));
});
