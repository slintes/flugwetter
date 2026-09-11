package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsBlockedStaticPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/static/", true},
		{"/static/js/", true},
		{"/static/vendor/", true},
		{"/static/index.html", true},
		{"/static/js/index.html", true},
		{"/static/js/main.js", false},
		{"/static/styles.css", false},
		{"/static/icons/clear-day.svg", false},
	}

	for _, tc := range tests {
		if got := isBlockedStaticPath(tc.path); got != tc.want {
			t.Errorf("isBlockedStaticPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func newTestAssetsHandler(t *testing.T) http.Handler {
	t.Helper()
	assets, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	return assets.StaticHandler()
}

func TestStaticHandler_RejectsDirectoryRequests(t *testing.T) {
	handler := newTestAssetsHandler(t)

	for _, path := range []string{"/static/", "/static/js/", "/static/vendor/", "/static/icons/"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
		})
	}
}

// The regression this guards: /static/index.html used to serve the *unrendered* template --
// {{asset "..."}} calls and the {{.ImportMap}} placeholder still in it -- as text/html.
func TestStaticHandler_RejectsIndexHTML(t *testing.T) {
	handler := newTestAssetsHandler(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/index.html", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestStaticHandler_ServesARealAsset(t *testing.T) {
	handler := newTestAssetsHandler(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/js/main.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() == 0 {
		t.Error("got an empty body for a real asset")
	}
}

// A hashed URL -- the ?v= query matching the file's real content hash -- is cached
// immutably; anything else (unhashed, or a stale/wrong hash) gets the short TTL. Both must
// still serve the file; cacheControl only changes the header, never the response body.
func TestStaticHandler_CachingByHash(t *testing.T) {
	assets, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	handler := assets.StaticHandler()

	hashedURL := assets.URL("js/main.js") // "/static/js/main.js?v=<hash>"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, hashedURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got == "" || got == "public, max-age=3600" {
		t.Errorf("Cache-Control = %q for a correctly hashed URL, want immutable", got)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/js/main.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d for the unhashed URL", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q for an unhashed URL, want the short TTL", got)
	}
}
