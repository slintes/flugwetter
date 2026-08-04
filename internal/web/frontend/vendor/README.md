# Vendored browser libraries

Served same-origin from `/static/vendor/` and embedded in the binary, rather than loaded
from jsDelivr.

Three reasons:

- **Availability.** A CDN that cannot be reached — captive portal, weak mobile signal, a
  corporate proxy — rendered a blank page. For a tool consulted at an airfield that is the
  wrong failure mode.
- **Integrity.** The CDN tags carried no Subresource Integrity hash, so a compromised or
  hijacked CDN could execute arbitrary JavaScript on the page.
- **Content-Security-Policy.** With everything same-origin, `default-src 'self'` becomes
  achievable.

## Contents

| File | Package | Version |
|---|---|---|
| `chart.umd.min.js` | [chart.js](https://www.chartjs.org/) | 4.5.1 |
| `chartjs-adapter-date-fns.bundle.min.js` | [chartjs-adapter-date-fns](https://github.com/chartjs/chartjs-adapter-date-fns) | 3.0.0 |
| `leaflet.js`, `leaflet.css` | [Leaflet](https://leafletjs.com/) | 1.9.4 |

All three were the current stable releases when vendored (2026-08-04).

Leaflet's `images/` directory is deliberately not vendored. The map uses `L.circleMarker`,
which is an SVG path, and adds no layers control, so nothing on the page references
`marker-icon.png` or `layers.png` and the browser never requests them.

## Updating

```bash
cd internal/web/frontend/vendor
base=https://cdn.jsdelivr.net/npm
curl -sfSL "$base/chart.js@<version>/dist/chart.umd.min.js" -o chart.umd.min.js
curl -sfSL "$base/chartjs-adapter-date-fns@<version>/dist/chartjs-adapter-date-fns.bundle.min.js" \
     -o chartjs-adapter-date-fns.bundle.min.js
curl -sfSL "$base/leaflet@<version>/dist/leaflet.js"  -o leaflet.js
curl -sfSL "$base/leaflet@<version>/dist/leaflet.css" -o leaflet.css
```

Then update the table above and run `make test`, and load the page to confirm the charts
and the map picker still render — these are pinned majors, so an upgrade is a real change.
