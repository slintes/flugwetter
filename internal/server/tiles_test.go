package server

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
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
	withNoAirportBounds(t)
	t.Setenv(openAIPKeyEnv, "test-key")

	body := []byte("fake-png-bytes")
	openAIPTiles.put("7/66/41", body, false)
	t.Cleanup(func() { openAIPTiles = newTileCache(tileCacheMaxEntries) })

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

// A cached 204 -- a tile outside openAIP's data area -- must be served the same way, with
// no body, rather than falling through to another upstream request.
func TestServeOpenAIPTileServesCachedNoContent(t *testing.T) {
	withNoAirportBounds(t)
	t.Setenv(openAIPKeyEnv, "test-key")

	openAIPTiles.put("7/66/41", nil, true)
	t.Cleanup(func() { openAIPTiles = newTileCache(tileCacheMaxEntries) })

	req := httptest.NewRequest(http.MethodGet, "/api/tiles/openaip/7/66/41.png", nil)
	rec := httptest.NewRecorder()

	newTileRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
}

func TestTileCacheExpiry(t *testing.T) {
	c := newTileCache(10)
	c.put("1/1/1", []byte("stale"), false)

	// Backdate the entry the cache just stored, bypassing put (which always stamps "now")
	// to exercise the TTL check in get.
	c.entries["1/1/1"].Value.(*cachedTile).fetchedAt = time.Now().Add(-2 * tileCacheTTL)

	if _, _, ok := c.get("1/1/1"); ok {
		t.Error("expired tile was served from the cache")
	}
}

// TestTileCacheBounded checks not just the bound but that it is an LRU rather than the
// wholesale-clear it replaced: the most recently inserted entry survives eviction.
func TestTileCacheBounded(t *testing.T) {
	c := newTileCache(tileCacheMaxEntries)
	for i := 0; i < tileCacheMaxEntries+10; i++ {
		c.put(fmt.Sprintf("%d", i), []byte("x"), false)
	}

	if len(c.entries) > tileCacheMaxEntries {
		t.Errorf("cache holds %d entries, want at most %d", len(c.entries), tileCacheMaxEntries)
	}

	last := fmt.Sprintf("%d", tileCacheMaxEntries+9)
	if _, _, ok := c.get(last); !ok {
		t.Errorf("most recently inserted entry %q was evicted", last)
	}
	if _, _, ok := c.get("0"); ok {
		t.Error("earliest entry survived eviction, want the LRU to have dropped it")
	}
}

