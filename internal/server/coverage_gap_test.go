package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/opensky"
)

func TestSetFocusEmptyIgnored(t *testing.T) {
	srv, err := New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	prev := srv.focus
	srv.SetFocus(geo.Focus{})
	if srv.focus != prev {
		t.Fatal("empty focus should be ignored")
	}
}

func TestRefreshAndWarmErrorMetric(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	store.UpstreamURL = "http://127.0.0.1:1"
	store.HTTP = &http.Client{Timeout: 50 * time.Millisecond}
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := srv.refreshErrors.Load()
	srv.refreshAndWarm()
	if srv.refreshErrors.Load() <= before {
		t.Fatal("expected refresh error increment")
	}
}

func TestHandleFetchErrorMetric(t *testing.T) {
	retries := 0
	store := cache.New(&opensky.Client{
		HTTP:    &http.Client{Timeout: 50 * time.Millisecond},
		BaseURL: "http://127.0.0.1:1",
		Retries: &retries,
	}, opensky.Wroclaw)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FETCH_TOKEN", "")
	before := srv.refreshErrors.Load()
	rec := httptest.NewRecorder()
	srv.handleFetch(rec, httptest.NewRequest(http.MethodPost, "/api/fetch", nil))
	if srv.refreshErrors.Load() <= before {
		t.Fatal("expected fetch error metric")
	}
}

func TestMetricsLiveGauge(t *testing.T) {
	srv, err := New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	prevI, prevL := LiveTimingForTest(time.Hour, time.Hour)
	t.Cleanup(func() { LiveTimingForTest(prevI, prevL) })
	srv.touchLive()
	rec := httptest.NewRecorder()
	srv.handleMetrics(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "wroclaw_sky_live 1") {
		t.Fatalf("live gauge: %s", rec.Body.String())
	}
}

func TestSSEHubDropSlowClient(t *testing.T) {
	h := newSSEHub()
	ch := h.subscribe()
	for i := 0; i < 8; i++ {
		h.broadcast("x")
	}
	select {
	case <-ch:
	default:
	}
	h.unsubscribe(ch)
}

type netWriter struct {
	h    http.Header
	code int
	body strings.Builder
}

func (n *netWriter) Header() http.Header         { return n.h }
func (n *netWriter) Write(p []byte) (int, error) { return n.body.Write(p) }
func (n *netWriter) WriteHeader(statusCode int)  { n.code = statusCode }

func TestHandleEventsNoFlusher(t *testing.T) {
	srv, err := New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	nw := &netWriter{h: make(http.Header)}
	srv.handleEvents(nw, httptest.NewRequest(http.MethodGet, "/api/events", nil))
	if nw.code != http.StatusInternalServerError {
		t.Fatalf("code = %d", nw.code)
	}
}

func TestPublishUpdateMarshalError(t *testing.T) {
	srv, err := New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	prev := jsonMarshal
	t.Cleanup(func() { jsonMarshal = prev })
	jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("boom") }
	srv.publishUpdate()
}

func TestPublishUpdatePayloadHasAircraft(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{{ICAO24: "aa", Callsign: "A", Lat: 1, Lon: 2}}, time.Now(), nil)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	ch := srv.hub.subscribe()
	t.Cleanup(func() { srv.hub.unsubscribe(ch) })
	srv.publishUpdate()
	select {
	case msg := <-ch:
		var m map[string]any
		if err := json.Unmarshal([]byte(msg), &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] != "update" {
			t.Fatalf("%#v", m)
		}
		if _, ok := m["aircraft"]; !ok {
			t.Fatalf("missing aircraft: %#v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestHandleEventsChannelClosed(t *testing.T) {
	srv, err := New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
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
		time.Sleep(5 * time.Millisecond)
	}
	srv.hub.mu.Lock()
	for ch := range srv.hub.clients {
		close(ch)
		delete(srv.hub.clients, ch)
	}
	srv.hub.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler stuck")
	}
}
