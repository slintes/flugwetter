let temperatureChart;
let cloudChart;
let windChart;
let vfrChart;

// Airport selection state, filled from /api/config on startup.
let appConfig = { airports: [], default_airport: '', openaip_overlay: false };
let currentAirportId = '';

const AIRPORT_STORAGE_KEY = 'flugwetter.airport';

// Initialize charts when page loads
document.addEventListener('DOMContentLoaded', function() {
    applyDensity();
    initializeCharts();

    // The zoom is set once the data is in, by updateCharts -- see initialZoomHours(),
    // which needs the actual labels and the laid-out plot width to choose it.

    // The airport list has to arrive before the first weather request, so the initial
    // load hangs off the config fetch rather than running beside it.
    initAirportPicker().then(loadWeatherData);

    // Set up manual pan/zoom after charts are created
    setTimeout(setupManualPanZoom, 1000);
});

// ---------------------------------------------------------------------------
// Airport selection
// ---------------------------------------------------------------------------

async function initAirportPicker() {
    const select = document.getElementById('airportSelect');
    const mapButton = document.getElementById('mapPickerButton');

    try {
        const response = await fetch('/api/config');
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        appConfig = await response.json();
    } catch (error) {
        // Without a config there is nothing to select between. The charts still load the
        // backend's default airport, so the dashboard degrades to its pre-selector state.
        console.error('Error loading config:', error);
        select.disabled = true;
        mapButton.disabled = true;
        return;
    }

    // The list arrives in display order (pinned first, then north to south); it is kept
    // as-is so the ordering rule lives in exactly one place.
    select.innerHTML = '';
    appConfig.airports.forEach(airport => {
        const option = document.createElement('option');
        option.value = airport.identifier;
        option.textContent = airportLabel(airport);
        select.appendChild(option);
    });

    currentAirportId = initialAirportId();
    select.value = currentAirportId;
    updateAirportHeading();

    select.addEventListener('change', () => selectAirport(select.value));
    mapButton.addEventListener('click', openAirportMap);
    document.getElementById('mapModalClose').addEventListener('click', closeAirportMap);
    document.getElementById('airportMapModal').addEventListener('click', event => {
        // Click on the backdrop itself, not on the dialog inside it.
        if (event.target.id === 'airportMapModal') {
            closeAirportMap();
        }
    });
    document.addEventListener('keydown', event => {
        if (event.key === 'Escape') {
            closeAirportMap();
        }
    });
}

function airportLabel(airport) {
    const runways = airport.runways && airport.runways.length > 0
        ? ` (${airport.runways.join(', ')})`
        : '';
    return `${airport.identifier} — ${airport.name}${runways}`;
}

function findAirport(identifier) {
    return appConfig.airports.find(airport => airport.identifier === identifier);
}

// initialAirportId resolves the airport to show first: an explicit ?airport= wins so links
// are shareable, then the last choice made in this browser, then the backend default.
// An identifier that is no longer configured falls back instead of erroring.
function initialAirportId() {
    const fromUrl = new URLSearchParams(window.location.search).get('airport');
    if (fromUrl && findAirport(fromUrl)) {
        return fromUrl;
    }

    let stored = null;
    try {
        stored = localStorage.getItem(AIRPORT_STORAGE_KEY);
    } catch (error) {
        // Private mode / disabled storage. Not worth failing over.
    }
    if (stored && findAirport(stored)) {
        return stored;
    }

    if (findAirport(appConfig.default_airport)) {
        return appConfig.default_airport;
    }
    return appConfig.airports.length > 0 ? appConfig.airports[0].identifier : '';
}

function selectAirport(identifier) {
    if (!identifier || identifier === currentAirportId || !findAirport(identifier)) {
        return;
    }

    currentAirportId = identifier;

    const select = document.getElementById('airportSelect');
    if (select.value !== identifier) {
        select.value = identifier;
    }

    try {
        localStorage.setItem(AIRPORT_STORAGE_KEY, identifier);
    } catch (error) {
        // See initialAirportId: storage being unavailable is not fatal.
    }

    // Keep the URL shareable without adding a history entry per click.
    const url = new URL(window.location.href);
    url.searchParams.set('airport', identifier);
    window.history.replaceState({}, '', url);

    updateAirportHeading();
    updateMapSelection();
    loadWeatherData();
}

// The heading itself stays plain "Flugwetter" -- the dropdown next to it already names the
// airport, so repeating it there only cost a line. The tab title carries the full
// identifier and name, because that is the only place the airport is visible when the page
// sits in a background tab, and tabs truncate from the right.
function updateAirportHeading() {
    const airport = findAirport(currentAirportId);
    document.title = airport
        ? `Flugwetter ${airport.identifier} — ${airport.name}`
        : 'Flugwetter - Aviation Weather Forecast';
}

// ---------------------------------------------------------------------------
// Map picker
// ---------------------------------------------------------------------------

let airportMap = null;
let airportMarkers = {};

// Bootstrap view, only used until fitMapToAirports frames the real airports. Roughly
// north-west Germany.
const MAP_FALLBACK_VIEW = { center: [52.7, 7.5], zoom: 7 };

function openAirportMap() {
    const modal = document.getElementById('airportMapModal');
    modal.hidden = false;
    initAirportMap();
    // Leaflet measures the container on creation, and the container had no size while the
    // modal was hidden. Both the first open and every later one need the recalculation.
    setTimeout(() => {
        airportMap.invalidateSize();
        fitMapToAirports();
    }, 0);
}

function closeAirportMap() {
    // A popup left open would still be there on the next open, since the map instance is
    // reused rather than rebuilt.
    if (airportMap) {
        airportMap.closePopup();
    }
    document.getElementById('airportMapModal').hidden = true;
}

// The dialog is sized in CSS and follows the window, but Leaflet caches the pixel size of
// its container: without this the tile grid keeps the dimensions it had when the modal was
// opened, leaving grey gaps on the way out and clipped tiles on the way in. The view itself
// is left alone -- refitting would undo whatever the user panned to.
window.addEventListener('resize', () => {
    // Zooming the browser changes devicePixelRatio and fires resize.
    applyDensity();
    if (airportMap && !document.getElementById('airportMapModal').hidden) {
        airportMap.invalidateSize();
        // Re-applies the marker styles, which carry the viewport-dependent radius.
        updateMapSelection();
    }
});

function initAirportMap() {
    if (airportMap) {
        return;
    }

    // The view has to be set here, not later: Leaflet resolves a layer's tile coordinates
    // against the map's pixel origin the moment it is added, and that origin does not exist
    // until the map has a center and zoom. Creating the map bare and calling fitBounds
    // afterwards throws inside the first addTo(), leaving the modal on a blank grey box.
    airportMap = L.map('airportMap', {
        scrollWheelZoom: true,
        // Leaflet's default is one whole zoom level per 60px of wheel delta, and a single
        // notch on a normal mouse already delivers far more than that, so one flick jumped
        // several levels. 200px per level plus quarter-level snapping turns the wheel into
        // a gradual zoom. The +/- buttons keep their full-level step (zoomDelta default).
        wheelPxPerZoomLevel: 200,
        zoomSnap: 0.25,
        center: MAP_FALLBACK_VIEW.center,
        zoom: MAP_FALLBACK_VIEW.zoom
    });

    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
        maxZoom: 14,
        attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>'
    }).addTo(airportMap);

    const note = document.getElementById('mapOverlayNote');
    if (appConfig.openaip_overlay) {
        // Proxied through the backend so the openAIP API key stays server-side.
        L.tileLayer('/api/tiles/openaip/{z}/{x}/{y}.png', {
            maxZoom: 14,
            attribution: 'Aeronautical data &copy; <a href="https://www.openaip.net">openAIP</a> (CC BY-NC 4.0)'
        }).addTo(airportMap);
        note.textContent = '';
    } else {
        note.textContent = 'openAIP overlay unavailable — set OPENAIP_API_KEY on the server to show airspaces.';
    }

    appConfig.airports.forEach(airport => {
        const marker = L.circleMarker([airport.latitude, airport.longitude], markerStyle(false))
            .addTo(airportMap)
            .bindTooltip(airport.identifier, {
                permanent: true,
                direction: 'right',
                className: 'airport-tooltip',
                offset: [tooltipOffsetX(), 0]
            });

        // Details on hover, not on click. bindPopup() would open on click -- the same click
        // that selects the airport and closes the modal -- so the popup flashed on the way
        // out and was still open the next time the map was shown. Opening it by hand keeps
        // click free for selection, and openOn() closes any previously open popup.
        marker.on('mouseover', () => {
            L.popup({ closeButton: false, autoPan: false, offset: [0, -4] })
                .setLatLng([airport.latitude, airport.longitude])
                .setContent(airportPopup(airport))
                .openOn(airportMap);
        });
        marker.on('mouseout', () => airportMap.closePopup());

        marker.on('click', () => {
            selectAirport(airport.identifier);
            closeAirportMap();
        });

        airportMarkers[airport.identifier] = marker;
    });

    updateMapSelection();
}

