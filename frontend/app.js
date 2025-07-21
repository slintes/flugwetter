let temperatureChart;
let cloudChart;

// Initialize charts when page loads
document.addEventListener('DOMContentLoaded', function() {
    initializeCharts();
    loadWeatherData();
    
    // Set up manual pan/zoom after charts are created
    setTimeout(setupManualPanZoom, 1000);
});



function initializeCharts() {
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
                fill: true,
                tension: 0.4
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
                x: {
                    type: 'time',
                    time: {
                        parser: 'YYYY-MM-DDTHH:mm',
                        displayFormats: {
                            hour: 'MMM dd HH:mm'
                        }
                    },
                    min: new Date(),
                    max: new Date(Date.now() + 24 * 60 * 60 * 1000), // 24 hours from now
                    grid: {
                        color: 'rgba(0,0,0,0.1)'
                    },
                    title: {
                        display: true,
                        text: 'Time',
                        font: { size: 14, weight: 'bold' }
                    },
                    ticks: {
                        maxTicksLimit: 8,
                        maxRotation: 45
                    }
                }
            },
            plugins: {
                legend: {
                    display: true,
                    position: 'top'
                }
            }
        }
    });

    // Cloud Cover Chart - Scatter plot with height vs time
    const cloudCtx = document.getElementById('cloudChart').getContext('2d');
    
    // Register a custom plugin for rendering cloud symbols
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
                        if (point && point.symbol) {
                            const meta = chart.getDatasetMeta(datasetIndex);
                            const element = meta.data[index];
                            if (element && !element.skip && 
                                element.x >= chartArea.left && element.x <= chartArea.right &&
                                element.y >= chartArea.top && element.y <= chartArea.bottom) {
                                
                                // Set up text rendering
                                ctx.font = '18px Arial';
                                ctx.textAlign = 'center';
                                ctx.textBaseline = 'middle';
                                
                                // Add a slight background to make symbols more visible
                                const textWidth = ctx.measureText(point.symbol).width;
                                ctx.fillStyle = 'rgba(255, 255, 255, 0.9)';
                                ctx.fillRect(element.x - textWidth/2 - 3, element.y - 10, textWidth + 6, 20);
                                
                                // Draw the symbol
                                ctx.fillStyle = '#333';
                                ctx.fillText(point.symbol, element.x, element.y);
                            }
                        }
                    });
                }
            });
            
            // Restore the context state
            ctx.restore();
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
                pointRadius: 0 // Hide points, we'll show symbols instead
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
                    min: 40, // Start from 40m for low-level clouds
                    max: 10000, // Max altitude in meters
                    grid: {
                        color: 'rgba(0,0,0,0.1)'
                    },
                    title: {
                        display: true,
                        text: 'Height (meters) - Log Scale',
                        font: { size: 14, weight: 'bold' }
                    },
                    ticks: {
                        callback: function(value) {
                            // Show nice round numbers on log scale
                            if (value >= 1000) {
                                return (value / 1000) + 'km';
                            }
                            return value + 'm';
                        }
                    }
                },
                x: {
                    type: 'time',
                    time: {
                        parser: 'YYYY-MM-DDTHH:mm',
                        displayFormats: {
                            hour: 'MMM dd HH:mm'
                        }
                    },
                    min: new Date(),
                    max: new Date(Date.now() + 24 * 60 * 60 * 1000), // 24 hours from now
                    grid: {
                        color: 'rgba(0,0,0,0.1)'
                    },
                    title: {
                        display: true,
                        text: 'Time',
                        font: { size: 14, weight: 'bold' }
                    },
                    ticks: {
                        maxTicksLimit: 8,
                        maxRotation: 45
                    }
                }
            },
            plugins: {
                legend: {
                    display: false // Hide legend since we show symbols
                },
                tooltip: {
                    callbacks: {
                        title: function(context) {
                            return new Date(context[0].parsed.x).toLocaleString();
                        },
                        label: function(context) {
                            const point = context.raw;
                            return `Height: ${point.y}m, Coverage: ${point.coverage}%, Symbol: ${point.symbol}`;
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
    const tempData = data.temperature_data.map(point => ({
        x: new Date(point.time).getTime(),
        y: point.temperature
    }));
    
    temperatureChart.data.datasets[0].data = tempData;
    temperatureChart.update();
    
    // Update cloud chart with scatter data
    const scatterData = [];
    
    data.cloud_data.forEach(timePoint => {
        const timeValue = new Date(timePoint.time).getTime();
        
        timePoint.cloud_layers.forEach(layer => {
            scatterData.push({
                x: timeValue,
                y: layer.height_meters,
                coverage: layer.coverage,
                symbol: layer.symbol
            });
        });
    });
    
    cloudChart.data.datasets[0].data = scatterData;
    cloudChart.update();
}

// Function to refresh data
function refreshData() {
    loadWeatherData();
}

// Auto-refresh every 15 minutes (900000 ms)
setInterval(refreshData, 900000);

// Function to reset zoom on both charts
function resetZoom() {
    if (temperatureChart) {
        resetChartZoom(temperatureChart);
    }
    if (cloudChart) {
        resetChartZoom(cloudChart);
    }
}

function resetChartZoom(chart) {
    const xAxis = chart.scales.x;
    xAxis.options.min = undefined;
    xAxis.options.max = undefined;
    chart.update();
}

// Manual pan/zoom setup function
function setupManualPanZoom() {
    if (temperatureChart) {
        addManualPanZoom(temperatureChart, 'Temperature');
    }
    if (cloudChart) {
        addManualPanZoom(cloudChart, 'Cloud');
    }
}

function addManualPanZoom(chart, chartName) {
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
        
        // Sync with other chart
        if (chart === temperatureChart && cloudChart) {
            syncManualPan(cloudChart, xAxis.options.min, xAxis.options.max);
        } else if (chart === cloudChart && temperatureChart) {
            syncManualPan(temperatureChart, xAxis.options.min, xAxis.options.max);
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
    
    // Zoom with wheel
    canvas.addEventListener('wheel', function(e) {
        e.preventDefault();
        const xAxis = chart.scales.x;
        const zoomFactor = e.deltaY > 0 ? 1.1 : 0.9;
        
        const range = xAxis.max - xAxis.min;
        const center = (xAxis.max + xAxis.min) / 2;
        const newRange = range * zoomFactor;
        
        xAxis.options.min = center - newRange / 2;
        xAxis.options.max = center + newRange / 2;
        
        chart.update('none');
        
        // Sync with other chart
        if (chart === temperatureChart && cloudChart) {
            syncManualPan(cloudChart, xAxis.options.min, xAxis.options.max);
        } else if (chart === cloudChart && temperatureChart) {
            syncManualPan(temperatureChart, xAxis.options.min, xAxis.options.max);
        }
        

    });
    

}

function syncManualPan(targetChart, min, max) {
    const xAxis = targetChart.scales.x;
    xAxis.options.min = min;
    xAxis.options.max = max;
    targetChart.update('none');
}

 