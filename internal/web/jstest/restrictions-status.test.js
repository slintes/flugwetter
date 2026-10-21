import assert from 'node:assert/strict';
import test from 'node:test';

const { restrictionsUnavailable, loadRestrictions } = await import('../frontend/js/restrictions.js');

test('warns for missing, empty or degraded EDR data', () => {
    assert.equal(restrictionsUnavailable(), true);
    assert.equal(restrictionsUnavailable({ areas: [], fetchedAt: '2026-10-21T00:00:00Z', degraded: false }), true);
    assert.equal(restrictionsUnavailable({ areas: [{}], fetchedAt: null, degraded: false }), true);
    assert.equal(restrictionsUnavailable({ areas: [{}], fetchedAt: '2026-10-21T00:00:00Z', degraded: true }), true);
    assert.equal(restrictionsUnavailable({ areas: [{}], fetchedAt: '2026-10-21T00:00:00Z', degraded: false }), false);
});

test('a failed request marks retained EDR data as degraded', async t => {
    const previousFetch = globalThis.fetch;
    t.after(() => { globalThis.fetch = previousFetch; });
    globalThis.fetch = async () => ({ ok: true, json: async () => ({ areas: [{ name: 'ED-R37A' }], fetched_at: '2026-10-21T00:00:00Z', degraded: false }) });
    await loadRestrictions();
    assert.equal(restrictionsUnavailable(), false);
    globalThis.fetch = async () => ({ ok: false, status: 503 });
    const data = await loadRestrictions();
    assert.equal(data.areas[0].name, 'ED-R37A');
    assert.equal(restrictionsUnavailable(), true);
});