// Marker radii. Leaflet takes these in CSS pixels, so without a step here a 2560px screen
// gets the same 7px dot as a 1280px one and it reads as undersized.
const MARKER_RADII = { normal: 7, selected: 10 };
const MARKER_RADII_WIDE = { normal: 9, selected: 13 };
// Multiples of 4 so the diameter stays a whole number of physical pixels at dpr 0.75.
const MARKER_RADII_LOW_DENSITY = { normal: 12, selected: 16 };

function markerRadii() {
    if (isLowDensity()) {
        return MARKER_RADII_LOW_DENSITY;
    }
    return isWideViewport() ? MARKER_RADII_WIDE : MARKER_RADII;
}

// Leaflet anchors a path's tooltip at the marker's centre, not its edge, so the label has
// to be pushed clear of the circle by hand — otherwise a larger radius grows out over it.
// The offset uses the *selected* radius for every marker, which is the largest a marker can
// get, so the labels stay in a straight line as the selection moves between them.
function tooltipOffsetX() {
    return markerRadii().selected + 6;
}

function markerStyle(selected) {
    const radii = markerRadii();
    return {
        radius: selected ? radii.selected : radii.normal,
        color: '#ffffff',
        weight: 2,
        fillColor: selected ? '#d63031' : '#0984e3',
        fillOpacity: 1
    };
}

function airportPopup(airport) {
    const runways = airport.runways && airport.runways.length > 0
        ? `<br>Runway ${airport.runways.join(', ')}`
        : '';
    return `<strong>${airport.identifier}</strong><br>${airport.name}${runways}`;
}

function updateMapSelection() {
    const offset = tooltipOffsetX();
    Object.keys(airportMarkers).forEach(identifier => {
        const marker = airportMarkers[identifier];
        marker.setStyle(markerStyle(identifier === currentAirportId));

        // The offset is fixed at bind time, so a density or viewport change that alters the
        // radius would otherwise leave the label sitting on top of the circle.
        const tooltip = marker.getTooltip();
        if (tooltip && tooltip.options.offset[0] !== offset) {
            tooltip.options.offset = L.point(offset, 0);
            marker.closeTooltip();
            marker.openTooltip();
        }
    });
}

// fitMapToAirports frames whatever airports were configured, so changing the list moves the
// map without a hardcoded viewport here.
function fitMapToAirports() {
    const points = appConfig.airports.map(airport => [airport.latitude, airport.longitude]);
    if (points.length === 0) {
        airportMap.setView(MAP_FALLBACK_VIEW.center, MAP_FALLBACK_VIEW.zoom);
        return;
    }
    airportMap.fitBounds(L.latLngBounds(points), { padding: [40, 40] });
}

// All four charts share one time axis, so their plot areas have to start and end at the
// same x. Left to itself, Chart.js sizes each y axis to its own tick labels ("35" vs
// "10000ft"), which pushed the four plot areas 59px apart on the left and 72px on the
// right — the same hour sat at a different pixel in every chart. Pinning both axis widths
// to a constant is what keeps them aligned; the values are the widest the axes naturally
// wanted, so no label is clipped. A new chart must use the same two constants, and the
// VFR chart, which has no visible axis at all, pads by them instead.
const AXIS_WIDTHS_WIDE = { left: 82, right: 81 };
// On a phone 82+81 is 163px of a 360px screen — 44% of it, taken from every chart. These
// clear the widest tick label ("10000ft", "30 mm") but not the rotated axis title as well,
// which is why the titles are hidden at this size (see the responsiveAxes plugin).
const AXIS_WIDTHS_NARROW = { left: 54, right: 52 };

// Phones report 360-430 CSS px in portrait, so 600 puts the switch clear of that band.
const NARROW_VIEWPORT = 600;
// Above this the map's markers and labels, which are fixed pixel sizes, start to look
// undersized against the rest of the page. Kept in step with the media query for
// .airport-tooltip in styles.css.
const WIDE_VIEWPORT = 1600;

function isNarrowViewport() {
    return window.innerWidth <= NARROW_VIEWPORT;
}

