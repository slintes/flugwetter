package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServeOpenAIPTileRejectsBadCoordinates(t *testing.T) {
	// A valid key would let a bad request through to openAIP; these must be rejected here.
	t.Setenv(openAIPKeyEnv, "test-key")

	tests := []struct {
		name string
		path string
	}{
		{"non-numeric zoom", "/api/tiles/openaip/x/1/1.png"},
		// The stdlib route matches the whole final segment, so the .png is validated in
		// the handler rather than by the pattern.
		{"missing .png extension", "/api/tiles/openaip/7/66/41"},
		{"non-numeric y with a valid extension", "/api/tiles/openaip/7/66/abc.png"},
		{"zoom above the map maximum", "/api/tiles/openaip/20/1/1.png"},
		{"negative zoom", "/api/tiles/openaip/-1/1/1.png"},
		// At zoom 2 the grid is 4x4, so x=4 does not exist.
		{"x outside the grid", "/api/tiles/openaip/2/4/1.png"},
		{"y outside the grid", "/api/tiles/openaip/2/1/4.png"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			newTileRouter().ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

// TestServeOpenAIPTileServesFromCache proves the cache short-circuits before any network
// call: with a bogus API key configured, a hit can only come from the cache.
func TestServeOpenAIPTileServesFromCache(t *testing.T) {
	t.Setenv(openAIPKeyEnv, "test-key")

	body := []byte("fake-png-bytes")
	openAIPTiles.put("7/66/41", body)
	t.Cleanup(func() { openAIPTiles.entries = make(map[string]cachedTile) })

	req := httptest.NewRequest(http.MethodGet, "/api/tiles/openaip/7/66/41.png", nil)
	rec := httptest.NewRecorder()

	newTileRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != string(body) {
		t.Errorf("body = %q, want %q", got, string(body))
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
}

func TestTileCacheExpiry(t *testing.T) {
	c := &tileCache{entries: make(map[string]cachedTile)}
	c.entries["1/1/1"] = cachedTile{body: []byte("stale"), fetchedAt: time.Now().Add(-2 * tileCacheTTL)}

	if _, ok := c.get("1/1/1"); ok {
		t.Error("expired tile was served from the cache")
	}
}

func TestTileCacheBounded(t *testing.T) {
	c := &tileCache{entries: make(map[string]cachedTile)}
	for i := 0; i < tileCacheMaxEntries+10; i++ {
		c.put(string(rune(i)), []byte("x"))
	}

	if len(c.entries) > tileCacheMaxEntries {
		t.Errorf("cache holds %d entries, want at most %d", len(c.entries), tileCacheMaxEntries)
	}
}

func TestOpenAIPEnabled(t *testing.T) {
	t.Setenv(openAIPKeyEnv, "")
	if openAIPEnabled() {
		t.Error("openAIPEnabled() = true without an API key")
	}

	t.Setenv(openAIPKeyEnv, "some-key")
	if !openAIPEnabled() {
		t.Error("openAIPEnabled() = false with an API key set")
	}
}
