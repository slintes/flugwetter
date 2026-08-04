package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// openAIPKeyEnv holds the openAIP API key. Without it the tile proxy is not registered and
// the airport map falls back to the OpenStreetMap base layer alone.
const openAIPKeyEnv = "OPENAIP_API_KEY"

const (
	// openAIPTileURL is the XYZ raster endpoint of the openAIP Tiles API.
	openAIPTileURL = "https://api.tiles.openaip.net/api/data/openaip/%d/%d/%d.png?apiKey=%s"
	// tileCacheTTL is generous on purpose: aeronautical data changes on AIRAC cycles,
	// not by the hour, and openAIP asks clients to cache to stay inside its rate limits.
	tileCacheTTL = 24 * time.Hour
	// tileCacheMaxEntries bounds memory. Tiles are a few KB each, so this is tens of MB
	// at worst. The cache is dropped wholesale when full rather than evicted per entry --
	// at this size an LRU would be more machinery than the problem deserves.
	tileCacheMaxEntries = 2000
	// tileMaxZoom mirrors what the map lets the user reach; anything beyond it is a bug
	// or a scraper, and is rejected before it reaches openAIP.
	tileMaxZoom = 14
)

type cachedTile struct {
	body      []byte
	fetchedAt time.Time
}

type tileCache struct {
	entries map[string]cachedTile
	mutex   sync.RWMutex
}

var openAIPTiles = &tileCache{entries: make(map[string]cachedTile)}

func (c *tileCache) get(key string) ([]byte, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	entry, ok := c.entries[key]
	if !ok || time.Since(entry.fetchedAt) > tileCacheTTL {
		return nil, false
	}
	return entry.body, true
}

func (c *tileCache) put(key string, body []byte) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if len(c.entries) >= tileCacheMaxEntries {
		c.entries = make(map[string]cachedTile)
	}
	c.entries[key] = cachedTile{body: body, fetchedAt: time.Now()}
}

// openAIPEnabled reports whether an API key is configured.
func openAIPEnabled() bool {
	return openAIPAPIKey() != ""
}

func openAIPAPIKey() string {
	return os.Getenv(openAIPKeyEnv)
}

// tileRoute is the proxy path, including the method, so the server and the tests agree on
// both. Wildcards are stdlib ServeMux patterns, read back with r.PathValue.
//
// The final segment is {y} rather than {y}.png because a stdlib wildcard must span a whole
// path segment -- "{y}.png" is rejected at registration. The served URL is unchanged: the
// handler takes "41.png" and strips the extension itself, so Leaflet's tile template and
// any cached URLs keep working.
const tileRoute = "GET /api/tiles/openaip/{z}/{x}/{y}"

// tileExtension is required in the request path, so the route does not also answer to
// /api/tiles/openaip/7/66/41 with no extension.
const tileExtension = ".png"

// newTileRouter returns a router serving only the tile proxy.
func newTileRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(tileRoute, serveOpenAIPTile)
	return mux
}

// serveOpenAIPTile proxies one tile from openAIP, attaching the API key server-side so it
// is never shipped to the browser, and caching the result.
func serveOpenAIPTile(w http.ResponseWriter, r *http.Request) {
	yParam := r.PathValue("y")
	if !strings.HasSuffix(yParam, tileExtension) {
		http.Error(w, "Invalid tile coordinates", http.StatusBadRequest)
		return
	}

	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(strings.TrimSuffix(yParam, tileExtension))
	if errZ != nil || errX != nil || errY != nil {
		http.Error(w, "Invalid tile coordinates", http.StatusBadRequest)
		return
	}
	if z < 0 || z > tileMaxZoom {
		http.Error(w, "Zoom level out of range", http.StatusBadRequest)
		return
	}
	// x and y must be inside the tile grid for this zoom, or upstream is being asked for
	// tiles that cannot exist.
	limit := 1 << z
	if x < 0 || x >= limit || y < 0 || y >= limit {
		http.Error(w, "Tile coordinates out of range", http.StatusBadRequest)
		return
	}

	key := fmt.Sprintf("%d/%d/%d", z, x, y)
	if body, ok := openAIPTiles.get(key); ok {
		writeTile(w, body)
		return
	}

	url := fmt.Sprintf(openAIPTileURL, z, x, y, openAIPAPIKey())
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		slog.Error("failed to build openAIP tile request", "tile", key, "error", err)
		http.Error(w, "Failed to fetch tile", http.StatusBadGateway)
		return
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Error("failed to fetch openAIP tile", "tile", key, "error", err)
		http.Error(w, "Failed to fetch tile", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Outside the data area openAIP answers 204; Leaflet handles an empty response fine.
	if resp.StatusCode == http.StatusNoContent {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if resp.StatusCode != http.StatusOK {
		slog.Error("openAIP returned an unexpected status", "status", resp.StatusCode, "tile", key)
		http.Error(w, "Failed to fetch tile", http.StatusBadGateway)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("failed to read openAIP tile", "tile", key, "error", err)
		http.Error(w, "Failed to fetch tile", http.StatusBadGateway)
		return
	}

	openAIPTiles.put(key, body)
	writeTile(w, body)
}

func writeTile(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "image/png")
	// Let the browser cache too, so panning back over the map costs nothing.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if _, err := w.Write(body); err != nil {
		slog.Error("failed to write tile response", "error", err)
	}
}