function isWideViewport() {
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
function isLowDensity() {
    return window.devicePixelRatio < 1;
}

// applyDensity exposes the above to CSS. devicePixelRatio changes when the user zooms, and
// a zoom fires resize, so this is re-run from the same listener as the map sizing.
function applyDensity() {
    document.documentElement.dataset.density = isLowDensity() ? 'low' : 'normal';
}

function axisWidths() {
    return isNarrowViewport() ? AXIS_WIDTHS_NARROW : AXIS_WIDTHS_WIDE;
}

// afterFit runs on every layout, so crossing the breakpoint re-pins all four charts with
// no resize handler of our own. Both sides must come from the same source or the charts
// drift apart again.
const pinAxisWidth = side => scale => { scale.width = axisWidths()[side]; };

// The VFR chart's weather icons and probability labels, which are drawn by the vfrText
// plugin rather than by Chart.js. At full size one hour needs 56px, and a phone's plot
// area cannot give that to even one hour, so they shrink with the axes.
const VFR_METRICS_WIDE = { icon: 36, font: '24px Narrow', unknownFont: '20px Narrow' };
const VFR_METRICS_NARROW = { icon: 20, font: '14px Narrow', unknownFont: '12px Narrow' };

function vfrMetrics() {
    return isNarrowViewport() ? VFR_METRICS_NARROW : VFR_METRICS_WIDE;
}

// responsiveAxes runs before every layout, which is what makes a resize across the
// breakpoint take effect without a resize handler of our own.
//
// Two jobs: reserve the axis space on the VFR chart, which has no visible axis of its own
// and would otherwise run wider than the other three; and drop the rotated axis titles on
// a phone, because "Height (feet) - Log Scale" plus a "10000ft" tick does not fit in 54px
// and the tick labels are the half that cannot be inferred from the chart heading.
Chart.register({
    id: 'responsiveAxes',
    beforeLayout: function(chart) {
        const widths = axisWidths();

        if (chart.canvas.id === 'vfrChart') {
            chart.options.layout.padding.left = widths.left;
            chart.options.layout.padding.right = widths.right;
            return;
        }

        const showTitles = !isNarrowViewport();
        Object.entries(chart.options.scales || {}).forEach(([id, scale]) => {
            if (id !== 'x' && scale.title) {
                scale.title.display = showTitles;
            }
        });
    }
});

// Initial zoom. A fixed 72h looked fine on a wide monitor and jammed the VFR chart's
// weather icons and probability labels into each other on anything narrower, so the range
// is derived from how much room one hour actually gets. Applied once, on first load; after
// that the range belongs to the user and is left alone across airport switches and the
// 15-minute refresh.
const VFR_SLOT_GAP = 4;      // so neighbours are visibly apart, not merely touching
// 3 rather than 6: on a phone the plot cannot fit 6 hours without the icons colliding, and
// a readable 3-hour view that pans beats an unreadable 6-hour one.
const INITIAL_ZOOM_MIN_HOURS = 3;
const INITIAL_ZOOM_MAX_HOURS = 72;

let initialZoomApplied = false;

// vfrSlotWidth returns the horizontal room one hour needs before the icon or the label
// starts touching its neighbour. The labels are measured rather than assumed, because
// their width swings a lot: "0" against "100?".
function vfrSlotWidth() {
    const metrics = vfrMetrics();
    const ctx = vfrChart.ctx;
    ctx.save();
    ctx.font = metrics.font;
    let widestLabel = 0;
    (vfrChart.data.datasets[0].data || []).forEach(point => {
        let label;
        if (point.probability < 0) {
            label = '–';
        } else if (point.visibilityKnown === false) {
            label = `${point.probability}?`;
        } else {
            label = `${point.probability}`;
        }
        widestLabel = Math.max(widestLabel, ctx.measureText(label).width);
    });
    ctx.restore();
    return Math.max(metrics.icon, widestLabel) + VFR_SLOT_GAP;
}

function initialZoomHours() {
    const area = vfrChart.chartArea;
    const widths = axisWidths();
    const plotWidth = area && area.right > area.left
        ? area.right - area.left
        : vfrChart.canvas.clientWidth - widths.left - widths.right;

    // resetChartZoom puts 3 hours of history on screen on top of the range asked for, so
    // those slots have to come out of the budget.
    const hours = Math.floor(plotWidth / vfrSlotWidth()) - 3;
    return Math.min(INITIAL_ZOOM_MAX_HOURS, Math.max(INITIAL_ZOOM_MIN_HOURS, hours));
}

function initializeCharts() {
    // VFR Chart
    const vfrCtx = document.getElementById('vfrChart').getContext('2d');

    const xAxisConfig = function(drawOnChartArea){
        return {
            type: 'time',
            // No `parser`: every x value is already an epoch-millisecond number. The
            // previous 'YYYY-MM-DDTHH:mm' was a moment.js pattern, and under the
            // date-fns adapter YYYY/DD mean week-numbering year and day-of-year, which
            // that library throws on. `distribution` was a Chart.js v2 option.
            time: {
                unit: 'hour',
                stepSize: 1,
                displayFormats: {
                    hour: 'MMM dd HH:mm'
                }
            },
            grid: {
                drawOnChartArea: drawOnChartArea,
                z: -1,
                color: 'rgba(0,0,0,0.3)',
                offset: false
            },
            title: {
                display: false
            },
            ticks: {
                callback: function(value, index, values) {
                    const date = new Date(value);
                    // Show tick if it's at 0, 3, 6, 9, 12, 15, 18, 21 hours
                    if (date.getHours() % 3 === 0) {
                        if (date.getHours() === 0) {
                            // Midnight labels are drawn by the midnightDateLabels plugin
                            // Return empty string to reserve tick space but skip default rendering
                            return '';
                        } else {
                            // For other hours, show time only
                            return date.toLocaleTimeString('en-US', {
                                hour: '2-digit',
                                minute: '2-digit',
                                hour12: false
                            });
                        }
                    }
                    return '';
                },
                maxRotation: 45,
                autoSkip: false,
                padding: 8,
            },
        }
    }

    // Cache for weather icons
    const weatherIconCache = {};
    
    // Function to preload weather icons
    function preloadWeatherIcons() {
        // Get all unique icon filenames from the weatherCodeToIcon mapping
        const iconFilenames = new Set(Object.values(weatherCodeToIcon));
        
        // Preload each icon
        iconFilenames.forEach(iconFilename => {
            const img = new Image();
            img.src = `static/icons/${iconFilename}`;
            weatherIconCache[iconFilename] = img;
        });
    }
    
    // Preload icons when the page loads
    preloadWeatherIcons();
    
    // Custom interaction mode: correlate datasets by TIME, not by array index.
    //
    // Chart.js' built-in 'index' mode takes the nearest element's array index and reads
    // that same index out of every other dataset. That only works when all datasets are
    // the same length. On the cloud and wind charts they are not: 'Cloud Layers' and
    // 'Wind Layers' hold one point per pressure level per hour (~6/hour for wind), while
    // the line series hold one point per hour. Hovering hour H would read index 6H from
    // the line series — a different hour, or past the end of the array, in which case
    // those rows vanished from the tooltip entirely.
    //
    // Matching on the x value is exact: every series derives x from the same
    // `new Date(point.time + 'Z').getTime()` conversion. Within each dataset the
    // y-closest candidate wins, so the tooltip still shows one row per dataset.
    Chart.Interaction.modes.timeNearest = function(chart, e, options, useFinalPosition) {
        const nearest = Chart.Interaction.modes.nearest(
            chart, e, {...options, axis: 'x', intersect: false}, useFinalPosition);
        if (!nearest.length) return [];

        const seed = chart.data.datasets[nearest[0].datasetIndex].data[nearest[0].index];
        if (!seed) return [];
        const targetX = seed.x;

        const position = Chart.helpers.getRelativePosition(e, chart);
        const items = [];

        chart.getSortedVisibleDatasetMetas().forEach(meta => {
            const data = chart.data.datasets[meta.index].data;
            let best = null;
            let bestDy = Infinity;

            for (let i = 0; i < data.length; i++) {
                if (!data[i] || data[i].x !== targetX) continue;
                const element = meta.data[i];
                if (!element || element.skip) continue;

                // Non-finite y (e.g. a log scale fed a zero) must never win the compare.
                const dy = Math.abs(element.y - position.y);
                if (dy < bestDy) {
                    bestDy = dy;
                    best = {element, datasetIndex: meta.index, index: i};
                }
            }

            if (best) items.push(best);
        });

        return items;
    };

    // Custom plugin to render midnight date labels (day-of-week + date, larger + bold; time smaller)
    Chart.register({
        id: 'midnightDateLabels',
        afterDraw(chart) {
            const xAxis = chart.scales.x;
            if (!xAxis) return;
            const ctx = chart.ctx;
            const ticks = xAxis.ticks;
            if (!ticks) return;

            const dateFontSize = 14;
            const timeFontSize = 11;
            const lineSpacing = 3;

            ticks.forEach((tick) => {
                const date = new Date(tick.value);
                if (date.getHours() !== 0 || date.getMinutes() !== 0) return;

                const x = xAxis.getPixelForValue(tick.value);
                const yStart = xAxis.top + 6;

                const dateLine = date.toLocaleDateString('en-US', {
                    weekday: 'short',
                    month: 'short',
                    day: 'numeric'
                });
                const timeLine = date.toLocaleTimeString('en-US', {
                    hour: '2-digit',
                    minute: '2-digit',
                    hour12: false
                });

                ctx.save();
                ctx.textAlign = 'center';
                ctx.textBaseline = 'top';

                // Draw date line (larger, bold)
                ctx.fillStyle = '#333';
                ctx.font = `bold ${dateFontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif`;
                ctx.fillText(dateLine, x, yStart);

                // Draw time line (smaller)
                ctx.font = `${timeFontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif`;
                ctx.fillText(timeLine, x, yStart + dateFontSize + lineSpacing);

                ctx.restore();
            });
        }
    });

    // Custom plugin to draw vfr text with color coding and weather icons
    Chart.register({
        id: 'vfrText',
        afterDatasetsDraw: function(chart) {
            if (chart.canvas.id === 'vfrChart') {
                const metrics = vfrMetrics();
                const ctx = chart.ctx;
                const chartArea = chart.chartArea;
                
                // Save the current context state
                ctx.save();
                
                // Clip to chart area to prevent drawing outside
                ctx.beginPath();
                ctx.rect(chartArea.left, chartArea.top, chartArea.right - chartArea.left, chartArea.bottom - chartArea.top);
                ctx.clip();
                
                chart.data.datasets.forEach((dataset, datasetIndex) => {
                    if (dataset.label === 'VFR Probability') {
                        dataset.data.forEach((point, index) => {
                            if (point && point.probability !== undefined) {
                                
                                const xPos = chart.scales.x.getPixelForValue(point.x);
                                const yPos = chartArea.top + (chartArea.bottom - chartArea.top) * 0.65;
                                
                                // Skip if outside visible area
                                if (xPos < chartArea.left || xPos > chartArea.right) {
                                    return;
                                }
                                
                                const probability = point.probability;

                                // Three presentations, in order of precedence:
                                //   < 0            no score at all -> grey dash
                                //   visibility     unknown (model dropped it, typically
                                //                  the forecast tail) -> grey "NN?", so an
                                //                  estimate never reads as a hard number
                                //   otherwise      the normal colour ladder
                                let label;
                                if (probability < 0) {
                                    label = '–';
                                    ctx.fillStyle = '#999';
                                } else if (point.visibilityKnown === false) {
                                    label = `${probability}?`;
                                    ctx.fillStyle = '#888';
                                } else {
                                    label = `${probability}`;
                                    if (probability >= 90) {
                                        ctx.fillStyle = 'darkgreen';
                                    } else if (probability >= 60) {
                                        ctx.fillStyle = 'green';
                                    } else if (probability >= 30) {
                                        ctx.fillStyle = 'orange';
                                    } else {
                                        ctx.fillStyle = 'red';
                                    }
                                }

                                // Set text properties
                                ctx.font = metrics.font;
                                ctx.textAlign = 'center';
                                ctx.textBaseline = 'middle';

                                // Draw the text
                                ctx.fillText(label, xPos, yPos);
                                
                                // Draw weather icon if available
                                if (point.weatherCode !== undefined) {
                                    const weatherCode = point.weatherCode;
                                    const iconFilename = weatherCodeToIcon[weatherCode];
                                    const maxIconSize = metrics.icon;
                                    const iconY = yPos - metrics.icon; // Position above the text

                                    function drawIcon(img, x, y) {
                                        // Icons carry only a viewBox and are not square
                                        // (cloudy is 84x44), so preserve aspect ratio.
                                        // The fallback covers browsers reporting 0.
                                        const natW = img.naturalWidth || maxIconSize;
                                        const natH = img.naturalHeight || maxIconSize;
                                        const scale = Math.min(maxIconSize / natW, maxIconSize / natH);
                                        const w = natW * scale;
                                        const h = natH * scale;
                                        ctx.drawImage(img, x - w/2, y - h/2, w, h);
                                    }

                                    if (!iconFilename) {
                                        // Unknown code. Draw a glyph rather than pointing
                                        // at a placeholder file that does not exist.
                                        ctx.fillStyle = '#999';
                                        ctx.font = metrics.unknownFont;
                                        ctx.fillText('?', xPos, iconY);
                                    } else {
                                        let img = weatherIconCache[iconFilename];
                                        if (!img) {
                                            img = new Image();
                                            img.src = `static/icons/${iconFilename}`;
                                            weatherIconCache[iconFilename] = img;
                                        }

                                        if (img.complete && img.naturalWidth) {
                                            drawIcon(img, xPos, iconY);
                                        } else if (!img.redrawHooked) {
                                            // preloadWeatherIcons() puts every icon in the
                                            // cache up front, so the old "not cached yet"
                                            // branch could never fire and a still-decoding
                                            // icon drew nothing and never retried. Ask the
                                            // chart to redraw instead of painting directly:
                                            // a direct draw would land outside the render
                                            // pass, unclipped, at coordinates captured
                                            // before any pan.
                                            img.redrawHooked = true;
                                            img.addEventListener('load', () => chart.draw(), {once: true});
                                        }
                                    }
                                }
                            }
                        })
                    }
                })
                
                // Restore the context state
                ctx.restore();
            }
        }
    });

    vfrChart = new Chart(vfrCtx, {
        type: 'line',
        data: {
            datasets: [{
                label: 'VFR Probability',
                data: [],
                borderColor: 'transparent',
                backgroundColor: 'transparent',
                pointRadius: 0,
                pointHoverRadius: 0
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            interaction: {
                intersect: false,
                mode: 'index'
            },
            events: ['mousedown', 'mousemove', 'mouseup', 'click', 'mouseover', 'mouseout', 'wheel'],
            // No visible y axis here, so the space the other charts give their axes has to
            // come from padding instead, or this chart's time axis would run wider than
            // theirs.
            layout: {
                // Kept in step with the other charts' axes by the vfrAxisPadding plugin,
                // which rewrites these before every layout.
                padding: { left: AXIS_WIDTHS_WIDE.left, right: AXIS_WIDTHS_WIDE.right }
            },
            scales: {
                y: {
                    display: false, // No y-axis as per requirements
                    min: 0,
                    max: 1
                },
                x: xAxisConfig(false),
            },
            plugins: {
                legend: {
                    display: false // No legend needed
                },
                tooltip: {
                    callbacks: {
                        title: function(context) {
                            // Display time in local timezone with date and time
                            return new Date(context[0].parsed.x).toLocaleString();
                        },
                        label: function(context) {
                            const point = context.raw;
                            return ``;
                        }
                    }
                }
            }
        }
    });
    
    // Temperature Chart
    const tempCtx = document.getElementById('temperatureChart').getContext('2d');
    temperatureChart = new Chart(tempCtx, {
        type: 'line',
        data: {
            datasets: [{
                label: '2m Temperature (°C)',
                data: [],
                borderColor: '#e17055',
                backgroundColor: 'rgba(225, 112, 85, 0.1)',
                borderWidth: 3,
                fill: false,
                tension: 0.4,
                yAxisID: 'y'
            }, {
                label: '2m Dew Point (°C)',
                data: [],
                borderColor: '#00b894',
                backgroundColor: 'rgba(0, 184, 148, 0.1)',
                borderWidth: 3,
                fill: false,
                tension: 0.4,
                yAxisID: 'y'
            }, {
                label: 'Precipitation (mm)',
                data: [],
                type: 'bar',
                backgroundColor: function(ctx) {
                    if (ctx.raw && ctx.raw.precipitationProbability !== undefined) {
                        const probability = ctx.raw.precipitationProbability / 100;
                        const alpha = Math.max(0.2, probability);
                        return `rgba(9, 132, 227, ${alpha})`;
                    }
                    return 'rgba(9, 132, 227, 0.5)';
                },
                borderColor: '#0984e3',
                borderWidth: 1,
                yAxisID: 'y1',
                barPercentage: 0.8,
                categoryPercentage: 1.0
            }, {
                label: 'Precipitation Probability (%)',
                data: [],
                borderColor: 'transparent',
                backgroundColor: 'transparent',
                borderWidth: 0,
                fill: false,
                tension: 0.4,
                yAxisID: 'y2',
                pointRadius: 0,
                pointHoverRadius: 0
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            interaction: {
                intersect: false,
                mode: 'index'
            },
            events: ['mousedown', 'mousemove', 'mouseup', 'click', 'mouseover', 'mouseout', 'wheel'],
            scales: {
                y: {
                    type: 'linear',
                    display: true,
                    position: 'left',
                    beginAtZero: false,
                    afterFit: pinAxisWidth('left'),
                    grid: {
                        color: 'rgba(0,0,0,0.1)'
                    },
                    title: {
                        display: true,
                        text: 'Temperature (°C)',
                        font: { size: 14, weight: 'bold' }
                    }
                },
                y1: {
                    type: 'logarithmic',
                    display: true,
                    position: 'right',
                    beginAtZero: true,
                    min: 0.09,
                    max: 30,
                    afterFit: pinAxisWidth('right'),
                    grid: {
                        drawOnChartArea: false,
                    },
                    title: {
                        display: true,
                        text: 'Precipitation (mm)',
                        font: { size: 14, weight: 'bold' }
                    },
                    ticks: {
                        callback: function(value) {
                            return value + ' mm';
                        }
                    }
                },
                y2: {
                    type: 'linear',
                    display: false, // Invisible scale
                    position: 'right',
                    min: 0,
                    max: 100, // Scale from 0 to 100%
                    grid: {
                        drawOnChartArea: false,
                    }
                },
                x: xAxisConfig(true),
            },
            plugins: {
                legend: {
                    display: true,
                    position: 'top',
                    maxHeight: 50,
                    fullSize: false,
                    labels: {
                        boxWidth: 15,
                        padding: 10
                    }
                },
                tooltip: {
                    callbacks: {
                        title: function(context) {
                            // Display time in local timezone with date and time
                            return new Date(context[0].parsed.x).toLocaleString();
                        },
                        label: function(context) {
                            const point = context.raw;
                            if (context.dataset.label === '2m Temperature (°C)') {
                                return `Temperature: ${point.y.toFixed(1)}°C`;
                            } else if (context.dataset.label === '2m Dew Point (°C)') {
                                return `Dew Point: ${point.y.toFixed(1)}°C`;
                            } else if (context.dataset.label === 'Precipitation (mm)') {
                                let result = `Precipitation: ${point.y.toFixed(1)} mm`;
                                if (point.precipitationProbability !== undefined) {
                                    result += `, Probability: ${point.precipitationProbability}%`;
                                }
                                return result;
                            } else if (context.dataset.label === 'Precipitation Probability (%)') {
                                return `Probability: ${point.y}%`;
                            }
                            return '';
                        }
                    }
                }
            }
        }
    });

    // Cloud Cover Chart - Scatter plot with height vs time
    const cloudCtx = document.getElementById('cloudChart').getContext('2d');
    
    // Register custom plugins for rendering clouds and cloud base
    Chart.register({
        id: 'cloudSymbols',
        afterDatasetsDraw: function(chart) {
            const ctx = chart.ctx;
            const chartArea = chart.chartArea;
            
            // Save the current context state
            ctx.save();
            
            // Clip to chart area to prevent drawing outside
            ctx.beginPath();
            ctx.rect(chartArea.left, chartArea.top, chartArea.right - chartArea.left, chartArea.bottom - chartArea.top);
            ctx.clip();
            
            chart.data.datasets.forEach((dataset, datasetIndex) => {
                if (dataset.label === 'Cloud Layers') {
                    dataset.data.forEach((point, index) => {
                        if (point && point.coverage !== undefined) {
                            const meta = chart.getDatasetMeta(datasetIndex);
                            const element = meta.data[index];
                            if (element && !element.skip && 
                                element.x >= chartArea.left && element.x <= chartArea.right &&
                                element.y >= chartArea.top && element.y <= chartArea.bottom) {
                                
                                // Calculate transparency based on coverage (0% = transparent, 100% = opaque)
                                const alpha = 0.1 + (point.coverage / 100 * 0.8);
                                
                                // Calculate base size for the cloud (slightly larger for a nicer appearance)
                                const baseSize = 7;
                                
                                // Draw a more realistic cloud shape with transparency based on coverage
                                const x = element.x;
                                const y = element.y;
                                
                                // Add a subtle white glow/border effect for depth
                                ctx.beginPath();
                                
                                // Start at the bottom-left of the cloud
                                ctx.moveTo(x - baseSize * 1.1, y + baseSize * 0.2);
                                
                                // Bottom curve
                                ctx.bezierCurveTo(
                                    x - baseSize * 0.8, y + baseSize * 0.5,
                                    x - baseSize * 0.3, y + baseSize * 0.6,
                                    x, y + baseSize * 0.4
                                );
                                
                                // Bottom-right curve
                                ctx.bezierCurveTo(
                                    x + baseSize * 0.4, y + baseSize * 0.6,
                                    x + baseSize * 0.9, y + baseSize * 0.5,
                                    x + baseSize * 1.1, y + baseSize * 0.2
                                );
                                
                                // Right side curve
                                ctx.bezierCurveTo(
                                    x + baseSize * 1.3, y,
                                    x + baseSize * 1.2, y - baseSize * 0.4,
                                    x + baseSize * 0.9, y - baseSize * 0.6
                                );
                                
                                // Top-right bump
                                ctx.bezierCurveTo(
                                    x + baseSize * 0.7, y - baseSize * 1.0,
                                    x + baseSize * 0.4, y - baseSize * 1.2,
                                    x + baseSize * 0.1, y - baseSize * 0.9
                                );
                                
                                // Top-middle bump
                                ctx.bezierCurveTo(
                                    x - baseSize * 0.1, y - baseSize * 1.3,
                                    x - baseSize * 0.4, y - baseSize * 1.2,
                                    x - baseSize * 0.6, y - baseSize * 0.9
                                );
                                
                                // Top-left bump
                                ctx.bezierCurveTo(
                                    x - baseSize * 0.8, y - baseSize * 1.1,
                                    x - baseSize * 1.0, y - baseSize * 0.8,
                                    x - baseSize * 1.2, y - baseSize * 0.5
                                );
                                
                                // Left side curve back to start
                                ctx.bezierCurveTo(
                                    x - baseSize * 1.3, y - baseSize * 0.2,
                                    x - baseSize * 1.3, y,
                                    x - baseSize * 1.1, y + baseSize * 0.2
                                );
                                
                                // Create a subtle white glow/border effect
                                ctx.strokeStyle = `rgba(255, 255, 255, ${alpha * 0.7})`;
                                ctx.lineWidth = 1.5;
                                ctx.stroke();
                                
                                // Fill with the main cloud color
                                ctx.fillStyle = `rgba(0, 0, 0, ${alpha})`;
                                ctx.fill();
                            }
                        }
                    });
                }

                if (dataset.label === 'Cloud Base') {
                    dataset.data.forEach((point, index) => {
                        if (point && point.y !== undefined) {
                            const xPos = chart.scales.x.getPixelForValue(point.x);
                            // const yPos = chartArea.top + (chartArea.bottom - chartArea.top) * 0.75;
                            const yPos = chartArea.bottom - 8;

                            // Skip if outside visible area
                            if (xPos < chartArea.left || xPos > chartArea.right) {
                                return;
                            }

                            // Set text style
                            ctx.font = '20px Narrow';
                            ctx.textAlign = 'center';
                            ctx.textBaseline = 'middle';

                            // Color coding based on height
                            let textColor;
                            if (point.y < 10) {
                                textColor = 'rgb(255,0,0)'; // Red for < 1000ft
                            } else if (point.y < 15) {
                                textColor = 'rgb(255,66,0)'; // Orange for 1000-2000ft
                            } else if (point.y < 20) {
                                textColor = 'rgb(216,137,18)'; // Orange for 1000-2000ft
                            } else if (point.y < 25) {
                                textColor = 'rgb(144,182,25)'; // Light green for 2000-3000ft
                            } else if (point.y < 30) {
                                textColor = 'rgba(105,221,28,0.9)'; // Light green for 2000-3000ft
                            } else {
                                textColor = 'rgba(0, 150, 0, 0.8)'; // Dark green for > 3000ft
                            }

                            ctx.fillStyle = textColor;

                            // Draw the text
                            ctx.fillText(point.y, xPos, yPos);
                        }
                    })
                }

            });
            
            // Restore the context state
            ctx.restore();
        }
    });
    
    // Register a custom plugin for drawing the 2000ft grid line in the cloud chart
    Chart.register({
        id: 'cloudGridLines',
        afterDraw: function(chart) {
            if (chart.canvas.id === 'cloudChart') {
                const ctx = chart.ctx;
                const chartArea = chart.chartArea;
                const yScale = chart.scales.y;
                
                // Draw the 2000ft grid line
                const yPosition = yScale.getPixelForValue(2000);
                
                ctx.save();
                ctx.beginPath();
                ctx.moveTo(chartArea.left, yPosition);
                ctx.lineTo(chartArea.right, yPosition);
                ctx.lineWidth = 3;
                ctx.strokeStyle = 'rgba(0, 150, 0, 0.2)';
                ctx.stroke();
                ctx.restore();
            }
        }
    });
    
    cloudChart = new Chart(cloudCtx, {
        type: 'scatter',
        data: {
            datasets: [{
                label: 'Cloud Layers',
                data: [],
                backgroundColor: 'transparent',
                borderColor: 'transparent',
                pointRadius: 0, // Hide points, we'll show symbols instead
                yAxisID: 'y'
            }, {
                type: 'line',
                label: 'Visibility (km)',
                data: [],
                borderColor: '#e17055',
                backgroundColor: 'rgba(225, 112, 85, 0.1)',
                borderWidth: 3,
                fill: false,
                tension: 0.4,
                yAxisID: 'y1'
            }, {
                label: 'Cloud Base',
                data: [],
                backgroundColor: 'transparent',
                borderColor: 'transparent',
                pointRadius: 0, // Hide points, we'll show symbols instead
                // Flight levels, not feet — must not land on the logarithmic height
                // axis, where FL0 would parse to a non-finite pixel.
                yAxisID: 'yBase'
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            interaction: {
                intersect: false,
                // 'Cloud Layers' holds one point per level per hour, the line series one
                // per hour, so index-based correlation reads the wrong time. See the
                // timeNearest registration above.
                mode: 'timeNearest'
            },
            events: ['mousedown', 'mousemove', 'mouseup', 'click', 'mouseover', 'mouseout', 'wheel'],
            scales: {
                y: {
                    type: 'logarithmic',
                    position: 'left',
                    min: 200, // low-level clouds
                    max: 12000, // Max altitude
                    afterFit: pinAxisWidth('left'),
                    grid: {
                        color: 'rgba(0,0,0,0.1)'
                    },
                    title: {
                        display: true,
                        text: 'Height (feet) - Log Scale',
                        font: { size: 14, weight: 'bold' }
                    },
                    ticks: {
                        callback: function(value) {
                            return value + 'ft';
                        }
                    }
                },
                y1: {
                    type: 'linear',
                    display: true,
                    position: 'right',
                    min: 0,
                    max: 80, // Maximum visibility
                    afterFit: pinAxisWidth('right'),
                    grid: {
                        drawOnChartArea: false, // Don't draw grid lines for second axis
                    },
                    title: {
                        display: true,
                        text: 'Visibility (km)',
                        font: { size: 14, weight: 'bold' }
                    },
                    ticks: {
                        callback: function(value) {
                            return value + ' km';
                        }
                    }
                },
                // Carrier axis for the Cloud Base dataset. It is drawn entirely by the
                // cloudSymbols plugin at a fixed height, so this only needs to give the
                // values a finite, linear home.
                yBase: {
                    type: 'linear',
                    display: false,
                    min: 0,
                    max: 400
                },
                x: xAxisConfig(true),
            },
            plugins: {
                legend: {
                    display: true,
                    position: 'top',
                    maxHeight: 50,
                    fullSize: false,
                    labels: {
                        boxWidth: 15,
                        padding: 10
                    }
                },
                tooltip: {
                    callbacks: {
                        title: function(context) {
                            // Display time in local timezone with date and time
                            return new Date(context[0].parsed.x).toLocaleString();
                        },
                        label: function(context) {
                            const point = context.raw;
                            if (context.dataset.label === 'Cloud Layers') {
                                // Find visibility data at the same time point
                                let visibilityValue = "N/A";
                                
                                // Get the visibility dataset
                                const visibilityDataset = context.chart.data.datasets.find(dataset => 
                                    dataset.label === 'Visibility (km)'
                                );
                                
                                if (visibilityDataset) {
                                    // Find the visibility data point with the same x-value (time)
                                    const visibilityPoint = visibilityDataset.data.find(dataPoint => 
                                        dataPoint.x === point.x
                                    );
                                    
                                    if (visibilityPoint && visibilityPoint.y !== undefined) {
                                        visibilityValue = visibilityPoint.y.toFixed(1) + " km";
                                    }
                                }
                                
                                return `Height: ${point.y}ft, Coverage: ${point.coverage}%, Visibility: ${visibilityValue}`;
                            }
                            return '';
                        }
                    }
                }
            }
        }
    });

    // Function to draw wind barbs
    function drawWindBarb(ctx, x, y, speedKnots, directionDegrees) {
        ctx.save();
        
        // If calm conditions (< 3 knots), draw a circle
        if (speedKnots < 3) {
            ctx.beginPath();
            ctx.arc(x, y, 4, 0, 2 * Math.PI);
            ctx.strokeStyle = '#333';
            ctx.lineWidth = 1.5;
            ctx.stroke();
            ctx.restore();
            return;
        }
        
        // Convert direction to radians (meteorological: direction wind comes FROM)
        // Add 180° to point in direction wind is coming FROM
        const angle = ((directionDegrees + 180) % 360) * Math.PI / 180;
        
        // Move to wind barb position
        ctx.translate(x, y);
        ctx.rotate(angle);
        
        // Set drawing style
        ctx.strokeStyle = '#333';
        ctx.fillStyle = '#333';
        ctx.lineWidth = 1.5;
        ctx.lineCap = 'round';
        
        // Calculate barb components
        const speed = Math.round(speedKnots);
        const pennants = Math.floor(speed / 50);
        const fullBarbs = Math.floor((speed % 50) / 10);
        const halfBarb = Math.floor((speed % 10) / 5);
        
        // Draw main shaft (points in direction wind comes FROM)
        const shaftLength = 25;
        ctx.beginPath();
        ctx.moveTo(0, 0);
        ctx.lineTo(0, -shaftLength);
        ctx.stroke();
        
        // Draw barbs starting from the tail (center) of the shaft
        let currentPos = 0;
        const barbSpacing = 4;
        const barbLength = 8;
        
        // Draw pennants (50 knots each) - at the tail
        for (let i = 0; i < pennants; i++) {
            ctx.beginPath();
            ctx.moveTo(0, currentPos);
            ctx.lineTo(-barbLength, currentPos - barbLength);
            ctx.lineTo(0, currentPos - barbLength);
            ctx.closePath();
            ctx.fill();
            currentPos -= barbSpacing * 2;
        }
        
        // Draw full barbs (10 knots each) - at the tail
        for (let i = 0; i < fullBarbs; i++) {
            ctx.beginPath();
            ctx.moveTo(0, currentPos);
            ctx.lineTo(-barbLength, currentPos - barbLength);
            ctx.stroke();
            currentPos -= barbSpacing;
        }
        
        // Draw half barb (5 knots) - at the tail
        if (halfBarb > 0) {
            ctx.beginPath();
            ctx.moveTo(0, currentPos);
            ctx.lineTo(-barbLength / 2, currentPos - barbLength / 2);
            ctx.stroke();
        }
        
        ctx.restore();
    }

    // Function to convert wind direction degrees to compass name
    function getWindDirectionName(degrees) {
        const directions = ['N', 'NNE', 'NE', 'ENE', 'E', 'ESE', 'SE', 'SSE', 'S', 'SSW', 'SW', 'WSW', 'W', 'WNW', 'NW', 'NNW'];
        const index = Math.round(degrees / 22.5) % 16;
        return directions[index];
    }

    // Wind Chart - Similar to cloud chart but for wind data
    const windCtx = document.getElementById('windChart').getContext('2d');
    
    // Register custom plugins for rendering wind barbs and grid lines
    Chart.register({
        id: 'windBarbs',
        afterDatasetsDraw: function(chart) {
            const ctx = chart.ctx;
            const chartArea = chart.chartArea;
            
            // Save the current context state
            ctx.save();
            
            // Clip to chart area to prevent drawing outside
            ctx.beginPath();
            ctx.rect(chartArea.left, chartArea.top, chartArea.right - chartArea.left, chartArea.bottom - chartArea.top);
            ctx.clip();
            
            chart.data.datasets.forEach((dataset, datasetIndex) => {
                if (dataset.label === 'Wind Layers') {
                    dataset.data.forEach((point, index) => {
                        if (point && point.speed !== undefined && point.direction !== undefined) {
                            const meta = chart.getDatasetMeta(datasetIndex);
                            const element = meta.data[index];
                            if (element && !element.skip && 
                                element.x >= chartArea.left && element.x <= chartArea.right &&
                                element.y >= chartArea.top && element.y <= chartArea.bottom) {
                                
                                drawWindBarb(ctx, element.x, element.y, point.speed, point.direction);
                            }
                        }
                    });
                }
            });
            
            // Restore the context state
            ctx.restore();
        }
    });
    
    // Register a custom plugin for drawing the 10kt grid line in the wind chart
    Chart.register({
        id: 'windGridLines',
        afterDraw: function(chart) {
            if (chart.canvas.id === 'windChart') {
                const ctx = chart.ctx;
                const chartArea = chart.chartArea;
                const y1Scale = chart.scales.y1;
                
                // Draw the 10kt grid line
                const yPosition = y1Scale.getPixelForValue(10);
                
                ctx.save();
                ctx.beginPath();
                ctx.moveTo(chartArea.left, yPosition);
                ctx.lineTo(chartArea.right, yPosition);
                ctx.lineWidth = 3;
                ctx.strokeStyle = 'rgba(0, 150, 0, 0.3)';
                ctx.stroke();
                ctx.restore();
            }
        }
    });
    
    windChart = new Chart(windCtx, {
        type: 'scatter',
        data: {
            datasets: [{
                label: 'Wind Layers',
                data: [],
                backgroundColor: 'transparent',
                borderColor: 'transparent',
                pointRadius: 0, // Hide points, we'll show symbols instead
                yAxisID: 'y'
            }, {
                type: 'line',
                label: 'Wind Speed 10m (kn)',
                data: [],
                borderColor: '#e17055',
                backgroundColor: 'rgba(225, 112, 85, 0.1)',
                borderWidth: 3,
                fill: false,
                tension: 0.4,
                yAxisID: 'y1'
            }, {
                type: 'line',
                label: 'Wind Gusts 10m (kn)',
                data: [],
                borderColor: '#e17055',
                backgroundColor: 'rgba(225, 112, 85, 0.1)',
                borderWidth: 2,
                fill: false,
                tension: 0.4,
                borderDash: [5, 5],
                yAxisID: 'y1'
            }, {
                type: 'line',
                label: 'Crosswind 10m (kn)',
                data: [],
                borderColor: '#ff8c00',
                backgroundColor: 'rgba(255, 140, 0, 0.1)',
                borderWidth: 3,
                fill: false,
                tension: 0.4,
                yAxisID: 'y1'
            }, {
                type: 'line',
                label: 'Crosswind Gusts 10m (kn)',
                data: [],
                borderColor: '#ff8c00',
                backgroundColor: 'rgba(255, 140, 0, 0.1)',
                borderWidth: 2,
                fill: false,
                tension: 0.4,
                borderDash: [5, 5],
                yAxisID: 'y1'
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            interaction: {
                intersect: false,
                // 'Wind Layers' holds ~6 points per hour against 1 per hour for the four
                // line series. See the timeNearest registration above.
                mode: 'timeNearest'
            },
            events: ['mousedown', 'mousemove', 'mouseup', 'click', 'mouseover', 'mouseout', 'wheel'],
            scales: {
                y: {
                    type: 'logarithmic',
                    position: 'left',
                    min: 20,
                    max: 10000, // Max altitude in ft
                    afterFit: pinAxisWidth('left'),
                    grid: {
                        color: 'rgba(0,0,0,0.1)'
                    },
                    title: {
                        display: true,
                        text: 'Height (feet) - Log Scale',
                        font: { size: 14, weight: 'bold' }
                    },
                    ticks: {
                        callback: function(value) {
                            return value + 'ft';
                        }
                    }
                },
                y1: {
                    type: 'linear',
                    position: 'right',
                    min: 0,
                    afterFit: pinAxisWidth('right'),
                    grid: {
                        drawOnChartArea: false, // Don't draw grid lines for second axis
                    },
                    title: {
                        display: true,
                        text: 'Wind Speed (knots)',
                        font: { size: 14, weight: 'bold' }
                    },
                    ticks: {
                        callback: function(value) {
                            return value + ' kn';
                        }
                    }
                },
                x: xAxisConfig(true),
            },
            plugins: {
                legend: {
                    display: true,
                    position: 'top',
                    maxHeight: 50,
                    fullSize: false,
                    labels: {
                        boxWidth: 15,
                        padding: 10
                    }
                },
                tooltip: {
                    callbacks: {
                        title: function(context) {
                            // Display time in local timezone with date and time
                            return new Date(context[0].parsed.x).toLocaleString();
                        },
                        label: function(context) {
                            const point = context.raw;
                            if (context.dataset.label === 'Wind Layers') {
                                return `Height: ${point.y}ft, Speed: ${point.speed.toFixed(1)} kn, Direction: ${point.direction}° (${getWindDirectionName(point.direction)})`;
                            } else if (context.dataset.label === 'Wind Speed 10m (kn)') {
                                return `Wind Speed: ${point.y.toFixed(1)} kn`;
                            } else if (context.dataset.label === 'Wind Gusts 10m (kn)') {
                                return `Wind Gusts: ${point.y.toFixed(1)} kn`;
                            } else if (context.dataset.label === 'Crosswind 10m (kn)') {
                                return `Crosswind: ${point.y.toFixed(1)} kn`;
                            } else if (context.dataset.label === 'Crosswind Gusts 10m (kn)') {
                                return `Crosswind Gusts: ${point.y.toFixed(1)} kn`;
                            }
                            return '';
                        }
                    }
                }
            }
        }
    });
}

async function loadWeatherData() {
    const loadingElement = document.getElementById('loading');
    const errorElement = document.getElementById('error');
    
    try {
        loadingElement.style.display = 'block';
        errorElement.style.display = 'none';
        
        const url = currentAirportId
            ? `/api/weather?airport=${encodeURIComponent(currentAirportId)}`
            : '/api/weather';
        const response = await fetch(url);

        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        
        const data = await response.json();
        
        updateCharts(data);
        loadingElement.style.display = 'none';
        
    } catch (error) {
        console.error('Error loading weather data:', error);
        loadingElement.style.display = 'none';
        errorElement.style.display = 'block';
    }
}

function updateCharts(data) {

    // Update temperature chart with time-based data
    // Convert UTC time strings to local timezone Date objects
    const tempData = data.temperature_data.map(point => ({
        x: new Date(point.time + 'Z').getTime(), // Add 'Z' to indicate UTC
        y: point.temperature
    }));
    
    const dewPointData = data.temperature_data.map(point => ({
        x: new Date(point.time + 'Z').getTime(), // Add 'Z' to indicate UTC
        y: point.dew_point
    }));
    
    const precipitationData = data.temperature_data.map(point => ({
        x: new Date(point.time + 'Z').getTime(), // Add 'Z' to indicate UTC
        y: point.precipitation,
        precipitationProbability: point.precipitation_probability // Include probability for scriptable functions
    }));
    
    const precipitationProbabilityData = data.temperature_data.map(point => ({
        x: new Date(point.time + 'Z').getTime(), // Add 'Z' to indicate UTC
        y: point.precipitation_probability
    }));
    
    temperatureChart.data.datasets[0].data = tempData;
    temperatureChart.data.datasets[1].data = dewPointData;
    temperatureChart.data.datasets[2].data = precipitationData;
    temperatureChart.data.datasets[3].data = precipitationProbabilityData;
    
    temperatureChart.update();
    
    // Update cloud chart with scatter data and visibility data
    const scatterData = [];
    const visibilityData = [];
    const cloudBaseData = [];

    data.cloud_data.forEach(timePoint => {
        // Convert UTC time string to local timezone Date object
        const timeValue = new Date(timePoint.time + 'Z').getTime(); // Add 'Z' to indicate UTC
        
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
    
    cloudChart.data.datasets[0].data = scatterData;
    cloudChart.data.datasets[1].data = visibilityData;
    cloudChart.data.datasets[2].data = cloudBaseData;
    cloudChart.update();

    // Update wind chart with scatter data
    const windScatterData = [];
    const windSpeed10mData = [];
    const windGusts10mData = [];
    const crosswind10mData = [];
    const crosswindGusts10mData = [];

    if (data.wind_data) {
        data.wind_data.forEach(timePoint => {
            // Convert UTC time string to local timezone Date object
            const timeValue = new Date(timePoint.time + 'Z').getTime(); // Add 'Z' to indicate UTC
            
            // Guarded: this was the one unguarded forEach in updateCharts, and a throw
            // here aborts the function before the VFR chart is updated further down.
            if (timePoint.wind_layers) {
                timePoint.wind_layers.forEach(layer => {
                    windScatterData.push({
                        x: timeValue,
                        y: layer.height_feet,
                        speed: layer.speed,
                        direction: layer.direction,
                        symbol: layer.symbol
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

    windChart.data.datasets[0].data = windScatterData;
    windChart.data.datasets[1].data = windSpeed10mData;
    windChart.data.datasets[2].data = windGusts10mData;
    windChart.data.datasets[3].data = crosswind10mData;
    windChart.data.datasets[4].data = crosswindGusts10mData;
    windChart.update();

    const vfrData = [];
    if (data.vfr_data) {
        data.vfr_data.forEach(timePoint => {
            // Convert UTC time string to local timezone Date object
            const timeValue = new Date(timePoint.time + 'Z').getTime(); // Add 'Z' to indicate UTC

            vfrData.push({
                x: timeValue,
                y: timePoint.probability / 100, // Convert to 0-1 range for chart
                probability: timePoint.probability, // Keep original percentage for display
                weatherCode: timePoint.weather_code, // Include weather code for icon display
                visibilityKnown: timePoint.visibility_known // false => score is an estimate
            })
        })
    }
    vfrChart.data.datasets[0].data = vfrData;
    vfrChart.update();

    // Only now are the labels and the laid-out plot width both known.
    if (!initialZoomApplied) {
        initialZoomApplied = true;
        resetZoom(initialZoomHours());
    }
}

// Function to refresh data
function refreshData() {
    loadWeatherData();
}

// Auto-refresh every 15 minutes (900000 ms)
setInterval(refreshData, 900000);

// Function to reset zoom on all charts
function resetZoom(hours) {
    if (vfrChart) {
        resetChartZoom(vfrChart, hours);
    }
    if (temperatureChart) {
        resetChartZoom(temperatureChart, hours);
    }
    if (cloudChart) {
        resetChartZoom(cloudChart, hours);
    }
    if (windChart) {
        resetChartZoom(windChart, hours);
    }
}

function resetChartZoom(chart, hours) {
    const xAxis = chart.scales.x;
    let min;
    let max;
    if ( hours !== undefined ) {
        min = Date.now() - 3 * 60 * 60 * 1000; // start 3 hours ago
        max = Date.now() + hours * 60 * 60 * 1000;
    }
    xAxis.options.min = min;
    xAxis.options.max = max;
    chart.update();
}

// Manual pan/zoom setup function
function setupManualPanZoom() {
    if (vfrChart) {
        addManualPanZoom(vfrChart);
    }
    if (temperatureChart) {
        addManualPanZoom(temperatureChart);
    }
    if (cloudChart) {
        addManualPanZoom(cloudChart);
    }
    if (windChart) {
        addManualPanZoom(windChart);
    }
}

function addManualPanZoom(chart) {
    let isDragging = false;
    let dragStart = null;
    let initialMin = null;
    let initialMax = null;
    
    const canvas = chart.canvas;
    
    canvas.addEventListener('mousedown', function(e) {
        isDragging = true;
        dragStart = e.clientX;
        const xAxis = chart.scales.x;
        initialMin = xAxis.min;
        initialMax = xAxis.max;

    });
    
    canvas.addEventListener('mousemove', function(e) {
        if (!isDragging) return;
        
        const deltaX = e.clientX - dragStart;
        const xAxis = chart.scales.x;
        const chartArea = chart.chartArea;
        const pixelRange = chartArea.right - chartArea.left;
        const timeRange = initialMax - initialMin;
        
        // Convert pixel movement to time movement
        const timeShift = -(deltaX / pixelRange) * timeRange;
        
        xAxis.options.min = initialMin + timeShift;
        xAxis.options.max = initialMax + timeShift;
        
        chart.update('none');

        syncAllCharts(chart, xAxis.options.min, xAxis.options.max);
    });
    
    canvas.addEventListener('mouseup', function(e) {
        if (isDragging) {

        }
        isDragging = false;
    });
    
    canvas.addEventListener('mouseleave', function(e) {
        isDragging = false;
    });
    
    // Touch support for mobile pan
    let touchStartX = null;
    let touchInitialMin = null;
    let touchInitialMax = null;
    let lastPinchDistance = null;

    canvas.addEventListener('touchstart', function(e) {
        if (e.touches.length === 1) {
            touchStartX = e.touches[0].clientX;
            const xAxis = chart.scales.x;
            touchInitialMin = xAxis.min;
            touchInitialMax = xAxis.max;
        } else if (e.touches.length === 2) {
            lastPinchDistance = Math.abs(e.touches[0].clientX - e.touches[1].clientX);
            const xAxis = chart.scales.x;
            touchInitialMin = xAxis.min;
            touchInitialMax = xAxis.max;
        }
    }, {passive: true});

    canvas.addEventListener('touchmove', function(e) {
        if (e.touches.length === 1 && touchStartX !== null) {
            e.preventDefault();
            const deltaX = e.touches[0].clientX - touchStartX;
            const xAxis = chart.scales.x;
            const chartArea = chart.chartArea;
            const pixelRange = chartArea.right - chartArea.left;
            const timeRange = touchInitialMax - touchInitialMin;
            const timeShift = -(deltaX / pixelRange) * timeRange;

            xAxis.options.min = touchInitialMin + timeShift;
            xAxis.options.max = touchInitialMax + timeShift;
            chart.update('none');
            syncAllCharts(chart, xAxis.options.min, xAxis.options.max);
        } else if (e.touches.length === 2 && lastPinchDistance !== null) {
            e.preventDefault();
            const currentDistance = Math.abs(e.touches[0].clientX - e.touches[1].clientX);
            const zoomFactor = lastPinchDistance / currentDistance;
            const xAxis = chart.scales.x;
            const range = touchInitialMax - touchInitialMin;
            const center = (touchInitialMax + touchInitialMin) / 2;
            const newRange = range * zoomFactor;

            xAxis.options.min = center - newRange / 2;
            xAxis.options.max = center + newRange / 2;
            chart.update('none');
            syncAllCharts(chart, xAxis.options.min, xAxis.options.max);
        }
    }, {passive: false});

    canvas.addEventListener('touchend', function(e) {
        if (e.touches.length === 0) {
            touchStartX = null;
            lastPinchDistance = null;
        }
    }, {passive: true});

    // Zoom with wheel only when Ctrl key is pressed
    canvas.addEventListener('wheel', function(e) {
        // Only prevent default and zoom if Ctrl key is pressed
        if (e.ctrlKey) {
            e.preventDefault();
            const xAxis = chart.scales.x;
            const zoomFactor = e.deltaY > 0 ? 1.1 : 0.9;
            
            const range = xAxis.max - xAxis.min;
            const center = (xAxis.max + xAxis.min) / 2;
            const newRange = range * zoomFactor;
            
            xAxis.options.min = center - newRange / 2;
            xAxis.options.max = center + newRange / 2;
            
            chart.update('none');

            syncAllCharts(chart, xAxis.options.min, xAxis.options.max);
        }
    });
}

function syncAllCharts(sourceChart, min, max) {
    const allCharts = [vfrChart, temperatureChart, cloudChart, windChart];
    for (const c of allCharts) {
        if (c && c !== sourceChart) {
            syncManualPan(c, min, max);
        }
    }
}

function syncManualPan(targetChart, min, max) {
    const xAxis = targetChart.scales.x;
    xAxis.options.min = min;
    xAxis.options.max = max;
    targetChart.update('none');
}
