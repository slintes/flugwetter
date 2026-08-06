// Formats the VFR score's breakdown for the tooltip.
//
// The backend scores every hour against one table of limits (internal/server/vfr.go) and
// sends back the factors that cost it something, worst first. Without this the only way to
// find out why an hour scored 95 rather than 100 was to run the server with debug logging.
//
// Deliberately free of Chart.js and of the DOM, so internal/web/jstest/ can run it under
// node --test -- the same rule viewport.js, barbs.js and time.js follow.

// formatPenalties turns one VFR point into the tooltip's lines.
export function formatPenalties(point) {
    if (!point || point.probability < 0) {
        return ['no forecast for this hour'];
    }

    const penalties = point.penalties || [];
    if (penalties.length === 0) {
        return ['nothing against it'];
    }

    return penalties.map(formatPenalty);
}

// formatPenalty renders one factor: what it was, and what it cost.
//
// A no-go is not written as a cost. It did not subtract 100 points from something -- it
// ended the hour on its own, and reporting it as arithmetic would invite the reader to add
// it to the others.
function formatPenalty(penalty) {
    const parts = [penalty.factor, formatValue(penalty.value, penalty.unit)];

    // A scaled factor was charged for what would happen, times how likely it is to happen.
    // Showing only the cost would make an hour of near-certain drizzle and one of unlikely
    // heavy rain look like the same forecast.
    if (penalty.scale) {
        parts.push(`at ${formatValue(penalty.scale.value, penalty.scale.unit)}`);
    }
    const head = parts.filter(Boolean).join(' ');

    if (penalty.severity === 'no-go') {
        return `${head} — no-go`;
    }
    return `${head} — ${penalty.severity}, −${penalty.cost}`;
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
