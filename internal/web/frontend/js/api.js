// Fetching the forecast and pushing it into the charts.

import { charts } from './charts.js';
import { applyInitialZoomOnce } from './panzoom.js';
import { getCurrentAirportId } from './airports.js';
import { toEpochMs } from './time.js';
import { shouldReload, latestModelRun, formatModelRun } from './status.js';

// When the data on screen was last replaced.
let lastLoadedAt = Date.now();

// The model run behind the forecast currently on screen. The status poll compares against
// this, so it is what makes a reload conditional rather than periodic.
let renderedRun = null;

// The two conditions the error area can be in. They are tracked separately because they are
// different events: one means there is no forecast, the other means there is one but the
// mechanism that keeps it current has stopped working.
let loadFailed = false;
let runsDegraded = false;

// renderErrorArea shows the box if either condition holds, and each message independently.
//
// The Retry button belongs only to the load failure. Retrying does nothing for a backend
// poller that cannot reach Open-Meteo's metadata, and offering a button that cannot help is
// worse than offering none.
function renderErrorArea() {
    document.getElementById('errorMessage').hidden = !loadFailed;
    document.getElementById('retryButton').hidden = !loadFailed;
    document.getElementById('degradedMessage').hidden = !runsDegraded;
    document.getElementById('error').style.display = loadFailed || runsDegraded ? 'block' : 'none';
}

// The scrim is delayed rather than shown immediately: the backend answers a warm cache in
// single-digit milliseconds, so an instant spinner would blink on every 15-minute refresh
// and on every switch back to an airport already fetched — more distracting than no
// indicator at all. A cold airport goes to Open-Meteo and takes seconds, which is the case
// worth covering.
const LOADING_SCRIM_DELAY_MS = 150;
let loadingScrimTimer = null;

function showLoadingScrim() {
    clearTimeout(loadingScrimTimer);
    loadingScrimTimer = setTimeout(() => {
        document.getElementById('chartsLoading').hidden = false;
    }, LOADING_SCRIM_DELAY_MS);
}

function hideLoadingScrim() {
    clearTimeout(loadingScrimTimer);
    document.getElementById('chartsLoading').hidden = true;
}

export async function loadWeatherData() {
    try {
        showLoadingScrim();
        loadFailed = false;
        renderErrorArea();

        const airport = getCurrentAirportId();
        const url = airport
            ? `/api/weather?airport=${encodeURIComponent(airport)}`
            : '/api/weather';
        const response = await fetch(url);

        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }

        const data = await response.json();

        updateCharts(data);
        hideLoadingScrim();

    } catch (error) {
        console.error('Error loading weather data:', error);
        hideLoadingScrim();
        loadFailed = true;
        renderErrorArea();
    }
}

// formatAge renders a whole number of minutes as something readable next to a forecast.
export function formatAge(minutes) {
    if (minutes < 60) {
        return `${minutes} minute${minutes === 1 ? '' : 's'}`;
    }
    const hours = Math.floor(minutes / 60);
    const rest = minutes % 60;
    if (rest === 0) {
        return `${hours} hour${hours === 1 ? '' : 's'}`;
    }
    return `${hours} h ${rest} min`;
}

// The backend falls back to an expired cache entry when Open-Meteo is unreachable, and
// flags it with `stale`. That has to be visible: the charts look identical either way, and
// for flight planning old weather shown as current is the failure that matters.
function updateStaleBanner(data) {
    const element = document.getElementById('stale');

    if (!data.stale || !data.generated_at) {
        element.style.display = 'none';
        return;
    }

    const ageMinutes = Math.max(0, Math.round((Date.now() - new Date(data.generated_at).getTime()) / 60000));
    element.textContent = `Upstream unavailable — showing forecast data from ${formatAge(ageMinutes)} ago.`;
    element.style.display = 'block';
}

// The forecast's own age, which generated_at is not: that one says when we fetched our
// copy, this says when the weather model behind it last ran.
function updateModelRunLabel(data) {
    renderedRun = latestModelRun(data.model_runs);

    const element = document.getElementById('modelRun');
    const label = formatModelRun(renderedRun);
    element.textContent = label;
    element.hidden = label === '';
}

