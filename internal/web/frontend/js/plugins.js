// Every custom Chart.js plugin, plus the two drawing helpers they use.
//
// Chart.js draws none of the aviation symbols itself: the barbs, cloud shapes, VFR labels
// and reference lines are all painted in afterDatasetsDraw/afterDraw hooks registered here.
// Changing how a symbol looks means editing a hook, not a dataset option.
//
// Importing this module registers the plugins as a side effect, which is why charts.js
// imports it before constructing anything.

import { weatherCodeToIcon } from './weather-icons.js';
import { barbComponents, isCalm } from './barbs.js';
import { axisWidths, isNarrowViewport, vfrMetrics, tooltipFont } from './viewport.js';
import { bands, visibleBands, NIGHT_FILL, TWILIGHT_FILL, DAY_FILL, RESTRICTED_FILL } from './bands.js';

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

// responsiveAxes runs before every layout, which is what makes a resize across the
// breakpoint take effect without a resize handler of our own.
//
// Three jobs: size the tooltip text on every chart; reserve the axis space on the VFR
// chart, which has no visible axis of its own and would otherwise run wider than the other
// three; and drop the rotated axis titles on a phone, because "Height (feet) - Log Scale"
// plus a "10000ft" tick does not fit in 54px and the tick labels are the half that cannot
// be inferred from the chart heading.
//
// The tooltip sizes are written here as plain numbers rather than declared as scriptable
// options. Chart.js sizes the tooltip box in its update pass and paints it in the draw
// pass, and a plain value is what guarantees both see the same number; a scriptable font
// re-resolves per call, which is a needless way to invite the two apart. Rewriting them
// here is also what makes them follow a resize, exactly as the axis widths do below.

// backgroundBands shades the light and the airspace behind all four charts: night, the
// civil twilight either side of it, an active ED-R, and the home field's opening window.
//
// beforeDatasetsDraw, so it sits behind the data rather than over it -- an afterDraw hook
// would put a grey wash on top of the wind barbs and the VFR icons. Positions come from
// scales.x, which is what makes the bands pan and zoom with everything else instead of
// staying nailed to the canvas.
Chart.register({
    id: 'backgroundBands',
    beforeDatasetsDraw(chart) {
        const scale = chart.scales.x;
        const area = chart.chartArea;
        if (!scale || !area) {
            return;
        }

        const day = visibleBands(bands.day, scale.min, scale.max);
        const restricted = visibleBands(bands.restricted, scale.min, scale.max);
        const twilight = visibleBands(bands.twilight, scale.min, scale.max);
        const night = visibleBands(bands.night, scale.min, scale.max);
        if (day.length === 0 && restricted.length === 0
            && twilight.length === 0 && night.length === 0) {
            return;
        }

        const ctx = chart.ctx;
        ctx.save();
        // Clipped to the plot area: without this a band runs out over the axis labels,
        // which on these charts are the only thing separating four stacked cards.
        ctx.beginPath();
        ctx.rect(area.left, area.top, area.right - area.left, area.bottom - area.top);
        ctx.clip();

        const paint = (list, fill) => {
            ctx.fillStyle = fill;
            for (const band of list) {
                const from = scale.getPixelForValue(band.from);
                const to = scale.getPixelForValue(band.to);
                ctx.fillRect(from, area.top, to - from, area.bottom - area.top);
            }
        };

        // Weakest claim first, strongest last. The four lists are disjoint -- setBands
        // subtracts them from each other -- so the order is redundant, and it is here to
        // state the precedence if that ever changes.
        paint(day, DAY_FILL);
        paint(restricted, RESTRICTED_FILL);
        paint(twilight, TWILIGHT_FILL);
        paint(night, NIGHT_FILL);
        ctx.restore();
    }
});

Chart.register({
    id: 'responsiveAxes',
    beforeLayout: function(chart) {
        const widths = axisWidths();
        const font = tooltipFont();

        const tooltip = chart.options.plugins && chart.options.plugins.tooltip;
        if (tooltip) {
            tooltip.titleFont = { size: font.title, weight: 'bold' };
            tooltip.bodyFont = { size: font.body };
            tooltip.padding = font.padding;
        }

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

// The reference marks on the temperature chart: 30 where heat starts to cost take-off
// performance, 20 as the pleasant middle, 0 where frost and carburettor icing begin. Warm to
// cold, red through green to blue, so the colour says which end of the scale it is.
const TEMPERATURE_MARKS = [
    {value: 30, color: 'rgba(200, 0, 0, 0.25)'},
    {value: 20, color: 'rgba(0, 150, 0, 0.25)'},
    {value: 0, color: 'rgba(0, 100, 200, 0.25)'},
];

// Unlike the cloud and wind axes, this one auto-scales, and any of these marks can be off the
// end of it -- 30 on most days, 0 for most of the summer. getPixelForValue then returns a
// pixel outside the plot, where the line would be drawn across the legend or the axis labels,
// so each mark is drawn only when the axis actually reaches it.
Chart.register({
    id: 'temperatureGridLines',
    afterDraw: function(chart) {
        if (chart.canvas.id !== 'temperatureChart') {
            return;
        }

        const chartArea = chart.chartArea;
        const ctx = chart.ctx;

        ctx.save();
        ctx.lineWidth = 3;

        for (const mark of TEMPERATURE_MARKS) {
            const yPosition = chart.scales.y.getPixelForValue(mark.value);
            if (!(yPosition >= chartArea.top && yPosition <= chartArea.bottom)) {
                continue;
            }

            ctx.beginPath();
            ctx.moveTo(chartArea.left, yPosition);
            ctx.lineTo(chartArea.right, yPosition);
            ctx.strokeStyle = mark.color;
            ctx.stroke();
        }

        ctx.restore();
    }
});

function drawWindBarb(ctx, x, y, speedKnots, directionDegrees) {
    ctx.save();
    
    // Calm draws a circle rather than a shaft; the threshold lives in barbs.js.
    if (isCalm(speedKnots)) {
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
    const { pennants, fullBarbs, halfBarb } = barbComponents(speedKnots);
    
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

export function getWindDirectionName(degrees) {
    const directions = ['N', 'NNE', 'NE', 'ENE', 'E', 'ESE', 'SE', 'SSE', 'S', 'SSW', 'SW', 'WSW', 'W', 'WNW', 'NW', 'NNW'];
    const index = Math.round(degrees / 22.5) % 16;
    return directions[index];
}

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
