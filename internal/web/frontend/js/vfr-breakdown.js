// Formats the VFR rating's breakdown for the tooltip.
//
// The backend scores every hour against one table of limits (internal/server/vfr.go) and
// sends back every factor that scored worse than perfect, worst first. Without this the
// only way to find out why an hour rated "difficult" rather than "good" was to run the
// server with debug logging.
//
// Deliberately free of Chart.js and of the DOM, so internal/web/jstest/ can run it under
// node --test -- the same rule viewport.js, barbs.js and time.js follow.

// formatBreakdown turns one VFR point into the tooltip's lines.
export function formatBreakdown(point) {
    if (!point || !point.rating) {
        return ['no forecast for this hour'];
    }

    const factors = point.factors || [];
    const lines = factors.length === 0 ? ['nothing against it'] : factors.map(formatFactor);

    // Independent of the rating above -- see gustWarning in internal/server/weather.go --
    // so it is appended after the rating's own lines are decided, not instead of them.
    if (point.gustWarning) {
        lines.push(formatGustWarning(point.gustWarning));
    }

    return lines;
}

// formatFactor renders one factor: what it was, and how it rated.
function formatFactor(factor) {
    const parts = [factor.factor, formatValue(factor.value, factor.unit)];

    // A scaled factor was judged on what would happen, times how likely it is to happen.
    // Showing only the severity would make an hour of near-certain drizzle and one of
    // unlikely heavy rain look like the same forecast.
    if (factor.scale) {
        parts.push(`at ${formatValue(factor.scale.value, factor.scale.unit)}`);
    }
    const head = parts.filter(Boolean).join(' ');

    return `${head} — ${factor.severity}`;
}

// formatGustWarning names the raw reading(s) that tripped the warning, not just that one
// did -- "check before you fly" alone gives nothing to weigh the warning against.
function formatGustWarning(warning) {
    return `gusts ${formatValue(warning.windGusts, 'kn')}, crosswind gusts ${formatValue(warning.crosswindGusts, 'kn')} — check before you fly`;
}

// Values arrive at full model precision (7.265331983420373 kn). One decimal is as much as
// any of these factors is meaningfully known to.
//
// A factor with no unit is an ordinal rather than a measurement -- daylight is scored as
// day / twilight / night -- and its number would mean nothing here, so only the name and
// the band are shown.
function formatValue(value, unit) {
    if (!unit || typeof value !== 'number' || !Number.isFinite(value)) {
        return '';
    }

    const rounded = Math.round(value * 10) / 10;
    // Percent hugs its number; every other unit here is a word and takes a space.
    return unit === '%' ? `${rounded}%` : `${rounded} ${unit}`;
}
