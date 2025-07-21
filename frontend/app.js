let temperatureChart;
let cloudChart;

// Initialize charts when page loads
document.addEventListener('DOMContentLoaded', function() {
    initializeCharts();
    loadWeatherData();
});

function initializeCharts() {
    // Temperature Chart
    const tempCtx = document.getElementById('temperatureChart').getContext('2d');
    temperatureChart = new Chart(tempCtx, {
        type: 'line',
        data: {
            labels: [],
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
                    grid: {
                        color: 'rgba(0,0,0,0.1)'
                    },
                    title: {
                        display: true,
                        text: 'Time',
                        font: { size: 14, weight: 'bold' }
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

    // Cloud Cover Chart
    const cloudCtx = document.getElementById('cloudChart').getContext('2d');
    cloudChart = new Chart(cloudCtx, {
        type: 'bar',
        data: {
            labels: [],
            datasets: [
                {
                    label: 'Low Clouds (%)',
                    data: [],
                    backgroundColor: 'rgba(116, 185, 255, 0.8)',
                    borderColor: '#74b9ff',
                    borderWidth: 1
                },
                {
                    label: 'Mid Clouds (%)',
                    data: [],
                    backgroundColor: 'rgba(9, 132, 227, 0.8)',
                    borderColor: '#0984e3',
                    borderWidth: 1
                },
                {
                    label: 'High Clouds (%)',
                    data: [],
                    backgroundColor: 'rgba(45, 52, 54, 0.8)',
                    borderColor: '#2d3436',
                    borderWidth: 1
                }
            ]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            scales: {
                y: {
                    beginAtZero: true,
                    max: 100,
                    grid: {
                        color: 'rgba(0,0,0,0.1)'
                    },
                    title: {
                        display: true,
                        text: 'Cloud Cover (%)',
                        font: { size: 14, weight: 'bold' }
                    }
                },
                x: {
                    grid: {
                        color: 'rgba(0,0,0,0.1)'
                    },
                    title: {
                        display: true,
                        text: 'Time',
                        font: { size: 14, weight: 'bold' }
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
    // Update temperature chart
    const tempLabels = data.temperature_data.map(point => 
        new Date(point.time).toLocaleTimeString('en-US', { 
            hour: '2-digit', 
            minute: '2-digit',
            month: 'short',
            day: 'numeric'
        })
    );
    const tempData = data.temperature_data.map(point => point.temperature);
    
    temperatureChart.data.labels = tempLabels;
    temperatureChart.data.datasets[0].data = tempData;
    temperatureChart.update();
    
    // Update cloud chart
    const cloudLabels = data.cloud_data.map(point => 
        new Date(point.time).toLocaleTimeString('en-US', { 
            hour: '2-digit', 
            minute: '2-digit',
            month: 'short',
            day: 'numeric'
        })
    );
    
    cloudChart.data.labels = cloudLabels;
    cloudChart.data.datasets[0].data = data.cloud_data.map(point => point.low_cloud_cover);
    cloudChart.data.datasets[1].data = data.cloud_data.map(point => point.mid_cloud_cover);
    cloudChart.data.datasets[2].data = data.cloud_data.map(point => point.high_cloud_cover);
    cloudChart.update();
}

// Function to refresh data
function refreshData() {
    loadWeatherData();
}

// Auto-refresh every 15 minutes (900000 ms)
setInterval(refreshData, 900000); 