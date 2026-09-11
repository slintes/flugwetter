package server

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Per-client rate limiting. Nothing in this app requires a login, so the only thing standing
// between a client and the upstreams behind it (Open-Meteo, sunrise-sunset.org, openAIP, the
// DFS AUP endpoint) is this.
const (
	// apiRateLimit and apiRateBurst cover everything except the tile proxy. A cold page
	// load pulls the four API endpoints plus roughly a dozen JS modules, the three
	// vendored libraries and every weather icon (preloadWeatherIcons loads all ~30 up
	// front) -- the burst has to comfortably clear that in one page load, not just one
	// request.
	apiRateLimit = rate.Limit(10) // sustained requests/second
	apiRateBurst = 60

	// tileRateLimit and tileRateBurst are tighter: this is the path that spends the
	// openAIP key, and a normal map pan draws at most a few dozen tiles already sitting in
	// the server-side cache.
	tileRateLimit = rate.Limit(5)
	tileRateBurst = 30

	// limiterIdleTTL bounds how long a per-IP limiter is kept once that IP goes quiet.
	// Without eviction the map of limiters is itself an unbounded-memory route -- one
	// entry per distinct IP ever seen, for the life of the process.
	limiterIdleTTL = 10 * time.Minute
	// limiterJanitorInterval is how often the sweep for idle limiters runs. Well under
	// limiterIdleTTL, so an entry is never kept alive for much longer than the TTL itself.
	limiterJanitorInterval = 2 * time.Minute

	// trustedProxyEnv, when set to any non-empty value, tells rateLimit to key on the
	// rightmost hop of X-Forwarded-For instead of the TCP peer address. Off by default:
	// behind no proxy, every request's peer address already is the client, and trusting a
	// header no proxy is adding turns the limiter into something any client can forge its
	// way around by sending its own X-Forwarded-For.
	trustedProxyEnv = "FLUGWETTER_TRUSTED_PROXY"
)

// limiterEntry pairs a limiter with when it was last used, which is what the janitor sweeps
// on.
type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// limiterSet is one rate.Limiter per client key, created lazily on first use.
type limiterSet struct {
	mutex   sync.Mutex
	entries map[string]*limiterEntry
	limit   rate.Limit
	burst   int
}

func newLimiterSet(limit rate.Limit, burst int) *limiterSet {
	return &limiterSet{entries: make(map[string]*limiterEntry), limit: limit, burst: burst}
}

func (s *limiterSet) allow(key string) bool {
	s.mutex.Lock()
	entry, ok := s.entries[key]
	if !ok {
		entry = &limiterEntry{limiter: rate.NewLimiter(s.limit, s.burst)}
		s.entries[key] = entry
	}
	entry.lastSeen = time.Now()
	limiter := entry.limiter
	s.mutex.Unlock()

	return limiter.Allow()
}

// sweep drops every entry idle for longer than limiterIdleTTL. Exported for tests; the
// janitor goroutine is what calls it in production.
func (s *limiterSet) sweep(now time.Time) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for key, entry := range s.entries {
		if now.Sub(entry.lastSeen) > limiterIdleTTL {
			delete(s.entries, key)
		}
	}
}

// janitor runs sweep on a ticker until ctx is cancelled.
func (s *limiterSet) janitor(ctx context.Context) {
	ticker := time.NewTicker(limiterJanitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(time.Now())
		}
	}
}

var (
	apiLimiters  = newLimiterSet(apiRateLimit, apiRateBurst)
	tileLimiters = newLimiterSet(tileRateLimit, tileRateBurst)
)

// trustedProxy reports whether the deployment sits behind a proxy this server is configured
// to trust for X-Forwarded-For. See trustedProxyEnv.
func trustedProxy() bool {
	return os.Getenv(trustedProxyEnv) != ""
}

// clientIP returns the key rateLimit buckets on: the rightmost X-Forwarded-For hop when a
// trusted proxy is configured (the hop that proxy itself appended, not one a client could
// forge earlier in the chain), otherwise the TCP peer address.
func clientIP(r *http.Request) string {
	if trustedProxy() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			hops := strings.Split(xff, ",")
			if candidate := strings.TrimSpace(hops[len(hops)-1]); candidate != "" {
				return candidate
			}
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr without a port, or otherwise unparsable -- use it whole rather than
		// dropping every client into one bucket.
		return r.RemoteAddr
	}
	return host
}

// rateLimit is the outermost middleware: a client past its bucket never reaches logging,
// compression or any handler. The tile route gets the tighter of the two buckets, because it
// is the one spending a paid key rather than this server's own CPU.
func rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limiters := apiLimiters
		if strings.HasPrefix(r.URL.Path, "/api/tiles/") {
			limiters = tileLimiters
		}

		if !limiters.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
