// Mapping from WMO weather codes to icon filenames
// Icons: Visual Crossing WeatherIcons, 2nd Set - Color (GNU LGPL)
// https://github.com/visualcrossing/WeatherIcons
const weatherCodeToIcon = {
    "0": "clear-day.svg",                      // Clear sky
    "1": "partly-cloudy-day.svg",              // Mainly clear
    "2": "partly-cloudy-day.svg",              // Partly cloudy
    "3": "cloudy.svg",                         // Overcast
    "45": "fog.svg",                           // Fog
    "48": "fog.svg",                           // Depositing rime fog
    "51": "showers-day.svg",                   // Drizzle: Light
    "53": "showers-day.svg",                   // Drizzle: Moderate
    "55": "rain.svg",                          // Drizzle: Dense
    "56": "sleet.svg",                         // Freezing Drizzle: Light
    "57": "sleet.svg",                         // Freezing Drizzle: Dense
    "61": "showers-day.svg",                   // Rain: Slight
    "63": "rain.svg",                          // Rain: Moderate
    "65": "rain.svg",                          // Rain: Heavy
    "66": "sleet.svg",                         // Freezing Rain: Light
    "67": "sleet.svg",                         // Freezing Rain: Heavy
    "71": "snow-showers-day.svg",              // Snow fall: Slight
    "73": "snow.svg",                          // Snow fall: Moderate
    "75": "snow.svg",                          // Snow fall: Heavy
    "77": "hail.svg",                          // Snow grains
    "80": "showers-day.svg",                   // Rain showers: Slight
    "81": "showers-day.svg",                   // Rain showers: Moderate
    "82": "rain.svg",                          // Rain showers: Violent
    "85": "snow-showers-day.svg",              // Snow showers: Slight
    "86": "snow.svg",                          // Snow showers: Heavy
    "95": "thunder-showers-day.svg",           // Thunderstorm: Slight or moderate
    "96": "thunder-rain.svg",                  // Thunderstorm with slight hail
    "99": "thunder-rain.svg",                  // Thunderstorm with heavy hail
    "0-night": "clear-night.svg",              // Clear sky (night)
    "1-night": "partly-cloudy-night.svg",      // Mainly clear (night)
    "2-night": "partly-cloudy-night.svg",      // Partly cloudy (night)
    "3-night": "cloudy.svg",                   // Overcast (night)
    "45-night": "fog.svg",                     // Fog (night)
    "48-night": "fog.svg",                     // Depositing rime fog (night)
    "51-night": "showers-night.svg",           // Drizzle: Light (night)
    "53-night": "showers-night.svg",           // Drizzle: Moderate (night)
    "55-night": "rain.svg",                    // Drizzle: Dense (night)
    "56-night": "sleet.svg",                   // Freezing Drizzle: Light (night)
    "57-night": "sleet.svg",                   // Freezing Drizzle: Dense (night)
    "61-night": "showers-night.svg",           // Rain: Slight (night)
    "63-night": "rain.svg",                    // Rain: Moderate (night)
    "65-night": "rain.svg",                    // Rain: Heavy (night)
    "66-night": "sleet.svg",                   // Freezing Rain: Light (night)
    "67-night": "sleet.svg",                   // Freezing Rain: Heavy (night)
    "71-night": "snow-showers-night.svg",      // Snow fall: Slight (night)
    "73-night": "snow.svg",                    // Snow fall: Moderate (night)
    "75-night": "snow.svg",                    // Snow fall: Heavy (night)
    "77-night": "hail.svg",                    // Snow grains (night)
    "80-night": "showers-night.svg",           // Rain showers: Slight (night)
    "81-night": "showers-night.svg",           // Rain showers: Moderate (night)
    "82-night": "rain.svg",                    // Rain showers: Violent (night)
    "85-night": "snow-showers-night.svg",      // Snow showers: Slight (night)
    "86-night": "snow.svg",                    // Snow showers: Heavy (night)
    "95-night": "thunder-showers-night.svg",   // Thunderstorm: Slight or moderate (night)
    "96-night": "thunder-rain.svg",            // Thunderstorm with slight hail (night)
    "99-night": "thunder-rain.svg"             // Thunderstorm with heavy hail (night)
};
