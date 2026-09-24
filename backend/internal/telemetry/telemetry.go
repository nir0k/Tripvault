// Package telemetry wires up structured logging and the build information the
// service reports through its health endpoints.
package telemetry

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// ServiceName is how the backend names itself in logs and diagnostics.
const ServiceName = "tripvault-backend"

// Version is the single version of the whole product: the backend, the frontend
// bundle and the OpenAPI document are all built from the root VERSION file. It
// is injected at link time via -ldflags; the default marks a binary built
// outside the Makefile or the Dockerfile.
var Version = "dev"

// BuildInfo describes the running binary for health and diagnostic endpoints.
type BuildInfo struct {
	Version string `json:"version"`
}

// Build - returns the build information linked into the binary.
//
// Returns:
//   - the version of the running binary.
func Build() BuildInfo {
	return BuildInfo{Version: Version}
}

// NewLogger - creates the application logger.
//
// The output format is logfmt and is not configurable: one format keeps log
// parsing identical across every deployment. Records go to the given writer
// only, normally stdout, so the container runtime owns collection and retention.
//
// Arguments:
//   - w: destination for log records.
//   - level: minimum level to emit, already validated by ParseLevel.
//
// Returns:
//   - a configured logger emitting logfmt records tagged with the service name
//     and version.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(handler).With(
		slog.String("service", ServiceName),
		slog.String("version", Version),
	)
}

// ParseLevel - maps a configured level name onto a slog level.
//
// An unknown name is an error rather than a silent fallback: a typo in the
// configuration should stop the service at start-up, not quietly change how
// much it logs.
//
// Arguments:
//   - level: one of debug, info, warn, error (case-insensitive).
//
// Returns:
//   - the matching slog level.
//   - an error when the name is not recognised.
func ParseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level %q: use debug, info, warn or error", level)
	}
}
