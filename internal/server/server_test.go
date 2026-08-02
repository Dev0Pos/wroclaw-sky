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
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func TestIndexAndHealthz(t *testing.T) {
	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("index status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Refresh from OpenSky") || !strings.Contains(body, "wroclaw-sky") {
		t.Fatalf("unexpected index body")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", rec.Code)
	}
	var health map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health["status"] != "ok" {
		t.Fatalf("health = %#v", health)
	}

	// Failed refresh must not poison liveness (Render health checks).
	store.ApplySnapshot(nil, time.Time{}, errors.New("boom"))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz after error status = %d", rec.Code)
	}
}

func TestRefreshUpdatesAPI(t *testing.T) {
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"time": 1700000000,
			"states": [][]any{
				{"bb", "LOT9", "Poland", nil, nil, 17.02, 51.12, 3000.0, false, 120.0, 45.0, 0.0, nil, 3000.0},
			},
		})
	}))
	t.Cleanup(osSrv.Close)

	store := cache.New(&opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}, opensky.Wroclaw)

	hex := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/aircraft/") {
			_ = json.NewEncoder(w).Encode(map[string]string{
				"Registration": "SP-TEST", "ICAOTypeCode": "B738", "Type": "737-800",
				"Manufacturer": "Boeing", "RegisteredOwners": "LOT",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"flight": "LOT9", "route": "EPWA-EPWR"})
	}))
	t.Cleanup(hex.Close)
	enrich := meta.NewEnricher()
	enrich.HTTP = hex.Client()
	enrich.BaseURL = hex.URL

	srv, err := server.New(store, enrich)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/refresh", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "LOT9") {
		t.Fatalf("flights partial missing LOT9: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/aircraft", nil))
	var payload struct {
		Aircraft []opensky.Aircraft `json:"aircraft"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Aircraft) != 1 || payload.Aircraft[0].Callsign != "LOT9" {
		t.Fatalf("api = %+v", payload.Aircraft)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/aircraft/bb", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d body=%s", rec.Code, rec.Body.String())
	}
	var detail map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail["callsign"] != "LOT9" {
		t.Fatalf("detail = %#v", detail)
	}
}

func TestFetchRequiresToken(t *testing.T) {
	t.Setenv("FETCH_TOKEN", "s3cret")
	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/fetch", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/fetch", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	// Will fail OpenSky (no mock) but should pass auth — use store with upstream mock instead
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"time": 1, "states": []any{}})
	}))
	t.Cleanup(osSrv.Close)
	store2 := cache.New(&opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}, opensky.Wroclaw)
	srv2, err := server.New(store2, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/fetch", nil)
	req2.Header.Set("Authorization", "Bearer s3cret")
	srv2.Handler().ServeHTTP(rec, req2)
	if rec.Code != http.StatusOK {
		t.Fatalf("fetch status = %d body=%s", rec.Code, rec.Body.String())
	}
}
