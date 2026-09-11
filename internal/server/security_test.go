package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"flugwetter/internal/web"
)

// newTestAssets builds a real *web.Assets from the embedded frontend, the same way Run()
// does. Building it fresh per test rather than sharing a package-level instance keeps this
// independent of DevMode(), which reads FLUGWETTER_DEV at call time.
func newTestAssets(t *testing.T) *web.Assets {
	t.Helper()
	assets, err := web.New()
	if err != nil {
		t.Fatalf("web.New() failed: %v", err)
	}
	return assets
}

func TestSecurityHeaders_FullSet(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))

	tests := []struct {
		header string
		want   string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
		{"X-Frame-Options", "DENY"},
		{"Content-Security-Policy", baselineCSP},
		{"Strict-Transport-Security", "max-age=63072000; includeSubDomains"},
		{"Cross-Origin-Opener-Policy", "same-origin"},
		{"Cross-Origin-Resource-Policy", "same-site"},
	}

	for _, tc := range tests {
		if got := rec.Header().Get(tc.header); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.header, got, tc.want)
		}
	}

	// The exact allowlist can drift as the app's needs change; what must hold is that the
	// header exists and denies by default (empty parens = disabled), not any one clause.
	if got := rec.Header().Get("Permissions-Policy"); !strings.Contains(got, "geolocation=()") {
		t.Errorf("Permissions-Policy = %q, want it to disable geolocation", got)
	}
}

// The index document sets its own nonce-bearing CSP, which must win over the baseline --
// Header().Set replaces rather than appends, so this is really a test that indexHandler
// runs after securityHeaders in the chain, not before.
func TestSecurityHeaders_IndexOverridesTheBaselineCSP(t *testing.T) {
	assets := newTestAssets(t)
	handler := securityHeaders(indexHandler(assets))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	got := rec.Header().Get("Content-Security-Policy")
	if got == baselineCSP {
		t.Error("Content-Security-Policy is the baseline, want the nonce-bearing policy the index sets")
	}
	if !strings.Contains(got, "script-src 'self' 'nonce-") {
		t.Errorf("Content-Security-Policy = %q, want a script-src nonce", got)
	}
}
