Goal
====

This a weather application. It reads a detailed weather forcast from an API in json format,
and displays the forecast on a webpage.

Archtitecture
=============

The application should consist of a backend and a frontend.
The backend is implemented in Go. It reads the weather data, caches the result for 15 minutes,
and prepares the data for being displayed nicely on a webpage. Use the hourly data only for now.

The frontend is a webpage, displaying the weather forecast.
It should consists of multiple graphs. The x axis is the the time.

The first graph's y axis is temperature in celcius, and will show the 2 meter temperatur as a line.

The second graph's x axis is height in feet. It should show symbols for the cloud cover at different heights over time.

Details
=======

This is the API endpoint which provides the weather data:
https://api.open-meteo.com/v1/forecast?latitude=52.13499946&longitude=7.684830594&hourly=precipitation_probability,pressure_msl,cloud_cover_low,cloud_cover,cloud_cover_mid,cloud_cover_high,wind_speed_120m,wind_speed_180m,wind_direction_120m,wind_direction_180m,temperature_180m,temperature_120m,temperature_2m,relative_humidity_2m,dew_point_2m,apparent_temperature,precipitation,rain,showers,snowfall,snow_depth,weather_code,surface_pressure,visibility,wind_speed_10m,wind_speed_80m,wind_direction_10m,wind_direction_80m,wind_gusts_10m,temperature_80m,cloud_cover_1000hPa,cloud_cover_975hPa,cloud_cover_950hPa,cloud_cover_925hPa,cloud_cover_900hPa,cloud_cover_850hPa,cloud_cover_800hPa,cloud_cover_700hPa,cloud_cover_600hPa,cloud_cover_500hPa,cloud_cover_400hPa,cloud_cover_300hPa,cloud_cover_200hPa,cloud_cover_250hPa,cloud_cover_150hPa,cloud_cover_100hPa,cloud_cover_70hPa,cloud_cover_50hPa,cloud_cover_30hPa,wind_speed_1000hPa,wind_speed_975hPa,wind_speed_950hPa,wind_speed_925hPa,wind_speed_900hPa,wind_speed_850hPa,wind_speed_800hPa,wind_speed_700hPa,wind_speed_600hPa,wind_direction_600hPa,wind_direction_700hPa,wind_direction_800hPa,wind_direction_850hPa,wind_direction_900hPa,wind_direction_925hPa,wind_direction_950hPa,wind_direction_975hPa,wind_direction_1000hPa,geopotential_height_1000hPa,geopotential_height_975hPa,geopotential_height_950hPa,geopotential_height_925hPa,geopotential_height_900hPa,geopotential_height_850hPa,geopotential_height_800hPa,geopotential_height_700hPa,geopotential_height_600hPa,geopotential_height_500hPa,geopotential_height_400hPa,geopotential_height_300hPa,geopotential_height_250hPa,geopotential_height_200hPa,geopotential_height_150hPa,geopotential_height_100hPa,geopotential_height_70hPa,geopotential_height_50hPa,geopotential_height_30hPa&models=icon_seamless&minutely_15=precipitation,rain,snowfall,weather_code,wind_speed_10m,wind_speed_80m,wind_direction_10m,wind_direction_80m,wind_gusts_10m,visibility,cape,lightning_potential&timezone=auto&wind_speed_unit=kn&forecast_minutely_15=96

