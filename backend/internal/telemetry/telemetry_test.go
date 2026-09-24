package telemetry

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestParseLevel checks the known names and that an unknown one is an error.
func TestParseLevel(t *testing.T) {
	for name, want := range map[string]slog.Level{
		"debug": slog.LevelDebug, "INFO": slog.LevelInfo, " warn ": slog.LevelWarn, "error": slog.LevelError,
	} {
		if got, err := ParseLevel(name); err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v", name, got, err)
		}
	}
	if _, err := ParseLevel("verbose"); err == nil {
		t.Error("an unknown level was accepted")
	}
}

// TestNewLoggerWritesLogfmt checks records are logfmt and carry the service.
func TestNewLoggerWritesLogfmt(t *testing.T) {
	var out bytes.Buffer
	NewLogger(&out, slog.LevelInfo).Info("hello", slog.String("key", "value"))
	line := out.String()
	for _, want := range []string{"msg=hello", "key=value", "service=tripvault-backend"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q does not contain %q", line, want)
		}
	}
}
