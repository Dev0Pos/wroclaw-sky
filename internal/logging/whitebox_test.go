package logging

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusRecorderWriteWithoutHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec}
	n, err := sr.Write([]byte("hi"))
	if err != nil || n != 2 || sr.status != http.StatusOK {
		t.Fatalf("n=%d err=%v status=%d", n, err, sr.status)
	}
}

func TestNewNilWriter(t *testing.T) {
	// nil writer falls back to stdout — just ensure it doesn't panic.
	log := New(nil, Options{Format: "json", Level: "info"})
	log.Info("x")
}

func TestNewDefaultFormat(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, Options{Format: "", Level: "info"})
	log.Info("y")
	if buf.Len() == 0 {
		t.Fatal("expected output")
	}
}
