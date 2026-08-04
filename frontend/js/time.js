// Open-Meteo returns naive-UTC timestamps ("2026-08-04T12:00") under timezone=GMT.
//
// The trailing "Z" is load-bearing: without it the browser parses the string as *local*
// time, which silently shifts the entire forecast by the UTC offset. This was repeated at
// ten call sites in updateCharts, each one an opportunity to forget it.

export function toEpochMs(time) {
    return new Date(time + 'Z').getTime();
}
