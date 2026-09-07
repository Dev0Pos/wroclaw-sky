package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

// Fetcher hosts set UPSTREAM_URL on the enricher used for Live UI, but GET /api/meta
// must enrich locally. Recursing would loop UI ↔ fetcher until timeout.
func TestMetaNeverRecursesUpstream(t *testing.T) {
	var upstreamHits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		http.Error(w, "should not be called", http.StatusTeapot)
	}))
	t.Cleanup(up.Close)

	adsb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/aircraft/") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"aircraft": map[string]string{
					"registration": "SP-LOCAL", "icao_type": "B738",
				},
			},
		})
	}))
	t.Cleanup(adsb.Close)

	enr := meta.NewEnricher()
	enr.HTTP = adsb.Client()
	enr.UpstreamURL = up.URL
	enr.UpstreamToken = "fetcher-secret"
	enr.ADSBdbBaseURL = adsb.URL
	enr.BaseURL = "http://127.0.0.1:1"

	srv, err := server.New(cache.New(nil, opensky.Wroclaw), enr)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetFetchToken("tok")
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/meta?icao24=aabbcc&callsign=LOT9", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("meta %d %s", rec.Code, rec.Body.String())
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("fetcher /api/meta hit UpstreamURL %d times (would recurse)", upstreamHits.Load())
	}
	if !strings.Contains(rec.Body.String(), "SP-LOCAL") {
		t.Fatalf("expected local adsbdb enrichment: %s", rec.Body.String())
	}
}

func TestWrongBearerDoesNotFallThroughToCookie(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLiveToken("sekret")
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/live", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	req.AddCookie(&http.Cookie{Name: "wroclaw_sky_live", Value: "sekret"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong Bearer must not fall through to cookie, got %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	req.AddCookie(&http.Cookie{Name: "wroclaw_sky_live", Value: "sekret"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("focus: %d %s", rec.Code, rec.Body.String())
	}
}

type sseRecorder struct {
	mu     sync.Mutex
	header http.Header
	buf    bytes.Buffer
	code   int
	wrote  chan struct{}
	once   sync.Once
}

func (s *sseRecorder) Header() http.Header { return s.header }

func (s *sseRecorder) WriteHeader(code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.code == 0 {
		s.code = code
	}
}

func (s *sseRecorder) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.code == 0 {
		s.code = http.StatusOK
	}
	n, err := s.buf.Write(p)
	s.once.Do(func() { close(s.wrote) })
	return n, err
}

func (s *sseRecorder) Flush() {}

func (s *sseRecorder) status() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.code == 0 {
		return http.StatusOK
	}
	return s.code
}

func (s *sseRecorder) body() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestEventsAuthorizedByLiveCookie(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLiveToken("sekret")
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/events", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth events %d", rec.Code)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: "wroclaw_sky_live", Value: "sekret"})
	w := &sseRecorder{header: make(http.Header), wrote: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(w, req)
		close(done)
	}()

	select {
	case <-w.wrote:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timed out waiting for SSE hello")
	}
	if w.status() == http.StatusUnauthorized {
		cancel()
		t.Fatalf("cookie should authorize SSE: %d %s", w.status(), w.body())
	}
	if !strings.Contains(w.body(), "event: hello") {
		cancel()
		t.Fatalf("no hello (code=%d): %q", w.status(), w.body())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return")
	}
}

func TestAlertsDownloadTrueStaysInline(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/alerts", nil))
	if rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("inline alerts must not force download: %v", rec.Header())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/alerts?download=true", nil))
	if rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("only download=1/export=1 attach, got %v", rec.Header())
	}
	if strings.Contains(rec.Body.String(), `"exported_at"`) {
		t.Fatal("download=true must stay inline")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/alerts?download=1", nil))
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "alerts.json") {
		t.Fatal(rec.Header())
	}
}
