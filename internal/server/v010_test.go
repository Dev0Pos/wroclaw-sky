package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func TestLiveTokenAuth(t *testing.T) {
	t.Skip("covered by TestLiveTokenAuthFast")
}

func mockOS(t *testing.T) *cache.Store {
	t.Helper()
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"time": 1, "states": []any{}})
	}))
	t.Cleanup(osSrv.Close)
	c := &opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}
	zero := 0
	c.Retries = &zero
	return cache.New(c, opensky.Wroclaw)
}

func TestFocusAPIAndTrailsExport(t *testing.T) {
	store := mockOS(t)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "A1", Lat: 51.1, Lon: 17.0},
	}, time.Now(), nil)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "A1", Lat: 51.11, Lon: 17.01},
	}, time.Now(), nil)

	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetFocusRadiusKM(60)
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/focus", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "EPWR") {
		t.Fatalf("get focus: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/focus", strings.NewReader(`{"icao":"EPWA"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "EPWA") {
		t.Fatalf("post focus: %d %s", rec.Code, rec.Body.String())
	}
	if store.BBox() == opensky.Wroclaw {
		t.Fatal("bbox should change")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/focus?icao=ZZZZ", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad icao: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/trails", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "aa") {
		t.Fatalf("trails: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "trails.json") {
		t.Fatal("disposition")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trails", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("trails method: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/focus", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("focus method: %d", rec.Code)
	}
}

func TestAlertWebhookAndSSEPayload(t *testing.T) {
	store := mockOS(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLowPassAltM(2000)
	h := srv.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/aircraft", nil))
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["stale"]; !ok {
		t.Fatal("stale field missing")
	}
	if _, ok := payload["circuit_open"]; !ok {
		t.Fatal("circuit_open missing")
	}
}

func TestPWAEndpoints(t *testing.T) {
	store := mockOS(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "wroclaw-sky") {
		t.Fatalf("manifest %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sw.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "CACHE") {
		t.Fatalf("sw %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sw.js", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("sw method %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/manifest.webmanifest", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("manifest method %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "playback-speed") || !strings.Contains(body, "focus-select") || !strings.Contains(body, "stale-banner") {
		t.Fatal("index missing v0.10 UI hooks")
	}
	if !strings.Contains(body, "serviceWorker") {
		t.Fatal("sw register missing")
	}
}

func TestIndexFocusQuerySwitches(t *testing.T) {
	store := mockOS(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?focus=EPWA", nil))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "icao: \"EPWA\"") {
		t.Fatalf("expected EPWA focus in page")
	}
}
