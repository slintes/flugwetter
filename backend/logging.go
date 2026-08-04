package main

import (
	"log/slog"
	"os"
	"strings"
)

// logLevelEnv sets the minimum level at startup: debug, info, warn or error.
//
// This replaces a `const DEBUG = false` that gated the VFR scoring traces. A compile-time
// constant meant the one situation those traces exist for -- an unexpected score on the
// deployed server -- was also the one situation where they could not be turned on without
// building and pushing a new image.
const logLevelEnv = "FLUGWETTER_LOG_LEVEL"

// setupLogging installs the default slog handler. It is called first thing in main, so
// everything after it is structured and level-filtered.
func setupLogging() {
	level := slog.LevelInfo
	raw := os.Getenv(logLevelEnv)

	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		level = slog.LevelDebug
	case "info", "":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		// Deliberately not fatal: an unreadable level should not stop the server, but it
		// must not be silently ignored either, or a typo means missing traces later.
		defer func() {
			slog.Warn("unrecognised log level, defaulting to info", "env", logLevelEnv, "value", raw)
		}()
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}
