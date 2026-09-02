package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/logging"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func TestFocusAirportAndPlaybackUI(t *testing.T) {
	srv, err := server.New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := geo.ParseFocus("EPWA")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetFocus(f)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `icao: "EPWA"`) {
		t.Fatalf("expected EPWA focus in page")
	}
	if !strings.Contains(body, "To EPWA") || !strings.Contains(body, "/api/events") {
		t.Fatalf("expected focus labels + SSE endpoint ref")
	}
	if !strings.Contains(body, "Trail playback") || !strings.Contains(body, "EventSource") {
		t.Fatalf("expected playback + EventSource UI")
	}
	if !strings.Contains(body, "positionAt") || !strings.Contains(body, "openLiveSource") {
		t.Fatalf("expected playback/SSE helpers")
	}
}

// Production wraps the mux with logging.AccessLog. That wrapper used to hide
// http.Flusher, so GET /api/events always returned 500 and Live SSE never pushed.
func TestEventsThroughAccessLog(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := logging.AccessLog(srv.Handler())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if rec.Code == http.StatusInternalServerError {
			cancel()
			t.Fatalf("AccessLog hid Flusher: %d %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "event: hello") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(rec.Body.String(), "event: hello") {
		cancel()
		t.Fatalf("no hello (code=%d): %q", rec.Code, rec.Body.String())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return")
	}
}
