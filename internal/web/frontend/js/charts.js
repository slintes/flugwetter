// The four charts and the axis configuration they share.
//
// `charts` is the registry every other module reaches the instances through. It is a
// mutable object rather than four exported bindings so that panzoom and api see the
// instances once initializeCharts has filled it in.
//
// Importing ./plugins.js registers the drawing plugins; that has to happen before any
// chart is constructed.

import './plugins.js';
import { getWindDirectionName } from './plugins.js';
import { pinAxisWidth, AXIS_WIDTHS_WIDE, isNarrowViewport } from './viewport.js';
import { formatPenalties } from './vfr-penalties.js';

export const charts = {
    vfr: null,
    temperature: null,
    cloud: null,
    wind: null,
};

export function initializeCharts() {
    const vfrCtx = document.getElementById('vfrChart').getContext('2d');

    const xAxisConfig = function(drawOnChartArea){
        return {
            type: 'time',
            // No `parser`: every x value is already an epoch-millisecond number. The
            // previous 'YYYY-MM-DDTHH:mm' was a moment.js pattern, and under the
            // date-fns adapter YYYY/DD mean week-numbering year and day-of-year, which
            // that library throws on. `distribution` was a Chart.js v2 option.
            //
            // Must be spelled out: the temperature chart's precipitation bars pull in
            // Chart.js's bar controller defaults, which set `offset: true` on the index
            // scale. That makes the time scale reserve half a slot at each end, so the
            // same timestamp lands ~26px further right and 5% closer together than on the
            // three charts without bars — the four time axes drift apart.
            offset: false,
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

    charts.vfr = new Chart(vfrCtx, {
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
                    // No colour swatch. It indents every line but is not counted when
                    // Chart.js sizes the box, so the longest line runs past the right edge
                    // and loses its last character -- which is the cost, the part worth
                    // reading. Seen with "crosswind gust spread 5.7 kn — good, −1" on a
                    // 360px screen. This chart has one dataset, so the swatch identified
                    // nothing anyway.
                    displayColors: false,
                    callbacks: {
                        title: function(context) {
                            // Display time in local timezone with date and time
                            return new Date(context[0].parsed.x).toLocaleString();
                        },
                        // What the score lost and to what. Chart.js renders an array as
                        // one line each.
                        label: function(context) {
                            return formatPenalties(context.raw, { compact: isNarrowViewport() });
                        }
                    }
                }
            }
        }
    });

    const tempCtx = document.getElementById('temperatureChart').getContext('2d');
    charts.temperature = new Chart(tempCtx, {
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

    const cloudCtx = document.getElementById('cloudChart').getContext('2d');

    charts.cloud = new Chart(cloudCtx, {
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
                        // One row per dataset, each reporting its own series.
                        //
                        // Visibility used to be appended to the cloud row, which read fine
                        // until an hour had no cloud at all: with no cloud point there was
                        // no row to append it to, and the tooltip came up empty on exactly
                        // the clear hours where visibility is the only thing left to check.
                        label: function(context) {
                            const point = context.raw;
                            switch (context.dataset.label) {
                                case 'Cloud Layers':
                                    return `Height: ${point.y}ft, Coverage: ${point.coverage}%`;
                                case 'Visibility (km)':
                                    return `Visibility: ${point.y.toFixed(1)} km`;
                                default:
                                    // Cloud base carries its own label on the chart, drawn
                                    // and colour-coded by the cloudSymbols plugin.
                                    return '';
                            }
                        }
                    }
                }
            }
        }
    });

    const windCtx = document.getElementById('windChart').getContext('2d');

    charts.wind = new Chart(windCtx, {
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
