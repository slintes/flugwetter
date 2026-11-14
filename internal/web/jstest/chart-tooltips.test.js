import { test, after } from 'node:test';
import assert from 'node:assert/strict';

// Capture the real chart constructors' options without needing a canvas. This tests the
// wiring as well as the policy: a VFR-only `animations: false` must not slip through again.
const savedGlobals = Object.fromEntries(['Chart', 'Image', 'document', 'window']
    .map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
after(() => {
    for (const [key, descriptor] of Object.entries(savedGlobals)) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor);
        else delete globalThis[key];
    }
});

globalThis.Chart = class {
    static Interaction = { modes: {} };
    static helpers = { getRelativePosition: event => event };
    static register() {}
    constructor(ctx, config) {
        this.options = config.options;
    }
};
globalThis.Image = class {};
globalThis.document = { getElementById: () => ({ getContext: () => ({}) }) };
globalThis.window = { innerWidth: 1200 };

const { charts, initializeCharts } = await import('../frontend/js/charts.js');
initializeCharts();

for (const [name, chart] of Object.entries(charts)) {
    test(`${name} tooltip eases movement briefly, but resizes immediately`, () => {
        const { animation, animations } = chart.options.plugins.tooltip;
        assert.deepEqual(animation, { duration: 100, easing: 'easeOutCubic' });
        assert.deepEqual(animations.numbers.properties, ['x', 'y', 'caretX', 'caretY']);
        assert.deepEqual(animations.size.properties, ['width', 'height']);
        assert.equal(animations.size.duration, 0);
        assert.equal(animations.opacity.duration, 100);
    });
}

test('all tooltips retain their labels and share the local timestamp title', () => {
    const x = Date.UTC(2026, 10, 14, 12);
    for (const chart of Object.values(charts)) {
        const { callbacks } = chart.options.plugins.tooltip;
        assert.equal(callbacks.title([{ parsed: { x } }]), new Date(x).toLocaleString());
        assert.equal(typeof callbacks.label, 'function');
    }
    assert.equal(charts.vfr.options.plugins.tooltip.displayColors, false);
});

test('each chart gets its own mutable animation options', () => {
    const policies = Object.values(charts).map(chart => chart.options.plugins.tooltip.animations);
    assert.equal(new Set(policies).size, 4);
    assert.equal(new Set(policies.map(policy => policy.numbers)).size, 4);
});

test('VFR still selects only drawn badges at coarse zoom, not the hidden hour underneath', () => {
    assert.equal(charts.vfr.options.interaction.mode, 'vfrVisible');
    const data = [10, 20, 30, 40].map(x => ({ x, rating: 'good' }));
    const elements = data.map(point => ({ x: point.x, y: 35 }));
    const chart = {
        chartArea: { left: 0, right: 50, top: 0, bottom: 100 },
        data: { datasets: [{ data }] },
        getDatasetMeta: () => ({ data: elements }),
        isDatasetVisible: () => true,
        scales: { x: { getPixelForValue: x => x } }
    };
    // Wide badges are 26px: only hours at x=10 and x=40 are drawn.
    const select = event => Chart.Interaction.modes.vfrVisible(chart, event);
    assert.equal(select({ x: 20, y: 35 })[0].index, 0);
    assert.equal(select({ x: 30, y: 35 })[0].index, 3);
    assert.deepEqual(select({ x: -1, y: 35 }), []);
    chart.isDatasetVisible = () => false;
    assert.deepEqual(select({ x: 30, y: 35 }), []);
});
