package server

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

const (
	// healthcheckURL targets the loopback address inside the container. /api/config is the
	// right probe: it exercises the router and the loaded airport list without touching
	// Open-Meteo, so a healthcheck never depends on an upstream being reachable.
	healthcheckURL     = "http://127.0.0.1:8080/api/config"
	healthcheckTimeout = 3 * time.Second
)

// Build information, set at link time:
//
//	go build -ldflags "-X flugwetter/internal/server.commit=$(git rev-parse --short HEAD) ..."
//
// The Makefile does this for the container image, so the running app can say which commit
// it is. Without it, a deployment tagged :latest gives no way to tell what is actually
// serving traffic.
var (
	commit    = ""
	buildTime = ""
)

// BuildInfo describes the running binary.
type BuildInfo struct {
	// Commit is the short git SHA, with "-dirty" appended if the tree had uncommitted
	// changes at build time.
	Commit string `json:"commit"`
	// BuildTime is RFC3339, or empty for a build that was not stamped.
	BuildTime string `json:"build_time"`
	// GoVersion is whatever toolchain produced the binary.
	GoVersion string `json:"go_version"`
}

// buildInfo returns what is known about this binary.
//
// A plain `go build` or `go run` sets no ldflags, so it falls back to the VCS revision the
// toolchain stamps automatically. That covers local builds; the Makefile covers images.
func buildInfo() BuildInfo {
	info := BuildInfo{Commit: commit, BuildTime: buildTime}

	if debugInfo, ok := debug.ReadBuildInfo(); ok {
		info.GoVersion = debugInfo.GoVersion

		if info.Commit == "" {
			var revision, modified string
			for _, setting := range debugInfo.Settings {
				switch setting.Key {
				case "vcs.revision":
					revision = setting.Value
				case "vcs.modified":
					modified = setting.Value
				case "vcs.time":
					if info.BuildTime == "" {
						info.BuildTime = setting.Value
					}
				}
			}
			if revision != "" {
				info.Commit = shortSHA(revision)
				if modified == "true" {
					info.Commit += "-dirty"
				}
			}
		}
	}

	if info.Commit == "" {
		info.Commit = "unknown"
	}
	return info
}

func shortSHA(revision string) string {
	const shortLength = 7
	if len(revision) > shortLength {
		return revision[:shortLength]
	}
	return strings.TrimSpace(revision)
}

// Healthcheck asks a running instance whether it is serving, and is how the container image
// probes itself.
//
// It exists because the runtime image is scratch: there is no shell, no curl and no wget for
// a HEALTHCHECK to call, so the binary has to be able to probe itself.
func Healthcheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthcheckURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build the healthcheck request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck returned status %d", resp.StatusCode)
	}
	return nil
}
