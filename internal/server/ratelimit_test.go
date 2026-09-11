package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestLimiterSet_BurstThenBlocked(t *testing.T) {
	s := newLimiterSet(rate.Limit(1), 3)

	for i := 0; i < 3; i++ {
		if !s.allow("client-a") {
			t.Fatalf("request %d within the burst was blocked", i)
		}
	}
	if s.allow("client-a") {
		t.Error("request past the burst was allowed")
	}
}

func TestLimiterSet_KeysAreIndependent(t *testing.T) {
	s := newLimiterSet(rate.Limit(1), 1)

	if !s.allow("client-a") {
		t.Fatal("first request from client-a was blocked")
	}
	if !s.allow("client-b") {
		t.Error("a different client was blocked by client-a's bucket")
	}
}

func TestLimiterSet_SweepDropsIdleEntries(t *testing.T) {
	s := newLimiterSet(rate.Limit(1), 1)
	s.allow("stale-client")

	s.sweep(time.Now().Add(limiterIdleTTL + time.Second))

	s.mutex.Lock()
	_, stillThere := s.entries["stale-client"]
	s.mutex.Unlock()

	if stillThere {
		t.Error("an idle-past-the-TTL entry survived the sweep")
	}
}

func TestLimiterSet_SweepKeepsRecentEntries(t *testing.T) {
	s := newLimiterSet(rate.Limit(1), 1)
	s.allow("active-client")

	s.sweep(time.Now())

	s.mutex.Lock()
	_, stillThere := s.entries["active-client"]
	s.mutex.Unlock()

	if !stillThere {
		t.Error("a just-used entry was dropped by the sweep")
	}
}

func TestClientIP_UsesRemoteAddrByDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:54321"
	req.Header.Set("X-Forwarded-For", "198.51.100.9")

	if got := clientIP(req); got != "203.0.113.5" {
		t.Errorf("clientIP() = %q, want the TCP peer address when no proxy is trusted", got)
	}
}

func TestClientIP_TrustsForwardedForOnlyWhenConfigured(t *testing.T) {
	t.Setenv(trustedProxyEnv, "1")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234" // the trusted proxy's own address
	req.Header.Set("X-Forwarded-For", "198.51.100.9, 203.0.113.5")

	// The rightmost hop is the one the trusted proxy itself appended; anything to its left
	// could have been forged by the client.
	if got := clientIP(req); got != "203.0.113.5" {
		t.Errorf("clientIP() = %q, want the rightmost X-Forwarded-For hop", got)
	}
}

func TestRateLimit_BlocksPastTheBurstAndSetsRetryAfter(t *testing.T) {
	apiLimiters = newLimiterSet(rate.Limit(1), 2)
	t.Cleanup(func() { apiLimiters = newLimiterSet(apiRateLimit, apiRateBurst) })

	handler := rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.RemoteAddr = "192.0.2.1:1"

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 response is missing Retry-After")
	}
}

func TestRateLimit_TileRouteUsesItsOwnBucket(t *testing.T) {
	// Exhaust the API bucket only; a tile request from the same client must still go
	// through, because the two routes are metered separately.
	apiLimiters = newLimiterSet(rate.Limit(1), 1)
	tileLimiters = newLimiterSet(rate.Limit(1), 1)
	t.Cleanup(func() {
		apiLimiters = newLimiterSet(apiRateLimit, apiRateBurst)
		tileLimiters = newLimiterSet(tileRateLimit, tileRateBurst)
	})

	handler := rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	apiReq := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	apiReq.RemoteAddr = "192.0.2.2:1"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, apiReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("first API request: status = %d, want %d", rec.Code, http.StatusOK)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, apiReq)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second API request: status = %d, want %d (bucket should be exhausted)", rec.Code, http.StatusTooManyRequests)
	}

	tileReq := httptest.NewRequest(http.MethodGet, "/api/tiles/openaip/1/1/1.png", nil)
	tileReq.RemoteAddr = "192.0.2.2:1"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, tileReq)
	if rec.Code != http.StatusOK {
		t.Errorf("tile request: status = %d, want %d (separate bucket from /api/*)", rec.Code, http.StatusOK)
	}
}
