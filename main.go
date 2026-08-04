// Command flugwetter serves an aviation weather dashboard for a configurable list of
// NW-German airfields.
//
// Everything lives in internal/: the server and its weather processing in internal/server,
// the browser assets in internal/web, which embeds them into this binary.
package main

import (
	"flag"
	"log/slog"
	"os"

	// The runtime image is scratch, which has no /usr/share/zoneinfo. Embedding the
	// database keeps timestamps in local time there rather than silently falling back to
	// UTC, and costs one import instead of a package in the image.
	_ "time/tzdata"

	"flugwetter/internal/server"
)

func main() {
	// The runtime image is scratch, so there is no shell or curl for a container
	// HEALTHCHECK to call. The binary probes itself instead.
	healthcheck := flag.Bool("healthcheck", false,
		"probe a running instance and exit non-zero if it is not serving")
	flag.Parse()

	if *healthcheck {
		if err := server.Healthcheck(); err != nil {
			slog.Error("healthcheck failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := server.Run(); err != nil {
		slog.Error("flugwetter exited", "error", err)
		os.Exit(1)
	}
}
