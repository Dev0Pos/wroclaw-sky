package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func TestIndexAndHealthz(t *testing.T) {
	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	srv, err := server.New(store)
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
	srv, err := server.New(store)
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
}