export function updateCharts(data) {
    updateStaleBanner(data);
    updateModelRunLabel(data);
    lastLoadedAt = Date.now();

    // Guarded like wind_data and vfr_data below. Unguarded, a payload missing this one
    // series threw here and aborted updateCharts before any of the other three charts were
    // touched, so a partial response blanked everything rather than degrading.
    const temperatureData = data.temperature_data || [];

    // Update temperature chart with time-based data
    const tempData = temperatureData.map(point => ({
        x: toEpochMs(point.time),
        y: point.temperature
    }));
    
    const dewPointData = temperatureData.map(point => ({
        x: toEpochMs(point.time),
        y: point.dew_point
    }));
    
    const precipitationData = temperatureData.map(point => ({
        x: toEpochMs(point.time),
        y: point.precipitation,
        precipitationProbability: point.precipitation_probability // Include probability for scriptable functions
    }));
    
    const precipitationProbabilityData = temperatureData.map(point => ({
        x: toEpochMs(point.time),
        y: point.precipitation_probability
    }));
    
    charts.temperature.data.datasets[0].data = tempData;
    charts.temperature.data.datasets[1].data = dewPointData;
    charts.temperature.data.datasets[2].data = precipitationData;
    charts.temperature.data.datasets[3].data = precipitationProbabilityData;
    
    charts.temperature.update();
    
    // Update cloud chart with scatter data and visibility data
    const scatterData = [];
    const visibilityData = [];
    const cloudBaseData = [];

    data.cloud_data.forEach(timePoint => {
        const timeValue = toEpochMs(timePoint.time);
        
        // Process cloud layers
        if (timePoint.cloud_layers) {
            timePoint.cloud_layers.forEach(layer => {
                scatterData.push({
                    x: timeValue,
                    y: layer.height_feet,
                    coverage: layer.coverage,
                });
            });
        }

        // The visibility is already in kilometers.
        // `null` means the model had no value; 0 means dense fog. Both are falsy, so a
        // truthiness test here silently dropped the worst weather in the forecast.
        if (timePoint.visibility != null) {
            visibilityData.push({
                x: timeValue,
                y: timePoint.visibility
            });
        }

        // The cloud base, as a flight level. Same trap: FL0 is a ceiling below 100ft,
        // not a missing reading.
        if (timePoint.base != null) {
            cloudBaseData.push({
                x: timeValue,
                y: timePoint.base
            });
        }
    });
    
    charts.cloud.data.datasets[0].data = scatterData;
    charts.cloud.data.datasets[1].data = visibilityData;
    charts.cloud.data.datasets[2].data = cloudBaseData;
    charts.cloud.update();

    // Update wind chart with scatter data
    const windScatterData = [];
    const windSpeed10mData = [];
    const windGusts10mData = [];
    const crosswind10mData = [];
    const crosswindGusts10mData = [];

    if (data.wind_data) {
        data.wind_data.forEach(timePoint => {
            const timeValue = toEpochMs(timePoint.time);
            
            // Guarded: this was the one unguarded forEach in updateCharts, and a throw
            // here aborts the function before the VFR chart is updated further down.
            if (timePoint.wind_layers) {
                timePoint.wind_layers.forEach(layer => {
                    windScatterData.push({
                        x: timeValue,
                        y: layer.height_feet,
                        speed: layer.speed,
                        direction: layer.direction
                    });
                });
            }

            // Add line data for wind speed and gusts at 10m
            windSpeed10mData.push({
                x: timeValue,
                y: timePoint.wind_speed_10m
            });

            windGusts10mData.push({
                x: timeValue,
                y: timePoint.wind_gusts_10m
            });

            // Add line data for the crosswind components at 10m
            crosswind10mData.push({
                x: timeValue,
                y: timePoint.crosswind_10m
            });

            crosswindGusts10mData.push({
                x: timeValue,
                y: timePoint.crosswind_gusts_10m
            });

        });
    }

    charts.wind.data.datasets[0].data = windScatterData;
    charts.wind.data.datasets[1].data = windSpeed10mData;
    charts.wind.data.datasets[2].data = windGusts10mData;
    charts.wind.data.datasets[3].data = crosswind10mData;
    charts.wind.data.datasets[4].data = crosswindGusts10mData;
    charts.wind.update();

    const vfrData = [];
    if (data.vfr_data) {
        data.vfr_data.forEach(timePoint => {
            const timeValue = toEpochMs(timePoint.time);

            vfrData.push({
                x: timeValue,
                y: timePoint.probability / 100, // Convert to 0-1 range for chart
                probability: timePoint.probability, // Keep original percentage for display
                weatherCode: timePoint.weather_code, // Include weather code for icon display
                visibilityKnown: timePoint.visibility_known, // false => score is an estimate
                penalties: timePoint.penalties // what the score lost, worst first; absent when nothing did
            })
        })
    }
    charts.vfr.data.datasets[0].data = vfrData;
    charts.vfr.update();

    // Only now are the labels and the laid-out plot width both known.
    applyInitialZoomOnce();
}


// How often to ask whether anything changed. Far more often than the forecast actually
// changes -- the models run every three hours -- because the question costs ~130 bytes and
// the answer is almost always no.
export const STATUS_POLL_INTERVAL_MS = 5 * 60 * 1000;

// The clock fallback, used only when run times are unknown on one side or the other. This
// is what the refresh interval used to be unconditionally.
export const MAX_AGE_MS = 15 * 60 * 1000;

// checkForNewData asks the backend what run it is on and reloads only if that differs from
// what is on screen. A network failure here is not surfaced: the forecast already displayed
// is still valid, and the next poll will try again.
export async function checkForNewData() {
    let status = null;
    try {
        const response = await fetch('/api/status');
        if (response.ok) {
            status = await response.json();
        }
    } catch (error) {
        console.error('Error polling status:', error);
    }

    runsDegraded = Boolean(status && status.model_runs_degraded);
    renderErrorArea();

    const latest = status ? status.latest_initialized_at : null;
    if (shouldReload(renderedRun, latest, Date.now() - lastLoadedAt, MAX_AGE_MS)) {
        await loadWeatherData();
    }
}

export function startAutoRefresh() {
    setInterval(checkForNewData, STATUS_POLL_INTERVAL_MS);

    // A background tab's timers are throttled and a sleeping phone's do not run at all, so
    // the interval alone can leave a forecast hours old on screen the moment the tab is
    // looked at again -- which for this app is exactly when it is about to be trusted.
    // Unconditional now, because asking is cheap: the check itself decides whether anything
    // needs fetching.
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') {
            checkForNewData();
        }
    });
}
