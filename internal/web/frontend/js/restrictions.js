// Restricted airspace activity, from /api/restrictions.
//
// Fetched once per forecast load and held here, because two unrelated parts of the page
// want it: the charts shade the home field's ED-R activity, and the map draws every area.
// airports.js must not import api.js (see the dependency rules in the js README), so this
// sits below both rather than beside either.
//
// No timer of its own. The backend refetches the plan every six hours; the page reloads
// whenever a new weather model run appears, which is far more often than that.

// The area the charts shade. EDWN sits inside it, and the AIP requires permission before
// entering -- which is what makes its activation worth seeing next to the weather.
//
// EDWN is also inside ED-R202D, which is not shaded: that one is active most days and
// shading it would paint half the week red, saying nothing a reader could act on. It is
// still drawn on the map like every other area.
export const HOME_AREA = 'ED-R37A';

let cached = { areas: [], fetchedAt: null, degraded: false };

// loadRestrictions refreshes the cache. A failure keeps whatever was there: an airspace
// plan that is a few hours old is useful, an empty one reads as "nothing is active".
export async function loadRestrictions() {
    try {
        const response = await fetch('/api/restrictions');
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        const data = await response.json();
        cached = {
            areas: Array.isArray(data.areas) ? data.areas : [],
            fetchedAt: data.fetched_at || null,
            degraded: Boolean(data.degraded),
        };
    } catch (error) {
        console.error('Error loading airspace restrictions:', error);
    }
    return cached;
}

export function restrictedAreas() {
    return cached.areas;
}

export function restrictionsDegraded() {
    return cached.degraded;
}

// windowsFor returns one area's activity windows, or an empty list. The wire format is
// {from, to, lower, upper}; the bands only need the two timestamps, and the map shows all
// four.
export function windowsFor(name) {
    const area = cached.areas.find(a => a && a.name === name);
    return area && Array.isArray(area.windows) ? area.windows : [];
}

// formatRestrictedAreaPopup turns one area into plain strings for the map popup: a name and
// one line per activity window. Kept free of the DOM, like every other function in this
// module, so it can be tested under node --test; airports.js is what turns this into actual
// elements (textContent only -- see restrictedAreaPopup there for why that separation
// matters here specifically).
export function formatRestrictedAreaPopup(area) {
    return {
        name: area && area.name || '',
        lines: (area && Array.isArray(area.windows) ? area.windows : []).map(formatRestrictionWindow),
    };
}

function formatRestrictionWindow(window) {
    const from = new Date(window.from);
    const to = new Date(window.to);
    const day = from.toLocaleDateString('en-GB', { weekday: 'short', day: 'numeric', month: 'short', timeZone: 'UTC' });
    const hhmm = t => `${String(t.getUTCHours()).padStart(2, '0')}:${String(t.getUTCMinutes()).padStart(2, '0')}`;

    // UTC, because that is how the plan publishes them and how they are discussed on the
    // radio -- the same reasoning as the model run label.
    let text = `${day} ${hhmm(from)}–${hhmm(to)}Z`;
    const limits = [window.lower, window.upper].filter(Boolean).join('–');
    if (limits) {
        text += ` · ${limits}`;
    }
    return text;
}
