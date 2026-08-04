package server

import (
	"runtime/debug"
	"strings"
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
