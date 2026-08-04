// Package web owns the browser-facing assets: the single HTML page, the ES modules, the
// stylesheet and the weather icons.
//
// It is its own package because of a go:embed rule -- an embed pattern may not leave its own
// package directory, so the files have to live under whichever package embeds them. Keeping
// that package separate from internal/server means the server deals in handlers rather than
// in file paths.
package web

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
)

// frontendFS is the whole frontend, compiled into the binary.
//
// `all:` is required. Without it embed silently skips files and directories whose names
// begin with "." or "_", which is the kind of omission that only surfaces as a 404 in
// production.
//
//go:embed all:frontend
var frontendFS embed.FS

// devModeEnv switches to serving the frontend from disk instead of from the binary.
// Frontend edits are far more frequent than Go ones, and rebuilding to see a CSS change is
// not a workflow.
const devModeEnv = "FLUGWETTER_DEV"

// devDir is where the assets live relative to the repository root, which is where `go run .`
// and `make dev` are invoked from.
const devDir = "internal/web/frontend"

// DevMode reports whether the frontend is being served from disk.
func DevMode() bool {
	return os.Getenv(devModeEnv) != ""
}

// Root returns the frontend as a filesystem rooted so paths are "index.html" rather than
// "frontend/index.html".
func Root() fs.FS {
	if DevMode() {
		slog.Warn("serving the frontend from disk", "env", devModeEnv, "dir", devDir)
		return os.DirFS(devDir)
	}

	sub, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		// Only reachable if the embed directive and this path disagree, which is a build
		// error rather than anything a deployment can cause.
		panic("frontend not embedded: " + err.Error())
	}
	return sub
}

// StaticHandler serves the frontend under /static/.
func StaticHandler(root fs.FS) http.Handler {
	return http.StripPrefix("/static/", http.FileServerFS(root))
}

// IndexHandler serves the single HTML page.
func IndexHandler(root fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := fs.ReadFile(root, "index.html")
		if err != nil {
			slog.Error("failed to read index.html", "error", err)
			http.Error(w, "Failed to load page", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write(body); err != nil {
			slog.Error("failed to write index.html", "error", err)
		}
	})
}
