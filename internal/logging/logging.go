// Package logging configures structured slog output for containers.
package logging

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Options controls logger format and level.
type Options struct {
	Format string // json (default) or text
	Level  string // debug, info (default), warn, error
}

// New builds a slog.Logger. Empty Format defaults to json; empty Level to info.
func New(w io.Writer, opts Options) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	level := ParseLevel(opts.Level)
	handlerOpts := &slog.HandlerOptions{Level: level}
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	var h slog.Handler
	switch format {
	case "text":
		h = slog.NewTextHandler(w, handlerOpts)
	default:
		h = slog.NewJSONHandler(w, handlerOpts)
	}
	return slog.New(h)
}

// NewFromEnv builds a logger from LOG_FORMAT and LOG_LEVEL.
func NewFromEnv() *slog.Logger {
	return New(os.Stdout, Options{
		Format: os.Getenv("LOG_FORMAT"),
		Level:  os.Getenv("LOG_LEVEL"),
	})
}

// ParseLevel maps a string to slog.Level (default info).
func ParseLevel(s string) slog.Level {
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

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// AccessLog wraps next with structured HTTP access logs.
// /healthz and /readyz are skipped to avoid probe noise.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}
