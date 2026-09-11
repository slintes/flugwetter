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

| File | Package | Version | SHA-256 |
|---|---|---|---|
| `chart.umd.min.js` | [chart.js](https://www.chartjs.org/) | 4.5.1 | `48444a82d4edcb5bec0f1965faacdde18d9c17db3063d042abada2f705c9f54a` |
| `chartjs-adapter-date-fns.bundle.min.js` | [chartjs-adapter-date-fns](https://github.com/chartjs/chartjs-adapter-date-fns) | 3.0.0 | `ea7ab30d26c38dcf1f2d26bb43e73a94537b58f1906f55e1a546dd09321b5615` |
| `leaflet.js` | [Leaflet](https://leafletjs.com/) | 1.9.4 | `db49d009c841f5ca34a888c96511ae936fd9f5533e90d8b2c4d57596f4e5641a` |
| `leaflet.css` | [Leaflet](https://leafletjs.com/) | 1.9.4 | `a7837102824184820dfa198d1ebcd109ff6d0ff9a2672a074b9a1b4d147d04c6` |

All three were the current stable releases when vendored (2026-08-04); the hashes above were
verified against jsDelivr on 2026-09-11 and still match byte for byte. Integrity is one of
the three reasons this directory exists at all (see above) -- without a recorded hash that
claim cannot be rechecked, only re-asserted.

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
sha256sum *.js *.css
```

Update the table above with the new version and the `sha256sum` output, then run
`make test`, and load the page to confirm the charts and the map picker still render --
these are pinned majors, so an upgrade is a real change.
