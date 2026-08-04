package server

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"

	"flugwetter/internal/web"
)

// Content-Security-Policy, now that the vendored libraries mean nothing on the page comes
// from a third party.
//
// Two directives are looser than 'self' and both are forced by something real:
//
//   - img-src also allows the OpenStreetMap tile host. It is the one remaining cross-origin
//     request, and only once the map picker is opened. The openAIP overlay is proxied
//     through this server, so it needs nothing here.
//   - style-src allows 'unsafe-inline'. Leaflet sets element styles from JavaScript for
//     every tile, marker and popup -- positioning is inline style by design -- and a nonce
//     cannot cover style attributes. script-src is the directive that actually constrains
//     code execution, and that one stays strict.
//
// script-src has no 'unsafe-inline': the inline onclick= attributes were removed when
// app.js was split into modules, and the one inline script left is the generated import
// map, which is covered by a per-response nonce.
const (
	osmTileHost = "https://tile.openstreetmap.org"

	cspTemplate = "default-src 'self'; " +
		"script-src 'self' 'nonce-%s'; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: " + osmTileHost + "; " +
		"connect-src 'self'; " +
		"font-src 'self'; " +
		"object-src 'none'; " +
		"base-uri 'none'; " +
		"form-action 'none'; " +
		"frame-ancestors 'none'"
)

// securityHeaders sets the response headers that do not depend on the page content.
//
// The CSP itself is set by the index handler, which is the only response that needs a nonce
// and the only one that is a document.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Stops a browser from second-guessing Content-Type, which is what turns an uploaded
		// or proxied file into an executable script.
		h.Set("X-Content-Type-Options", "nosniff")
		// Referrers leak the ?airport= parameter to the tile host otherwise.
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Belt and braces alongside frame-ancestors, for anything that predates CSP.
		h.Set("X-Frame-Options", "DENY")

		next.ServeHTTP(w, r)
	})
}

// newNonce returns a fresh base64 nonce for one response.
//
// It must be unpredictable and never reused: a nonce an attacker can guess, or one that is
// stable across responses, makes the script-src allowance worthless.
func newNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate a CSP nonce: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// indexHandler serves the HTML page under a per-response CSP nonce.
//
// The nonce goes into both the header and the import map's script tag; if they ever
// disagree the browser drops the import map and nothing loads, so they are produced here
// together rather than in two places.
func indexHandler(assets *web.Assets) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce, err := newNonce()
		if err != nil {
			slog.Error("failed to build the page", "error", err)
			http.Error(w, "Failed to load page", http.StatusInternalServerError)
			return
		}

		body, err := assets.Index(nonce)
		if err != nil {
			slog.Error("failed to render index.html", "error", err)
			http.Error(w, "Failed to load page", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Security-Policy", fmt.Sprintf(cspTemplate, nonce))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Always revalidated: this is the document carrying the hashed asset URLs, so a
		// cached copy would keep pointing at the previous deployment's assets.
		w.Header().Set("Cache-Control", "no-cache")

		if _, err := w.Write(body); err != nil {
			slog.Error("failed to write index.html", "error", err)
		}
	})
}
