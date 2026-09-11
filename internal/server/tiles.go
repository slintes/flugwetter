package server

import (
	"container/list"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// openAIPKeyEnv holds the openAIP API key. Without it the tile proxy is not registered and
// the airport map falls back to the OpenStreetMap base layer alone.
const openAIPKeyEnv = "OPENAIP_API_KEY"

const (
	// openAIPTileURL is the XYZ raster endpoint of the openAIP Tiles API. The key travels
	// in a request header, not here -- see openAIPKeyHeader.
	openAIPTileURL = "https://api.tiles.openaip.net/api/data/openaip/%d/%d/%d.png"
	// openAIPKeyHeader is openAIP's documented alternative to the `apiKey` query parameter.
	// It matters here because http.Client wraps a failed request in a *url.Error that embeds
	// the full request URL -- query string included -- and that error is what gets logged.
	// A key in the URL is one upstream timeout away from sitting in this server's log, which
	// the deploy skill has an operator reading routinely. A key in a header is never in that
	// URL, so it is never in that log line either. Confirmed against the live endpoint during
	// review: a request with the header but no apiKey= gets the same
	// "No authenticated user found" 403 as one with neither, meaning the header is read as
	// the credential rather than silently ignored.
	openAIPKeyHeader = "x-openaip-client-id"
	// tileCacheTTL is generous on purpose: aeronautical data changes on AIRAC cycles,
	// not by the hour, and openAIP asks clients to cache to stay inside its rate limits.
	tileCacheTTL = 24 * time.Hour
	// tileCacheMaxEntries bounds memory. Tiles are a few KB each, so this is tens of MB
	// at worst. The cache is an LRU rather than a flush-the-whole-map-at-the-limit design:
	// at 2000 distinct tiles a wholesale clear evicted the entire working set on every
	// single fetch past the limit, which is worse than not bounding it at all.
	tileCacheMaxEntries = 2000
	// tileMaxZoom mirrors what the map lets the user reach; anything beyond it is a bug
	// or a scraper, and is rejected before it reaches openAIP.
	tileMaxZoom = 14
	// maxTileBodyBytes bounds one upstream response. A raster tile is a few KB; openAIP
	// misbehaving or being impersonated should not be able to fill this process's memory
	// through the one route it can push bytes back through.
	maxTileBodyBytes = 2 << 20 // 2 MiB
	// tileBoundsMarginDeg pads the airfields' bounding box so the map can be panned some
	// distance beyond them before a tile is refused. Wide enough for normal use, nowhere
	// near wide enough to make the proxy usable as a general-purpose openAIP mirror -- see
	// tileInBounds.
	tileBoundsMarginDeg = 2.5
)

type cachedTile struct {
	key       string
	body      []byte
	noContent bool // openAIP answered 204: outside its coverage, cached the same as a hit
	fetchedAt time.Time
}

// tileCache is a bounded LRU, not a plain map. Tiles are a few KB each and openAIP's own
// rate limit is the reason to cache at all -- flushing the whole cache in one shot the
// moment a scraper walks past tileCacheMaxEntries distinct coordinates defeated the point of
// having one.
type tileCache struct {
	mutex      sync.Mutex
	entries    map[string]*list.Element // value *cachedTile
	order      *list.List               // front = most recently used
	maxEntries int
}

func newTileCache(maxEntries int) *tileCache {
	return &tileCache{entries: make(map[string]*list.Element), order: list.New(), maxEntries: maxEntries}
}

var openAIPTiles = newTileCache(tileCacheMaxEntries)

// get reports a cache hit and whether it was a cached 204 (outside openAIP's coverage,
// still worth remembering so the upstream is not asked again every single request).
func (c *tileCache) get(key string) (body []byte, noContent bool, ok bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	el, found := c.entries[key]
	if !found {
		return nil, false, false
	}
	entry := el.Value.(*cachedTile)
	if time.Since(entry.fetchedAt) > tileCacheTTL {
		c.order.Remove(el)
		delete(c.entries, key)
		return nil, false, false
	}
	c.order.MoveToFront(el)
	return entry.body, entry.noContent, true
}

func (c *tileCache) put(key string, body []byte, noContent bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	entry := &cachedTile{key: key, body: body, noContent: noContent, fetchedAt: time.Now()}

	if el, found := c.entries[key]; found {
		el.Value = entry
		c.order.MoveToFront(el)
		return
	}

	c.entries[key] = c.order.PushFront(entry)

	if len(c.entries) > c.maxEntries {
		if oldest := c.order.Back(); oldest != nil {
			c.order.Remove(oldest)
			delete(c.entries, oldest.Value.(*cachedTile).key)
		}
	}
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

// airportLatLonBounds returns the bounding box of every configured airfield, padded by
// tileBoundsMarginDeg. ok is false before the airport list is loaded -- startup ordering,
// or a test that never calls loadAirports -- in which case the bound is not yet meaningful
// and tileInBounds must not reject every tile because of it.
func airportLatLonBounds() (minLat, maxLat, minLon, maxLon float64, ok bool) {
	if len(airports) == 0 {
		return 0, 0, 0, 0, false
	}

	minLat, minLon = math.Inf(1), math.Inf(1)
	maxLat, maxLon = math.Inf(-1), math.Inf(-1)
	for _, a := range airports {
		minLat, maxLat = math.Min(minLat, a.Latitude), math.Max(maxLat, a.Latitude)
		minLon, maxLon = math.Min(minLon, a.Longitude), math.Max(maxLon, a.Longitude)
	}

	return minLat - tileBoundsMarginDeg, maxLat + tileBoundsMarginDeg,
		minLon - tileBoundsMarginDeg, maxLon + tileBoundsMarginDeg, true
}

// tileLatLon returns the north-west corner of slippy tile (x, y) at zoom z, in degrees.
// Standard Web Mercator tile math -- see
// https://wiki.openstreetmap.org/wiki/Slippy_map_tilenames#Lon..2Flat._to_tile_numbers_2.
func tileLatLon(z, x, y int) (lat, lon float64) {
	n := math.Exp2(float64(z))
	lon = float64(x)/n*360 - 180
	lat = math.Atan(math.Sinh(math.Pi*(1-2*float64(y)/n))) * 180 / math.Pi
	return lat, lon
}

// tileInBounds reports whether tile (z, x, y) overlaps the airfields' bounding box.
//
// Without this the proxy is a general-purpose openAIP mirror sitting in front of one paid
// key: at zoom 14 alone there are 2^28 addressable tiles, none of which openAIP's own rate
// limiting distinguishes from any other client of this server. Bounding the area this server
// will ever proxy turns that into a few tens of thousands of tiles across every zoom level --
// a set a scraper could at worst pre-warm into the cache, not an open faucet onto the key.
func tileInBounds(z, x, y int) bool {
	minLat, maxLat, minLon, maxLon, ok := airportLatLonBounds()
	if !ok {
		return true
	}

	// The NW corner of this tile, and the NW corner of the tile diagonally below-right of
	// it -- which is this tile's own SE corner. Latitude decreases going south, so the SE
	// corner has the smaller latitude and the larger longitude.
	nwLat, nwLon := tileLatLon(z, x, y)
	seLat, seLon := tileLatLon(z, x+1, y+1)

	return nwLat >= minLat && seLat <= maxLat && seLon >= minLon && nwLon <= maxLon
}

// tileFetchGroup coalesces concurrent requests for one uncached tile into a single upstream
// call. A newly panned-to area can draw the same handful of tiles from several browser
// requests (retina pulls 2x, a resize refetches) within milliseconds of each other; without
// this each one raced to the cache and lost, spending one openAIP request apiece.
var tileFetchGroup singleflight.Group

// tileResult is what a tile fetch produces, whether served fresh or found in cache.
type tileResult struct {
	body      []byte
	noContent bool
}

// fetchOpenAIPTileFn indirects the network call so tests can stub it, the same pattern
// fetchWeatherFn, getDayLightFn, fetchAUPFn and fetchModelRunMetaFn use elsewhere in this
// package.
var fetchOpenAIPTileFn = fetchOpenAIPTile

// fetchOpenAIPTile performs the upstream request for one tile. It does not touch the cache;
// fetchAndCacheTile does that once, after singleflight has picked the one caller that
// actually runs this.
func fetchOpenAIPTile(ctx context.Context, z, x, y int) (body []byte, noContent bool, err error) {
	url := fmt.Sprintf(openAIPTileURL, z, x, y)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("failed to build openAIP tile request: %w", err)
	}
	req.Header.Set(openAIPKeyHeader, openAIPAPIKey())

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to fetch openAIP tile: %w", err)
	}
	defer resp.Body.Close()

	// Outside the data area openAIP answers 204; Leaflet handles an empty response fine,
	// and caching the answer means the next request for the same empty tile costs nothing.
	if resp.StatusCode == http.StatusNoContent {
		return nil, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("openAIP returned status %d", resp.StatusCode)
	}

	body, err = io.ReadAll(io.LimitReader(resp.Body, maxTileBodyBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("failed to read openAIP tile: %w", err)
	}
	if len(body) > maxTileBodyBytes {
		return nil, false, fmt.Errorf("openAIP tile exceeded %d bytes", maxTileBodyBytes)
	}
	return body, false, nil
}

// fetchAndCacheTile runs one coalesced fetch and stores the result, hit or miss alike.
func fetchAndCacheTile(ctx context.Context, key string, z, x, y int) (tileResult, error) {
	body, noContent, err := fetchOpenAIPTileFn(ctx, z, x, y)
	if err != nil {
		return tileResult{}, err
	}
	openAIPTiles.put(key, body, noContent)
	return tileResult{body: body, noContent: noContent}, nil
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
	if !tileInBounds(z, x, y) {
		http.Error(w, "Tile is outside the configured airfields", http.StatusNotFound)
		return
	}

	key := fmt.Sprintf("%d/%d/%d", z, x, y)
	if body, noContent, ok := openAIPTiles.get(key); ok {
		if noContent {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeTile(w, body)
		return
	}

	// context.WithoutCancel: whichever request happens to be the singleflight leader must
	// not have the shared fetch cancelled just because that one caller's own connection
	// went away -- every other waiter is blocked on the same call. httpClient's own 10s
	// timeout is what actually bounds this.
	v, err, _ := tileFetchGroup.Do(key, func() (any, error) {
		return fetchAndCacheTile(context.WithoutCancel(r.Context()), key, z, x, y)
	})
	if err != nil {
		slog.Error("failed to fetch openAIP tile", "tile", key, "error", redactedError(err))
		http.Error(w, "Failed to fetch tile", http.StatusBadGateway)
		return
	}

	res := v.(tileResult)
	if res.noContent {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeTile(w, res.body)
}

func writeTile(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "image/png")
	// Let the browser cache too, so panning back over the map costs nothing.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if _, err := w.Write(body); err != nil {
		slog.Error("failed to write tile response", "error", err)
	}
}
