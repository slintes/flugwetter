// Viewport-dependent sizing. Deliberately free of any Chart.js import: the breakpoint
// arithmetic here is the part worth testing, and this way it loads in node.

// All four charts share one time axis, so their plot areas have to start and end at the
// same x. Left to itself, Chart.js sizes each y axis to its own tick labels ("35" vs
// "10000ft"), which pushed the four plot areas 59px apart on the left and 72px on the
// right — the same hour sat at a different pixel in every chart. Pinning both axis widths
// to a constant is what keeps them aligned; the values are the widest the axes naturally
// wanted, so no label is clipped. A new chart must use the same two constants, and the
// VFR chart, which has no visible axis at all, pads by them instead.
export const AXIS_WIDTHS_WIDE = { left: 82, right: 81 };
// On a phone 82+81 is 163px of a 360px screen — 44% of it, taken from every chart. These
// clear the widest tick label ("10000ft", "30 mm") but not the rotated axis title as well,
// which is why the titles are hidden at this size (see the responsiveAxes plugin).
export const AXIS_WIDTHS_NARROW = { left: 54, right: 52 };

// Phones report 360-430 CSS px in portrait, so 600 puts the switch clear of that band.
export const NARROW_VIEWPORT = 600;
// Above this the map's markers and labels, which are fixed pixel sizes, start to look
// undersized against the rest of the page. Kept in step with the media query for
// .airport-tooltip in styles.css.
export const WIDE_VIEWPORT = 1600;

export function isNarrowViewport() {
    return window.innerWidth <= NARROW_VIEWPORT;
}

export function isWideViewport() {
    return window.innerWidth >= WIDE_VIEWPORT;
}

// A devicePixelRatio below 1 means the page is being rendered smaller than its CSS pixels
// — browser zoom under 100%, or a display scale below it. Two consequences, both of which
// hit the map labels: everything is physically smaller than the CSS numbers suggest, and a
// size that is not a whole number of physical pixels (14px at dpr 0.75 is 10.5) rasterises
// at a half pixel and is then downsampled, which visibly mangles bold capitals.
//
// The compensating sizes live in styles.css under [data-density="low"] and are multiples
// of 4, so they stay whole physical pixels at 0.75.
export function isLowDensity() {
    return window.devicePixelRatio < 1;
}

// applyDensity exposes the above to CSS. devicePixelRatio changes when the user zooms, and
// a zoom fires resize, so this is re-run from the same listener as the map sizing.
export function applyDensity() {
    document.documentElement.dataset.density = isLowDensity() ? 'low' : 'normal';
}

export function axisWidths() {
    return isNarrowViewport() ? AXIS_WIDTHS_NARROW : AXIS_WIDTHS_WIDE;
}

// afterFit runs on every layout, so crossing the breakpoint re-pins all four charts with
// no resize handler of our own. Both sides must come from the same source or the charts
// drift apart again.
export const pinAxisWidth = side => scale => { scale.width = axisWidths()[side]; };

// The VFR chart's weather icons and probability labels, which are drawn by the vfrText
// plugin rather than by Chart.js. At full size one hour needs 56px, and a phone's plot
// area cannot give that to even one hour, so they shrink with the axes.
export const VFR_METRICS_WIDE = { icon: 36, font: '24px Narrow', unknownFont: '20px Narrow' };
export const VFR_METRICS_NARROW = { icon: 20, font: '14px Narrow', unknownFont: '12px Narrow' };

export function vfrMetrics() {
    return isNarrowViewport() ? VFR_METRICS_NARROW : VFR_METRICS_WIDE;
}

// Tooltip text. Chart.js defaults to 12px, which is small on a high-density display —
// physically smaller than the CSS number suggests, and the tooltip is the one place the
// charts show prose rather than a symbol. The VFR tooltip in particular lists a line per
// penalty and is meant to be read, not glanced at.
//
// Narrow takes the same jump the axes and the VFR labels take at the breakpoint, and for a
// harder reason than taste: Chart.js neither wraps a tooltip line nor shrinks it to fit, so
// the width of the longest line is a hard constraint. On a 360px screen the VFR breakdown
// is what sets the ceiling, which is also why it drops the severity word there.
// The padding is doing two jobs. The second is headroom: Chart.js sizes the tooltip box
// from a measurement a few percent narrower than what it then draws, and clips whatever
// overflows -- which is the end of the longest line, where the cost is. Because the
// shortfall is proportional to the text, shortening the text does not fix it; padding is
// the one lever that adds absolute room. These values were arrived at by measuring the
// rendered box against the rendered text, not derived.
export const TOOLTIP_FONT_WIDE = { title: 17, body: 16, padding: 14 };
export const TOOLTIP_FONT_NARROW = { title: 15, body: 14, padding: 14 };

export function tooltipFont() {
    return isNarrowViewport() ? TOOLTIP_FONT_NARROW : TOOLTIP_FONT_WIDE;
}
