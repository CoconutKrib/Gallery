// Package logging initialises structured logging for Gallery.
//
// Call Setup early in main() before any other subsystem is initialised.
// It configures the global slog default logger (slog.Info, slog.Warn, etc.)
// and — thanks to Go 1.21+ slog.SetDefault behaviour — also bridges the
// standard log package so any log.Printf calls from third-party code are
// routed through the same handler.
//
// Output is always written to stderr. If LogFile is non-empty in config, it
// is written to that file as well (stderr AND file via io.MultiWriter).
// The file is appended to on each run so restarts do not lose history.
//
// Log levels: "debug", "info" (default), "warn", "error" (case-insensitive).
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Setup initialises the default slog logger.
//
// logFile: path to a log file; empty string means stderr only.
// level:   "debug", "info", "warn", or "error"; defaults to "info".
//
// Returns a cleanup function that must be called on program exit (closes the
// log file, if any). It is safe to defer the returned function.
func Setup(logFile, level string) (func(), error) {
	lvl := parseLevel(level)

	var out io.Writer = os.Stderr
	closeFn := func() {}

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("opening log file %q: %w", logFile, err)
		}
		out = io.MultiWriter(os.Stderr, f)
		closeFn = func() { _ = f.Close() }
	}

	h := slog.NewTextHandler(out, &slog.HandlerOptions{Level: lvl})
	// SetDefault also bridges log.Printf → slog (Go 1.21+).
	slog.SetDefault(slog.New(h))

	return closeFn, nil
}

// HTTPMiddleware returns an http.Handler that logs each request to slog upon
// completion. Logged fields: method, path, status, duration_ms, remote_addr.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusCapture{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

// statusCapture is a minimal http.ResponseWriter wrapper that captures the
// status code written by the downstream handler.
type statusCapture struct {
	http.ResponseWriter
	status int
}

func (sc *statusCapture) WriteHeader(status int) {
	sc.status = status
	sc.ResponseWriter.WriteHeader(status)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
