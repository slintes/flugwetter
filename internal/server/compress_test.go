package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func gzipHandler(contentType, body string) http.Handler {
	return gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = io.WriteString(w, body)
	}))
}

func TestGzipMiddleware(t *testing.T) {
	large := strings.Repeat(`{"time":"2026-08-04T12:00","temperature":20.7},`, 200)

	t.Run("compresses a large JSON response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/weather", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()

		gzipHandler("application/json", large).ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("Content-Encoding = %q, want gzip", got)
		}
		// Caches must not hand a gzipped body to a client that did not ask for one.
		if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
			t.Errorf("Vary = %q, want it to include Accept-Encoding", got)
		}

		zr, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatalf("response is not valid gzip: %v", err)
		}
		got, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("failed to decompress: %v", err)
		}
		if string(got) != large {
			t.Error("decompressed body does not match what the handler wrote")
		}
		if rec.Body.Len() >= len(large) {
			t.Errorf("compressed to %d bytes from %d -- no saving", rec.Body.Len(), len(large))
		}
	})

	t.Run("leaves the body alone without Accept-Encoding", func(t *testing.T) {
		rec := httptest.NewRecorder()
		gzipHandler("application/json", large).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/weather", nil))

		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("Content-Encoding = %q, want empty", got)
		}
		if rec.Body.String() != large {
			t.Error("body was altered for a client that did not ask for gzip")
		}
	})

	// Below a packet's worth the gzip header and the CPU cost buy nothing.
	t.Run("skips a small response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()

		gzipHandler("application/json", `{"ok":true}`).ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("Content-Encoding = %q, want empty for a tiny body", got)
		}
		if rec.Body.String() != `{"ok":true}` {
			t.Errorf("body = %q, want it passed through untouched", rec.Body.String())
		}
	})

	// PNG tiles are already compressed; gzipping them costs CPU and saves nothing.
	t.Run("skips an already-compressed type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/tiles/openaip/7/66/41.png", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()

		gzipHandler("image/png", strings.Repeat("\x89PNG fake bytes ", 200)).ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("Content-Encoding = %q, want empty for image/png", got)
		}
	})

	// The regression this guards: http.ServeContent, which serves every embedded asset,
	// calls WriteHeader before its first Write. Forwarding that status immediately commits
	// the headers, so Content-Encoding was set too late to be sent -- the body went out
	// gzipped with nothing telling the browser so, which is unreadable rather than merely
	// uncompressed.
	t.Run("sets Content-Encoding when the handler calls WriteHeader first", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/static/js/charts.js", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()

		gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, large)
		})).ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("Content-Encoding = %q, want gzip -- a gzipped body without this header is unreadable", got)
		}

		zr, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatalf("response is not valid gzip: %v", err)
		}
		got, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("failed to decompress: %v", err)
		}
		if string(got) != large {
			t.Error("decompressed body does not match")
		}
	})

	// A Content-Length describing the uncompressed body would be wrong once compressed.
	t.Run("drops a stale Content-Length when compressing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/static/js/charts.js", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()

		gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", strconv.Itoa(len(large)))
			_, _ = io.WriteString(w, large)
		})).ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Length"); got != "" {
			t.Errorf("Content-Length = %q, want it removed once the body is compressed", got)
		}
	})

	t.Run("preserves a non-200 status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/weather", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()

		gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Unknown airport", http.StatusBadRequest)
		})).ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})
}

func TestCompressibleType(t *testing.T) {
	for _, tc := range []struct {
		contentType string
		want        bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"text/html; charset=utf-8", true},
		{"text/css", true},
		{"text/javascript; charset=utf-8", true},
		{"image/svg+xml", true},
		{"image/png", false},
		{"application/octet-stream", false},
		{"", false},
	} {
		if got := compressibleType(tc.contentType); got != tc.want {
			t.Errorf("compressibleType(%q) = %v, want %v", tc.contentType, got, tc.want)
		}
	}
}

func TestAcceptsGzip(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   bool
	}{
		{"gzip", true},
		{"gzip, deflate, br", true},
		{"br, gzip", true},
		{"gzip;q=1.0, identity;q=0.5", true},
		{"deflate", false},
		{"", false},
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept-Encoding", tc.header)
		if got := acceptsGzip(req); got != tc.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}
