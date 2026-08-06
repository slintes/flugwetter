// Fetching the forecast and pushing it into the charts.

import { charts } from './charts.js';
import { applyInitialZoomOnce } from './panzoom.js';
import { getCurrentAirportId } from './airports.js';
import { toEpochMs } from './time.js';

// When the data on screen was last replaced.
let lastLoadedAt = Date.now();

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
    const errorElement = document.getElementById('error');

    try {
        showLoadingScrim();
        errorElement.style.display = 'none';

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
        errorElement.style.display = 'block';
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

export function updateCharts(data) {
    updateStaleBanner(data);
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


// Auto-refresh. The interval matches the backend's cache TTL, so a refresh lands roughly
// when new data becomes available.
export const REFRESH_INTERVAL_MS = 15 * 60 * 1000;

export function startAutoRefresh() {
    setInterval(loadWeatherData, REFRESH_INTERVAL_MS);

    // A background tab's timers are throttled and a sleeping phone's do not run at all, so
    // the interval alone can leave a forecast hours old on screen the moment the tab is
    // looked at again -- which for this app is exactly when it is about to be trusted.
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible' && Date.now() - lastLoadedAt >= REFRESH_INTERVAL_MS) {
            loadWeatherData();
        }
    });
}
