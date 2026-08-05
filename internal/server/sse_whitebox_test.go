package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
)

func TestSSEHubBroadcast(t *testing.T) {
	h := newSSEHub()
	ch := h.subscribe()
	if h.len() != 1 {
		t.Fatalf("len = %d", h.len())
	}
	h.broadcast(`{"type":"update"}`)
	select {
	case msg := <-ch:
		if !strings.Contains(msg, "update") {
			t.Fatalf("msg = %q", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
	h.unsubscribe(ch)
	if h.len() != 0 {
		t.Fatalf("len after unsub = %d", h.len())
	}
}

func TestHandleEventsStreamsUpdate(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		srv.handleEvents(rec, req)
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.Body.String(), "event: hello") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(rec.Body.String(), "event: hello") {
		cancel()
		t.Fatalf("no hello: %q", rec.Body.String())
	}

	srv.publishUpdate()
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.Body.String(), "event: update") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(rec.Body.String(), "event: update") {
		cancel()
		t.Fatalf("no update: %q", rec.Body.String())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not exit")
	}
}

func TestHandleEventsMethod(t *testing.T) {
	srv, err := New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.handleEvents(rec, httptest.NewRequest(http.MethodPost, "/api/events", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}
