// The VFR chart skips overlapping badges when zoomed out. Drawing and interaction must
// agree on which hours survived, or a tooltip can describe an invisible neighboring hour.
export function visibleVfrIndices(points, pixelForTime, left, right, badgeWidth) {
    const indices = [];
    let previousRight = -Infinity;
    for (let i = 0; i < points.length; i++) {
        const point = points[i];
        if (!point || point.rating === undefined) continue;
        const x = pixelForTime(point.x);
        if (!Number.isFinite(x) || x < left || x > right || x - badgeWidth / 2 < previousRight) continue;
        indices.push(i);
        previousRight = x + badgeWidth / 2;
    }
    return indices;
}
