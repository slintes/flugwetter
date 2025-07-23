let temperatureChart;
let cloudChart;
let windChart;
let vfrChart;

// Initialize charts when page loads
document.addEventListener('DOMContentLoaded', function() {
    initializeCharts();
    loadWeatherData();
    resetZoom(24)

    // Set up manual pan/zoom after charts are created
    setTimeout(setupManualPanZoom, 1000);
});

function initializeCharts() {
    // VFR Chart
    const vfrCtx = document.getElementById('vfrChart').getContext('2d');

    xAxisConfig = function(drawOnChartArea){
        return {
            type: 'time',
            distribution: 'linear',
            time: {
                parser: 'YYYY-MM-DDTHH:mm',
                unit: 'hour',
                stepSize: 1,
                displayFormats: {
                    hour: 'MMM dd HH:mm'
                }
            },
            grid: {
                drawOnChartArea: drawOnChartArea,
                z: -1,
                color: 'rgba(0,0,0,0.3)'
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
                            // For midnight, show date and time
                            return date.toLocaleDateString('en-US', {
                                month: 'short',
                                day: 'numeric'
                            }) + '\n' + date.toLocaleTimeString('en-US', {
                                hour: '2-digit',
                                minute: '2-digit',
                                hour12: false
                            });
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
            },
        }
    }



    // Custom plugin to draw vfr text with color coding
    Chart.register({
        id: 'vfrText',
        afterDatasetsDraw: function(chart) {
            if (chart.canvas.id === 'vfrChart') {
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

                                // Set text color based on probability ranges
                                if (probability > 90) {
                                    ctx.fillStyle = 'darkgreen'; // Dark green for over 90%
                                } else if (probability > 70) {
                                    ctx.fillStyle = 'green'; // Green for over 80%
                                } else if (probability > 50) {
                                    ctx.fillStyle = 'orange'; // Orange for over 70%
                                } else {
                                    ctx.fillStyle = 'red'; // Red for below 50%
                                }

                                // Set text properties
                                ctx.font = '20px Narrow';
                                ctx.textAlign = 'center';
                                ctx.textBaseline = 'middle';

                                // Draw the text
                                ctx.fillText(`${probability}`, xPos, yPos);
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
                borderColor: '#0984e3',
                backgroundColor: 'rgba(9, 132, 227, 0.3)',
                borderWidth: 2,
                fill: true,
                tension: 0.4,
                yAxisID: 'y1',
                segment: {
                    borderColor: function(ctx) {
                        const p0 = ctx.p0.raw;
                        const p1 = ctx.p1.raw;
                        if (p0 && p0.precipitationProbability !== undefined) {
                            const probability = p0.precipitationProbability / 100;
                            const alpha = Math.max(0.2, probability);
                            return `rgba(9, 132, 227, ${alpha})`;
                        }
                        return 'rgba(9, 132, 227, 0.5)';
                    },
                    backgroundColor: function(ctx) {
                        const p0 = ctx.p0.raw;
                        if (p0 && p0.precipitationProbability !== undefined) {
                            const probability = p0.precipitationProbability / 100;
                            const alpha = Math.max(0.1, probability * 0.4);
                            return `rgba(9, 132, 227, ${alpha})`;
                        }
                        return 'rgba(9, 132, 227, 0.2)';
                    }
                }
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
                                const alpha = 0.1 + (point.coverage / 100 * 0.9);
                                
                                // Calculate base size for the cloud (slightly larger for a nicer appearance)
                                const baseSize = 9;
                                
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
                            const yPos = chartArea.top + (chartArea.bottom - chartArea.top) * 0.65;

                            // Skip if outside visible area
                            if (xPos < chartArea.left || xPos > chartArea.right) {
                                return;
                            }

                            // Set text style
                            ctx.font = '16px Narrow';
                            ctx.textAlign = 'center';
                            ctx.textBaseline = 'middle';

                            // Color coding based on height
                            let textColor;
                            if (point.y < 10) {
                                textColor = 'rgb(255,0,0)'; // Red for < 1000ft
                            } else if (point.y < 15) {
                                textColor = 'rgb(255,66,0)'; // Orange for 1000-2000ft
                            } else if (point.y < 20) {
                                textColor = 'rgb(255,196,0)'; // Orange for 1000-2000ft
                            } else if (point.y < 25) {
                                textColor = 'rgb(174,225,18)'; // Light green for 2000-3000ft
                            } else if (point.y < 30) {
                                textColor = 'rgba(28,221,28,0.7)'; // Light green for 2000-3000ft
                            } else {
                                textColor = 'rgba(0, 150, 0, 0.4)'; // Dark green for > 3000ft
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
                    type: 'logarithmic',
                    position: 'left',
                    min: 100, // Start from 100ft for low-level clouds
                    max: 24000, // Max altitude 24,000ft
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
                    type: 'logarithmic',
                    position: 'left',
                    min: 20,
                    max: 10000, // Max altitude in ft
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
        
        const response = await fetch('/api/weather');
        
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

        // The visibility is already in kilometers
        if (timePoint.visibility) {
            visibilityData.push({
                x: timeValue,
                y: timePoint.visibility
            });
        }

        // The cloud base
        if (timePoint.base) {
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

    if (data.wind_data) {
        data.wind_data.forEach(timePoint => {
            // Convert UTC time string to local timezone Date object
            const timeValue = new Date(timePoint.time + 'Z').getTime(); // Add 'Z' to indicate UTC
            
            timePoint.wind_layers.forEach(layer => {
                windScatterData.push({
                    x: timeValue,
                    y: layer.height_feet,
                    speed: layer.speed,
                    direction: layer.direction,
                    symbol: layer.symbol
                });
            });

            // Add line data for wind speed and gusts at 10m
            windSpeed10mData.push({
                x: timeValue,
                y: timePoint.wind_speed_10m
            });

            windGusts10mData.push({
                x: timeValue,
                y: timePoint.wind_gusts_10m
            });

        });
    }
    
    windChart.data.datasets[0].data = windScatterData;
    windChart.data.datasets[1].data = windSpeed10mData;
    windChart.data.datasets[2].data = windGusts10mData;
    windChart.update();

    const vfrData = [];
    if (data.vfr_data) {
        data.vfr_data.forEach(timePoint => {
            // Convert UTC time string to local timezone Date object
            const timeValue = new Date(timePoint.time + 'Z').getTime(); // Add 'Z' to indicate UTC

            vfrData.push({
                x: timeValue,
                y: timePoint.probability / 100, // Convert to 0-1 range for chart
                probability: timePoint.probability // Keep original percentage for display
            })
        })
    }
    vfrChart.data.datasets[0].data = vfrData;
    vfrChart.update();
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
    xAxis.options.min = new Date();
    min = undefined;
    max = undefined;
    if ( hours !== undefined ) {
        min = new Date(Date.now() - 3 * 60 * 60 * 1000) // start 3 hours ago
        max = new Date(Date.now() + hours * 60 * 60 * 1000);
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
        
        // Sync with other charts
        if (chart === temperatureChart) {
            if (cloudChart) syncManualPan(cloudChart, xAxis.options.min, xAxis.options.max);
            if (windChart) syncManualPan(windChart, xAxis.options.min, xAxis.options.max);
            if (vfrChart) syncManualPan(vfrChart, xAxis.options.min, xAxis.options.max);
        } else if (chart === cloudChart) {
            if (temperatureChart) syncManualPan(temperatureChart, xAxis.options.min, xAxis.options.max);
            if (windChart) syncManualPan(windChart, xAxis.options.min, xAxis.options.max);
            if (vfrChart) syncManualPan(vfrChart, xAxis.options.min, xAxis.options.max);
        } else if (chart === windChart) {
            if (temperatureChart) syncManualPan(temperatureChart, xAxis.options.min, xAxis.options.max);
            if (cloudChart) syncManualPan(cloudChart, xAxis.options.min, xAxis.options.max);
            if (vfrChart) syncManualPan(vfrChart, xAxis.options.min, xAxis.options.max);
        } else if (chart === vfrChart) {
            if (temperatureChart) syncManualPan(temperatureChart, xAxis.options.min, xAxis.options.max);
            if (cloudChart) syncManualPan(cloudChart, xAxis.options.min, xAxis.options.max);
            if (windChart) syncManualPan(windChart, xAxis.options.min, xAxis.options.max);
        }
    });
    
    canvas.addEventListener('mouseup', function(e) {
        if (isDragging) {

        }
        isDragging = false;
    });
    
    canvas.addEventListener('mouseleave', function(e) {
        isDragging = false;
    });
    
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
            
            // Sync with other charts
            if (chart === temperatureChart) {
                if (cloudChart) syncManualPan(cloudChart, xAxis.options.min, xAxis.options.max);
                if (windChart) syncManualPan(windChart, xAxis.options.min, xAxis.options.max);
                if (vfrChart) syncManualPan(vfrChart, xAxis.options.min, xAxis.options.max);
            } else if (chart === cloudChart) {
                if (temperatureChart) syncManualPan(temperatureChart, xAxis.options.min, xAxis.options.max);
                if (windChart) syncManualPan(windChart, xAxis.options.min, xAxis.options.max);
                if (vfrChart) syncManualPan(vfrChart, xAxis.options.min, xAxis.options.max);
            } else if (chart === windChart) {
                if (temperatureChart) syncManualPan(temperatureChart, xAxis.options.min, xAxis.options.max);
                if (cloudChart) syncManualPan(cloudChart, xAxis.options.min, xAxis.options.max);
                if (vfrChart) syncManualPan(vfrChart, xAxis.options.min, xAxis.options.max);
            } else if (chart === vfrChart) {
                if (temperatureChart) syncManualPan(temperatureChart, xAxis.options.min, xAxis.options.max);
                if (cloudChart) syncManualPan(cloudChart, xAxis.options.min, xAxis.options.max);
                if (windChart) syncManualPan(windChart, xAxis.options.min, xAxis.options.max);
            }
        }
    });
}

function syncManualPan(targetChart, min, max) {
    const xAxis = targetChart.scales.x;
    xAxis.options.min = min;
    xAxis.options.max = max;
    targetChart.update('none');
}

 