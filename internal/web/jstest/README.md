# Frontend tests

Deliberately outside `../frontend/`, because that whole directory is embedded into the
binary by `//go:embed all:frontend`. Test files living beside the modules were compiled in
and served to browsers — and listed in the generated import map.

`node --test` covers the pure frontend modules, with small browser stubs where needed.
`chart-tooltips.test.js` also captures the four chart configurations using lightweight
Chart.js/DOM stubs, so an accidental per-chart animation override is caught. Actual
animation and chart layout still need a browser check.

```bash
make test          # both suites
node --test 'internal/web/jstest/*.test.js'
```
