package server_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func TestV014ArrivalsAndTilesUI(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/arrivals", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"arrivals"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/arrivals?download=1", nil))
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "arrivals.json") {
		t.Fatal(rec.Header())
	}

	store.UpstreamURL = "https://fetcher.example"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?tiles=light&arrivals=0", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"tiles-toggle", "arrivals-toggle", "BOOT_TILES", "World_Light_Gray_Base",
		"upstream-banner", "UPSTREAM_CONFIGURED = true",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/aircraft", nil))
	if !strings.Contains(rec.Body.String(), `"upstream":true`) {
		t.Fatal(rec.Body.String())
	}
}

func TestV014ShareTilesAndArrivalsUI(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	def := rec.Body.String()
	if !strings.Contains(def, `id="arrivals-toggle" type="checkbox" class="rounded border-slate-600 bg-slate-900 text-sky-500" checked`) {
		t.Fatal("default arrivals board should be checked")
	}
	if !strings.Contains(def, `data-tiles="dark"`) || !strings.Contains(def, `BOOT_TILES = "dark"`) {
		t.Fatal("default tiles should be Esri dark canvas")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?arrivals=0&tiles=light", nil))
	hidden := rec.Body.String()
	if !strings.Contains(hidden, `id="arrivals-toggle" type="checkbox" class="rounded border-slate-600 bg-slate-900 text-sky-500" />`) {
		t.Fatal("arrivals=0 must leave the board toggle unchecked")
	}
	if !strings.Contains(hidden, `data-tiles="light"`) || !strings.Contains(hidden, "BOOT_TILES = \"light\"") {
		t.Fatal("tiles=light share URL must seed the light basemap")
	}
	if !strings.Contains(hidden, "World_Light_Gray_Base") {
		t.Fatal("light tiles URL missing")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?tiles=bogus", nil))
	if !strings.Contains(rec.Body.String(), `data-tiles="dark"`) {
		t.Fatal("unknown tiles value must fall back to dark")
	}
}

func TestV014ArrivalsDownloadContract(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/arrivals", nil))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	if rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("inline JSON must not force download: %v", rec.Header())
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatal(rec.Header().Get("Content-Type"))
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["focus"] != "EPWR" {
		t.Fatalf("focus %#v", payload["focus"])
	}
	if _, ok := payload["count"]; !ok {
		t.Fatal(payload)
	}
	if _, ok := payload["exported_at"]; ok {
		t.Fatal("inline response should omit exported_at")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/arrivals?download=1", nil))
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "arrivals.json") {
		t.Fatal(rec.Header())
	}
	if !strings.Contains(rec.Body.String(), `"exported_at"`) {
		t.Fatal(rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/arrivals?download=true", nil))
	if rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("only download=1 triggers attachment, got %v", rec.Header())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/arrivals?download=0", nil))
	if rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("download=0 must stay inline: %v", rec.Header())
	}
}

func TestV014ReadyzHealthzCircuitContract(t *testing.T) {
	c := &opensky.Client{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{Timeout: 20 * time.Millisecond}}
	zero := 0
	c.Retries = &zero
	store := cache.New(c, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{{ICAO24: "a", Lat: 1, Lon: 2}}, time.Now(), nil)
	store.Refresh()
	store.Refresh()
	store.Refresh()
	if !store.CircuitOpen() {
		t.Fatal("expected open circuit")
	}
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	// Liveness must stay 200 — Render health checks use /healthz.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"circuit_open":true`) {
		t.Fatal(rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz?strict=TRUE", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("strict=TRUE: %d %s", rec.Code, rec.Body.String())
	}

	// Only 1/true are strict — other truthy strings must not 503.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz?strict=yes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("strict=yes should stay 200, got %d %s", rec.Code, rec.Body.String())
	}

	healthy := mockOS11(t)
	healthy.ApplySnapshot([]opensky.Aircraft{{ICAO24: "aa", Lat: 51.1, Lon: 17.0}}, time.Now(), nil)
	okSrv, err := server.New(healthy, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	okSrv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz?strict=1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ready":true`) {
		t.Fatalf("healthy strict: %d %s", rec.Code, rec.Body.String())
	}

	stale := mockOS11(t)
	stale.ApplySnapshot([]opensky.Aircraft{{ICAO24: "aa", Lat: 51.1, Lon: 17.0}}, time.Now(), nil)
	stale.ApplySnapshot(nil, time.Time{}, errors.New("opensky timeout"))
	if !stale.Stale() || stale.CircuitOpen() {
		t.Fatalf("stale=%v circuit=%v", stale.Stale(), stale.CircuitOpen())
	}
	staleSrv, err := server.New(stale, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	staleSrv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz?strict=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("stale+strict without circuit must stay 200, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"stale":true`) || !strings.Contains(rec.Body.String(), `"ready":true`) {
		t.Fatal(rec.Body.String())
	}
}