// A get() moves an entry to the front, so it survives a later eviction that an
// equally-old-but-unread entry would not.
func TestTileCacheGetRefreshesRecency(t *testing.T) {
	c := newTileCache(2)
	c.put("a", []byte("a"), false)
	c.put("b", []byte("b"), false)

	// Touch "a", making "b" the least recently used.
	c.get("a")

	c.put("c", []byte("c"), false)

	if _, _, ok := c.get("b"); ok {
		t.Error("least recently used entry survived, want it evicted")
	}
	if _, _, ok := c.get("a"); !ok {
		t.Error("recently touched entry was evicted")
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

// stubTileFetch points the network call at a function under the test's control, mirroring
// stubFetchWeather and friends elsewhere in this package.
func stubTileFetch(t *testing.T, fn func(ctx context.Context, z, x, y int) ([]byte, bool, error)) {
	t.Helper()
	original := fetchOpenAIPTileFn
	fetchOpenAIPTileFn = fn
	t.Cleanup(func() { fetchOpenAIPTileFn = original })
}

// withNoAirportBounds clears the package-level airport list for the duration of the test, so
// tileInBounds falls back to permissive. Used by tests that are not exercising the bounds
// check itself and would otherwise depend on incidental test-ordering state.
func withNoAirportBounds(t *testing.T) {
	t.Helper()
	prev := airports
	airports = nil
	t.Cleanup(func() { airports = prev })
}

// latLonToTile is tileLatLon's inverse, built only for these tests: given a coordinate,
// which slippy tile contains it. Standard Web Mercator tile math, same source as
// tileLatLon's own doc comment.
func latLonToTile(lat, lon float64, z int) (x, y int) {
	n := math.Exp2(float64(z))
	x = int((lon + 180) / 360 * n)
	latRad := lat * math.Pi / 180
	y = int((1 - math.Log(math.Tan(latRad)+1/math.Cos(latRad))/math.Pi) / 2 * n)
	return x, y
}

func TestTileInBounds(t *testing.T) {
	withTestAirports(t) // a fixture airport in north-west Germany; see main_test.go

	// The airfield itself, at a zoom deep enough that the tile sits nowhere near the
	// padded bounding box edge.
	z := 10
	x, y := latLonToTile(testAirport.Latitude, testAirport.Longitude, z)
	if !tileInBounds(z, x, y) {
		t.Error("tile covering the configured airfield was rejected")
	}

	// Tokyo, nowhere near north-west Germany at any sane margin.
	tx, ty := latLonToTile(35.68, 139.69, z)
	if tileInBounds(z, tx, ty) {
		t.Error("tile over Tokyo was accepted")
	}
}

func TestTileInBounds_PermissiveBeforeAirportsAreLoaded(t *testing.T) {
	withNoAirportBounds(t)

	if !tileInBounds(10, 0, 0) {
		t.Error("tileInBounds rejected a tile while the airport list is empty, want permissive")
	}
}

// TestServeOpenAIPTileRejectsOutOfBoundsCoordinates checks the handler wires tileInBounds in
// ahead of any upstream call. The stub returning an error guarantees the test stays
// hermetic (no real network call) even if the bounds check were to regress; the assertion on
// StatusNotFound is what actually proves it fired.
func TestServeOpenAIPTileRejectsOutOfBoundsCoordinates(t *testing.T) {
	withTestAirports(t)
	t.Setenv(openAIPKeyEnv, "test-key")
	stubTileFetch(t, func(context.Context, int, int, int) ([]byte, bool, error) {
		return nil, false, fmt.Errorf("upstream must not be called for an out-of-bounds tile")
	})

	z := 10
	tx, ty := latLonToTile(35.68, 139.69, z) // Tokyo
	path := fmt.Sprintf("/api/tiles/openaip/%d/%d/%d.png", z, tx, ty)

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	newTileRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// captureSlog redirects the default slog logger into buf for the duration of the test.
func captureSlog(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
}

// TestServeOpenAIPTileFailureDoesNotLeakTheKey guards the regression this whole change
// exists to prevent: even if some future edit puts the key back in the URL, redactedError
// must strip it before the failing fetch reaches the log. Built on a real *url.Error, which
// is the concrete type http.Client.Do returns and the one redactedError type-asserts for.
func TestServeOpenAIPTileFailureDoesNotLeakTheKey(t *testing.T) {
	withNoAirportBounds(t)
	const secretKey = "super-secret-openaip-key"
	t.Setenv(openAIPKeyEnv, secretKey)

	stubTileFetch(t, func(context.Context, int, int, int) ([]byte, bool, error) {
		err := &url.Error{
			Op:  "Get",
			URL: "https://api.tiles.openaip.net/api/data/openaip/5/5/5.png?apiKey=" + secretKey,
			Err: fmt.Errorf("dial tcp: i/o timeout"),
		}
		return nil, false, err
	})

	var logBuf bytes.Buffer
	captureSlog(t, &logBuf)

	req := httptest.NewRequest(http.MethodGet, "/api/tiles/openaip/5/5/5.png", nil)
	rec := httptest.NewRecorder()
	newTileRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if bytes.Contains(logBuf.Bytes(), []byte(secretKey)) {
		t.Errorf("log output contains the API key: %s", logBuf.String())
	}
}

// TestTileFetchCoalesced proves concurrent requests for one uncached tile produce exactly
// one upstream call.
func TestTileFetchCoalesced(t *testing.T) {
	withNoAirportBounds(t)
	t.Setenv(openAIPKeyEnv, "test-key")
	openAIPTiles = newTileCache(tileCacheMaxEntries)
	t.Cleanup(func() { openAIPTiles = newTileCache(tileCacheMaxEntries) })

	var calls int32
	release := make(chan struct{})
	stubTileFetch(t, func(context.Context, int, int, int) ([]byte, bool, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		return []byte("tile-bytes"), false, nil
	})

	const n = 10
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/tiles/openaip/5/5/5.png", nil)
			rec := httptest.NewRecorder()
			newTileRouter().ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}

	// Give every goroutine time to reach the singleflight call before releasing it, or
	// this would just as easily prove nothing ran concurrently.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("upstream called %d times, want 1", got)
	}
	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("request %d: status = %d, want %d", i, code, http.StatusOK)
		}
	}
}
