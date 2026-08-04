# Frontend tests

Deliberately outside `../frontend/`, because that whole directory is embedded into the
binary by `//go:embed all:frontend`. Test files living beside the modules were compiled in
and served to browsers — and listed in the generated import map.

`node --test` over the modules that import neither Chart.js nor the DOM: `time.js`,
`barbs.js` and `viewport.js` (which needs only a two-line `window` stub).

```bash
make test          # both suites
node --test 'internal/web/jstest/*.test.js'
```
