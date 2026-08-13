// Bootstrap. Everything else is a module with no side effects beyond registering Chart.js
// plugins, so this is the only file that wires anything together.

import { initializeCharts } from './charts.js';
import { setupManualPanZoom, resetZoom } from './panzoom.js';
import { getAppConfig, initAirportPicker } from './airports.js';
import { loadWeatherData, startAutoRefresh } from './api.js';
import { applyDensity } from './viewport.js';

document.addEventListener('DOMContentLoaded', function() {
    applyDensity();
    initializeCharts();

    // The zoom is set once the data is in, by updateCharts -- see initialZoomHours(),
    // which needs the actual labels and the laid-out plot width to choose it.

    // The airport list has to arrive before the first weather request, so the initial
    // load hangs off the config fetch rather than running beside it.
    initAirportPicker(loadWeatherData).then(() => {
        updateBuildLabel();
        return loadWeatherData();
    });

    // initializeCharts is synchronous, so the canvases already exist here.
    setupManualPanZoom();

    wireControls();
    startAutoRefresh();
});

// The commit the running binary was built from, next to the model run: one says how old the
// weather is, the other how old the application is.
//
// Read once from the config, and it stays true without polling — the status check reloads
// the page when the server moves to a different commit, so a stale label cannot outlive the
// page showing it. Hidden for an unstamped build, where "unknown" would be noise.
function updateBuildLabel() {
    const commit = (getAppConfig().build || {}).commit;
    const element = document.getElementById('buildCommit');

    element.textContent = commit && commit !== 'unknown' ? `build ${commit}` : '';
    element.hidden = element.textContent === '';
}

// The time-range buttons and the retry button used inline onclick= attributes, which is
// what forced resetZoom and loadWeatherData to be globals. Binding them here keeps the
// module scope closed -- and is what makes a script-src CSP without 'unsafe-inline'
// possible later.
function wireControls() {
    document.querySelectorAll('.chart-instructions button[data-hours]').forEach(button => {
        button.addEventListener('click', () => {
            const raw = button.dataset.hours;
            resetZoom(raw === 'all' ? undefined : Number(raw));
        });
    });

    const retry = document.getElementById('retryButton');
    if (retry) {
        retry.addEventListener('click', loadWeatherData);
    }
}
