import assert from 'node:assert/strict';
import test from 'node:test';

// viewport.js reads window.innerWidth and window.devicePixelRatio. It imports nothing, so
// a stub global is all it needs to run outside a browser.
globalThis.window = { innerWidth: 1400, devicePixelRatio: 1 };
globalThis.document = { documentElement: { dataset: {} } };

const {
    AXIS_WIDTHS_WIDE, AXIS_WIDTHS_NARROW, NARROW_VIEWPORT, WIDE_VIEWPORT,
    VFR_METRICS_WIDE, VFR_METRICS_NARROW,
    TOOLTIP_FONT_WIDE, TOOLTIP_FONT_NARROW,
    axisWidths, vfrMetrics, tooltipFont, isNarrowViewport, isWideViewport, isLowDensity,
    applyDensity, pinAxisWidth,
} = await import('../frontend/js/viewport.js');

const atWidth = (px, fn) => {
    window.innerWidth = px;
    try { return fn(); } finally { window.innerWidth = 1400; }
};

// The breakpoint is inclusive of NARROW_VIEWPORT itself. Phones report 360-430 CSS px in
// portrait, so 600 has to sit clear above that band.
test('axisWidths switches at the narrow breakpoint', () => {
    assert.equal(NARROW_VIEWPORT, 600);
    assert.deepEqual(atWidth(360, axisWidths), AXIS_WIDTHS_NARROW);
    assert.deepEqual(atWidth(599, axisWidths), AXIS_WIDTHS_NARROW);
    assert.deepEqual(atWidth(600, axisWidths), AXIS_WIDTHS_NARROW);
    assert.deepEqual(atWidth(601, axisWidths), AXIS_WIDTHS_WIDE);
    assert.deepEqual(atWidth(1400, axisWidths), AXIS_WIDTHS_WIDE);
});

// The narrow widths exist because 82+81 of a 360px screen is 45% of it, leaving too little
// plot area for even one hour.
test('narrow axis widths leave a usable plot on a phone', () => {
    const narrowPlot = 360 - AXIS_WIDTHS_NARROW.left - AXIS_WIDTHS_NARROW.right;
    const widePlot = 360 - AXIS_WIDTHS_WIDE.left - AXIS_WIDTHS_WIDE.right;
    assert.ok(widePlot < 200, `wide axes would leave ${widePlot}px on a 360px screen`);
    assert.ok(narrowPlot > 240, `narrow axes leave only ${narrowPlot}px`);
});

test('vfrMetrics switches at the same breakpoint as the axes', () => {
    assert.deepEqual(atWidth(360, vfrMetrics), VFR_METRICS_NARROW);
    assert.deepEqual(atWidth(600, vfrMetrics), VFR_METRICS_NARROW);
    assert.deepEqual(atWidth(601, vfrMetrics), VFR_METRICS_WIDE);
});

test('tooltipFont switches at the same breakpoint as the axes', () => {
    assert.deepEqual(atWidth(360, tooltipFont), TOOLTIP_FONT_NARROW);
    assert.deepEqual(atWidth(600, tooltipFont), TOOLTIP_FONT_NARROW);
    assert.deepEqual(atWidth(601, tooltipFont), TOOLTIP_FONT_WIDE);
});

// Chart.js defaults to 12px. The whole point of these is to be bigger than that, and
// bigger on a roomy viewport than on a phone.
test('tooltip text is larger than the Chart.js default at both sizes', () => {
    for (const metrics of [TOOLTIP_FONT_NARROW, TOOLTIP_FONT_WIDE]) {
        assert.ok(metrics.body > 12, `body ${metrics.body} should beat the 12px default`);
        assert.ok(metrics.title >= metrics.body, 'the title should not be smaller than the body');
    }
    assert.ok(TOOLTIP_FONT_WIDE.body > TOOLTIP_FONT_NARROW.body);
});

test('isNarrowViewport and isWideViewport bracket the middle', () => {
    assert.equal(WIDE_VIEWPORT, 1600);
    assert.equal(atWidth(360, isNarrowViewport), true);
    assert.equal(atWidth(360, isWideViewport), false);
    assert.equal(atWidth(1000, isNarrowViewport), false);
    assert.equal(atWidth(1000, isWideViewport), false);
    assert.equal(atWidth(1600, isWideViewport), true);
    assert.equal(atWidth(2560, isWideViewport), true);
});

// Both axis widths must come from one axisWidths() call, or the charts drift apart. This
// pins that they at least agree with each other at every width.
test('pinAxisWidth writes the width for the matching side', () => {
    const wide = { width: 0 };
    atWidth(1400, () => pinAxisWidth('left')(wide));
    assert.equal(wide.width, AXIS_WIDTHS_WIDE.left);

    const narrow = { width: 0 };
    atWidth(390, () => pinAxisWidth('right')(narrow));
    assert.equal(narrow.width, AXIS_WIDTHS_NARROW.right);
});

// A devicePixelRatio below 1 (browser zoom under 100%) makes fixed pixel sizes render at
// fractional physical pixels, which mangles the map's bold labels.
test('isLowDensity keys off a sub-1 devicePixelRatio', () => {
    const restore = window.devicePixelRatio;
    try {
        window.devicePixelRatio = 0.75;
        assert.equal(isLowDensity(), true);
        window.devicePixelRatio = 1;
        assert.equal(isLowDensity(), false);
        window.devicePixelRatio = 2;
        assert.equal(isLowDensity(), false);
    } finally {
        window.devicePixelRatio = restore;
    }
});

test('applyDensity exposes the density to CSS', () => {
    const restore = window.devicePixelRatio;
    try {
        window.devicePixelRatio = 0.75;
        applyDensity();
        assert.equal(document.documentElement.dataset.density, 'low');
        window.devicePixelRatio = 1;
        applyDensity();
        assert.equal(document.documentElement.dataset.density, 'normal');
    } finally {
        window.devicePixelRatio = restore;
    }
});
