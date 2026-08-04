// Package web owns the browser-facing assets: the single HTML page, the ES modules, the
// stylesheet, the vendored libraries and the weather icons.
//
// It is its own package because of a go:embed rule -- an embed pattern may not leave its own
// package directory, so the files have to live under whichever package embeds them. Keeping
// that package separate from internal/server means the server deals in handlers rather than
// in file paths.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// frontendFS is the whole frontend, compiled into the binary.
//
// `all:` is required. Without it embed silently skips files and directories whose names
// begin with "." or "_", which is the kind of omission that only surfaces as a 404 in
// production.
//
//go:embed all:frontend
var frontendFS embed.FS

const (
	// devModeEnv switches to serving the frontend from disk instead of from the binary.
	// Frontend edits are far more frequent than Go ones, and rebuilding to see a CSS change
	// is not a workflow.
	devModeEnv = "FLUGWETTER_DEV"
	// devDir is where the assets live relative to the repository root, which is where
	// `go run .` and `make dev` are invoked from.
	devDir = "internal/web/frontend"

	// staticPrefix is the URL prefix every asset is served under.
	staticPrefix = "/static/"

	// hashLength is how much of the SHA-256 ends up in the URL. 8 hex characters is 32
	// bits: ample when the only thing being distinguished is one build of a file from
	// another.
	hashLength = 8

	// immutableMaxAge is a year, the practical maximum. Safe only because the URL carries
	// a content hash, so a changed file is a different URL rather than a stale cache entry.
	immutableMaxAge = 365 * 24 * time.Hour
	// unhashedMaxAge applies to assets requested without a version, which is the icons:
	// the frontend builds those URLs from the WMO code at runtime. Short enough that a
	// changed icon appears the same day.
	unhashedMaxAge = time.Hour
)

// DevMode reports whether the frontend is being served from disk.
func DevMode() bool {
	return os.Getenv(devModeEnv) != ""
}

// Assets serves the frontend and knows the content hash of every file in it.
type Assets struct {
	root fs.FS
	// hashes maps an asset path ("js/main.js") to the first hashLength hex characters of
	// its SHA-256. Empty in dev mode, where nothing is versioned.
	hashes map[string]string
	// index is the rendered HTML page, with hashed URLs and the import map already in it.
	// Rebuilt per request in dev mode.
	index []byte
}

// New reads the frontend, hashes every file and renders the HTML page.
func New() (*Assets, error) {
	root, err := rootFS()
	if err != nil {
		return nil, err
	}

	a := &Assets{root: root, hashes: map[string]string{}}

	if !DevMode() {
		if a.hashes, err = hashTree(root); err != nil {
			return nil, fmt.Errorf("failed to hash assets: %w", err)
		}
	}

	if a.index, err = a.renderIndex(); err != nil {
		return nil, err
	}
	return a, nil
}

func rootFS() (fs.FS, error) {
	if DevMode() {
		slog.Warn("serving the frontend from disk", "env", devModeEnv, "dir", devDir)
		return os.DirFS(devDir), nil
	}

	sub, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		return nil, fmt.Errorf("frontend not embedded: %w", err)
	}
	return sub, nil
}

// hashTree walks the asset tree and hashes every file.
func hashTree(root fs.FS) (map[string]string, error) {
	hashes := map[string]string{}

	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		f, err := root.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()

		sum := sha256.New()
		if _, err := io.Copy(sum, f); err != nil {
			return err
		}
		hashes[p] = hex.EncodeToString(sum.Sum(nil))[:hashLength]
		return nil
	})

	return hashes, err
}

// URL returns the public URL for an asset, carrying its content hash so the response can be
// cached permanently. A changed file produces a different URL, which is what makes a deploy
// reach browsers that are holding a year-long cache entry.
func (a *Assets) URL(assetPath string) string {
	url := staticPrefix + assetPath
	if hash, ok := a.hashes[assetPath]; ok {
		return url + "?v=" + hash
	}
	return url
}

// moduleImportMap maps each ES module's resolved URL to its hashed URL.
//
// Hashing only the entry point is not enough. A module's own `import './api.js'` resolves
// against the module's URL, not the document's, and the query string is not inherited -- so
// every inner import would request an unhashed URL and could not be cached. An import map
// remaps those resolved URLs, which keeps the hashes in exactly one place and leaves the
// module sources free of any versioning.
func (a *Assets) moduleImportMap() (template.HTML, error) {
	imports := map[string]string{}
	for assetPath := range a.hashes {
		if strings.HasPrefix(assetPath, "js/") && strings.HasSuffix(assetPath, ".js") {
			imports[staticPrefix+assetPath] = a.URL(assetPath)
		}
	}
	if len(imports) == 0 {
		return "", nil
	}

	encoded, err := json.Marshal(map[string]any{"imports": imports})
	if err != nil {
		return "", fmt.Errorf("failed to build the import map: %w", err)
	}

	// The content is generated here from paths we control, never from request input.
	return template.HTML(`<script type="importmap">` + string(encoded) + `</script>`), nil
}

// renderIndex fills index.html's asset URLs in.
func (a *Assets) renderIndex() ([]byte, error) {
	raw, err := fs.ReadFile(a.root, "index.html")
	if err != nil {
		return nil, fmt.Errorf("failed to read index.html: %w", err)
	}

	// A FuncMap rather than a data field: html/template cannot invoke a struct field with
	// arguments, only a registered function.
	tmpl, err := template.New("index").
		Funcs(template.FuncMap{"asset": a.URL}).
		Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to parse index.html: %w", err)
	}

	importMap, err := a.moduleImportMap()
	if err != nil {
		return nil, err
	}

	var out strings.Builder
	data := struct{ ImportMap template.HTML }{ImportMap: importMap}

	if err := tmpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("failed to render index.html: %w", err)
	}
	return []byte(out.String()), nil
}

// IndexHandler serves the single HTML page.
//
// Always revalidated: it is the one document carrying the hashed URLs, so a cached copy
// would keep pointing at the previous deployment's assets no matter how fresh they are.
func (a *Assets) IndexHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := a.index
		if DevMode() {
			// Pick up edits without a restart.
			rendered, err := a.renderIndex()
			if err != nil {
				slog.Error("failed to render index.html", "error", err)
				http.Error(w, "Failed to load page", http.StatusInternalServerError)
				return
			}
			body = rendered
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if _, err := w.Write(body); err != nil {
			slog.Error("failed to write index.html", "error", err)
		}
	})
}

// StaticHandler serves the assets under /static/.
func (a *Assets) StaticHandler() http.Handler {
	files := http.StripPrefix(staticPrefix, http.FileServerFS(a.root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", a.cacheControl(r))
		files.ServeHTTP(w, r)
	})
}

// cacheControl decides how long an asset may be held.
//
// A version that matches the file's real hash means the URL can only ever refer to these
// bytes, so it is safe to cache forever. A wrong or missing version is not: it might be a
// stale URL from a previous deployment, or an icon URL the frontend built at runtime.
func (a *Assets) cacheControl(r *http.Request) string {
	if DevMode() {
		return "no-store"
	}

	assetPath := strings.TrimPrefix(path.Clean(r.URL.Path), staticPrefix)
	if want, ok := a.hashes[assetPath]; ok && r.URL.Query().Get("v") == want {
		return fmt.Sprintf("public, max-age=%d, immutable", int(immutableMaxAge.Seconds()))
	}
	return fmt.Sprintf("public, max-age=%d", int(unhashedMaxAge.Seconds()))
}
