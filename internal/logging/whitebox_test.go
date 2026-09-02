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
	if sr.Unwrap() != rec {
		t.Fatal("Unwrap should return inner writer")
	}
	sr.Flush()
	if !rec.Flushed {
		t.Fatal("Flush should reach httptest recorder")
	}
}

func TestStatusRecorderFlushWithoutFlusher(t *testing.T) {
	sr := &statusRecorder{ResponseWriter: nopWriter{h: make(http.Header)}}
	sr.Flush() // must not panic when inner writer is not a Flusher
}

type nopWriter struct{ h http.Header }

func (n nopWriter) Header() http.Header         { return n.h }
func (n nopWriter) Write(p []byte) (int, error) { return len(p), nil }
func (n nopWriter) WriteHeader(int)             {}

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
