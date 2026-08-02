package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wroclaw-sky/internal/logging"
)

func TestParseLevel(t *testing.T) {
	if logging.ParseLevel("debug") != slog.LevelDebug {
		t.Fatal("debug")
	}
	if logging.ParseLevel("") != slog.LevelInfo {
		t.Fatal("default")
	}
}

func TestNewJSON(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(&buf, logging.Options{Format: "json", Level: "info"})
	log.Info("hello", "k", 1)
	var row map[string]any
	if err := json.Unmarshal(buf.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["msg"] != "hello" {
		t.Fatalf("row = %#v", row)
	}
}

func TestAccessLogSkipsHealthz(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(logging.New(&buf, logging.Options{Format: "json", Level: "info"}))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := logging.AccessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if buf.Len() != 0 {
		t.Fatalf("expected no access log for /healthz, got %s", buf.String())
	}

	buf.Reset()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(buf.String(), `"path":"/"`) {
		t.Fatalf("access log = %s", buf.String())
	}
}
