// Command flugwetter serves an aviation weather dashboard for a configurable list of
// NW-German airfields.
//
// Everything lives in internal/: the server and its weather processing in internal/server,
// the browser assets in internal/web, which embeds them into this binary.
package main

import (
	"log/slog"
	"os"

	"flugwetter/internal/server"
)

func main() {
	if err := server.Run(); err != nil {
		slog.Error("flugwetter exited", "error", err)
		os.Exit(1)
	}
}
