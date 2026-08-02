package cache_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
)

func TestRefreshAndSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"time": 1700000000,
			"states": [][]any{
				{"aa", "TEST1", "Poland", nil, nil, 17.0, 51.1, 5000.0, false, 100.0, 10.0, 0.0, nil, 5000.0},
			},
		})
	}))
	t.Cleanup(srv.Close)

	store := cache.New(&opensky.Client{HTTP: srv.Client(), BaseURL: srv.URL}, opensky.Wroclaw)
	list, _, err := store.Snapshot()
	if err != nil || len(list) != 0 {
		t.Fatalf("empty before refresh: %v %v", list, err)
	}
	store.Refresh()
	list, updated, err := store.Snapshot()
	if err != nil || len(list) != 1 || updated.IsZero() {
		t.Fatalf("after refresh: %v %v %v", list, updated, err)
	}
	if list[0].Callsign != "TEST1" {
		t.Fatalf("callsign = %q", list[0].Callsign)
	}
}

func TestRefreshFromUpstream(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fetch" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"updated_at": "2026-01-02T03:04:05Z",
			"error":      "",
			"aircraft": []opensky.Aircraft{
				{ICAO24: "ff", Callsign: "UP1", Lat: 51.1, Lon: 17.0},
			},
		})
	}))
	t.Cleanup(up.Close)

	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	store.UpstreamURL = up.URL
	store.UpstreamToken = "secret"
	store.HTTP = up.Client()
	store.Refresh()
	list, _, err := store.Snapshot()
	if err != nil || len(list) != 1 || list[0].Callsign != "UP1" {
		t.Fatalf("upstream snapshot: %v %v", list, err)
	}
}
