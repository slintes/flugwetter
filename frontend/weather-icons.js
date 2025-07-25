// Mapping from WMO weather codes to icon filenames
const weatherCodeToIcon = {
    "0": "0.png",                      // Clear sky
    "1": "1.png",                      // Mainly clear
    "2": "2.png",                      // Partly cloudy
    "3": "3.png",                      // Overcast
    "45": "45.png",                    // Fog
    "48": "48.png",                    // Depositing rime fog
    "51": "51.png",                    // Drizzle: Light
    "53": "53.png",                    // Drizzle: Moderate
    "55": "55.png",                    // Drizzle: Dense
    "56": "56.png",                    // Freezing Drizzle: Light
    "57": "57.png",                    // Freezing Drizzle: Dense
    "61": "61.png",                    // Rain: Slight
    "63": "63.png",                    // Rain: Moderate
    "65": "65.png",                    // Rain: Heavy
    "66": "66.png",                    // Freezing Rain: Light
    "67": "67.png",                    // Freezing Rain: Heavy
    "71": "71.png",                    // Snow fall: Slight
    "73": "73.png",                    // Snow fall: Moderate
    "75": "75.png",                    // Snow fall: Heavy
    "77": "77.png",                    // Snow grains
    "80": "80.png",                    // Rain showers: Slight
    "81": "81.png",                    // Rain showers: Moderate
    "82": "82.png",                    // Rain showers: Violent
    "85": "85.png",                    // Snow showers: Slight
    "86": "86.png",                    // Snow showers: Heavy
    "95": "95.png",                    // Thunderstorm: Slight or moderate
    "96": "96.png",                    // Thunderstorm with slight hail
    "99": "99.png",                     // Thunderstorm with heavy hail
    "0-night": "0-night.png",                      // Clear sky
    "1-night": "1-night.png",                      // Mainly clear
    "2-night": "2-night.png",                      // Partly cloudy
    "3-night": "3-night.png",                      // Overcast
    "45-night": "45-night.png",                    // Fog
    "48-night": "48-night.png",                    // Depositing rime fog
    "51-night": "51-night.png",                    // Drizzle-night: Light
    "53-night": "53-night.png",                    // Drizzle-night: Moderate
    "55-night": "55-night.png",                    // Drizzle-night: Dense
    "56-night": "56-night.png",                    // Freezing Drizzle-night: Light
    "57-night": "57-night.png",                    // Freezing Drizzle-night: Dense
    "61-night": "61-night.png",                    // Rain-night: Slight
    "63-night": "63-night.png",                    // Rain-night: Moderate
    "65-night": "65-night.png",                    // Rain-night: Heavy
    "66-night": "66-night.png",                    // Freezing Rain-night: Light
    "67-night": "67-night.png",                    // Freezing Rain-night: Heavy
    "71-night": "71-night.png",                    // Snow fall-night: Slight
    "73-night": "73-night.png",                    // Snow fall-night: Moderate
    "75-night": "75-night.png",                    // Snow fall-night: Heavy
    "77-night": "77-night.png",                    // Snow grains
    "80-night": "80-night.png",                    // Rain showers-night: Slight
    "81-night": "81-night.png",                    // Rain showers-night: Moderate
    "82-night": "82-night.png",                    // Rain showers-night: Violent
    "85-night": "85-night.png",                    // Snow showers-night: Slight
    "86-night": "86-night.png",                    // Snow showers-night: Heavy
    "95-night": "95-night.png",                    // Thunderstorm-night: Slight or moderate
    "96-night": "96-night.png",                    // Thunderstorm with slight hail
    "99-night": "99-night.png"                     // Thunderstorm with heavy hail
};