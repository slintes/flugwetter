// Pan and zoom, hand-rolled rather than chartjs-plugin-zoom, plus the initial range.
//
// The four charts share one time axis, so any change to one is pushed to the other three
// through syncAllCharts. A new chart that is not in that list will silently drift.

import { charts } from './charts.js';
import { axisWidths, vfrMetrics } from './viewport.js';

// 15-minute refresh.
const VFR_SLOT_GAP = 4;      // so neighbours are visibly apart, not merely touching
// 3 rather than 6: on a phone the plot cannot fit 6 hours without the icons colliding, and
// a readable 3-hour view that pans beats an unreadable 6-hour one.
const INITIAL_ZOOM_MIN_HOURS = 3;
const INITIAL_ZOOM_MAX_HOURS = 72;

// How far a touch must travel before it counts as horizontal (pan) or vertical (page
// scroll). Low enough that a pan still feels immediate, high enough that the first few
// pixels of a scroll are not read as a pan.
const TOUCH_AXIS_LOCK_PX = 8;

let initialZoomApplied = false;

// vfrSlotWidth returns the horizontal room one hour needs before the icon or the label
// starts touching its neighbour. The labels are measured rather than assumed, because
// their width swings a lot: "0" against "100?".
export function vfrSlotWidth() {
    const metrics = vfrMetrics();
    const ctx = charts.vfr.ctx;
    ctx.save();
    ctx.font = metrics.font;
    let widestLabel = 0;
    (charts.vfr.data.datasets[0].data || []).forEach(point => {
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

export function initialZoomHours() {
    const area = charts.vfr.chartArea;
    const widths = axisWidths();
    const plotWidth = area && area.right > area.left
        ? area.right - area.left
        : charts.vfr.canvas.clientWidth - widths.left - widths.right;

    // resetChartZoom puts 3 hours of history on screen on top of the range asked for, so
    // those slots have to come out of the budget.
    const hours = Math.floor(plotWidth / vfrSlotWidth()) - 3;
    return Math.min(INITIAL_ZOOM_MAX_HOURS, Math.max(INITIAL_ZOOM_MIN_HOURS, hours));
}

// Function to reset zoom on all charts
export function resetZoom(hours) {
    if (charts.vfr) {
        resetChartZoom(charts.vfr, hours);
    }
    if (charts.temperature) {
        resetChartZoom(charts.temperature, hours);
    }
    if (charts.cloud) {
        resetChartZoom(charts.cloud, hours);
    }
    if (charts.wind) {
        resetChartZoom(charts.wind, hours);
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

export function setupManualPanZoom() {
    if (charts.vfr) {
        addManualPanZoom(charts.vfr);
    }
    if (charts.temperature) {
        addManualPanZoom(charts.temperature);
    }
    if (charts.cloud) {
        addManualPanZoom(charts.cloud);
    }
    if (charts.wind) {
        addManualPanZoom(charts.wind);
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
    let touchStartY = null;
    // 'x' pans the time axis, 'y' is the browser's to scroll. Null until the gesture has
    // moved far enough to tell them apart; once set it holds for the whole gesture, so a
    // drifting finger cannot flip a page scroll into a pan half way through.
    let touchAxis = null;
    let touchInitialMin = null;
    let touchInitialMax = null;
    let lastPinchDistance = null;

    canvas.addEventListener('touchstart', function(e) {
        if (e.touches.length === 1) {
            touchStartX = e.touches[0].clientX;
            touchStartY = e.touches[0].clientY;
            touchAxis = null;
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
            const deltaX = e.touches[0].clientX - touchStartX;
            const deltaY = e.touches[0].clientY - touchStartY;

            if (touchAxis === null) {
                // Too small to read a direction from yet -- do nothing rather than guess.
                if (Math.abs(deltaX) < TOUCH_AXIS_LOCK_PX && Math.abs(deltaY) < TOUCH_AXIS_LOCK_PX) {
                    return;
                }
                touchAxis = Math.abs(deltaX) > Math.abs(deltaY) ? 'x' : 'y';
            }
            if (touchAxis === 'y') {
                return; // the page scrolls; touch-action: pan-y lets the browser do it
            }

            // Once the browser has committed to scrolling, touchmove is no longer
            // cancelable and preventDefault only logs a warning.
            if (e.cancelable) {
                e.preventDefault();
            }
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
            if (e.cancelable) {
                e.preventDefault();
            }
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
            touchStartY = null;
            touchAxis = null;
            lastPinchDistance = null;
        }
    }, {passive: true});

    // A gesture the browser takes over for scrolling ends in touchcancel, not touchend.
    canvas.addEventListener('touchcancel', function() {
        touchStartX = null;
        touchStartY = null;
        touchAxis = null;
        lastPinchDistance = null;
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
    const allCharts = [charts.vfr, charts.temperature, charts.cloud, charts.wind];
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


// applyInitialZoomOnce derives the opening range from the data and the laid-out plot width,
// and does it exactly once. Airport switches and the 15-minute refresh must not yank the
// range back from wherever the user has panned to.
export function applyInitialZoomOnce() {
    if (initialZoomApplied) {
        return;
    }
    initialZoomApplied = true;
    resetZoom(initialZoomHours());
}
