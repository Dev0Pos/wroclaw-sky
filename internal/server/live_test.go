package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func TestLiveStartsSharedPoller(t *testing.T) {
	var hits int
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"time": 1700000000,
			"states": [][]any{
				{"aa", "LIVE1", "Poland", nil, nil, 17.0, 51.1, 3000.0, false, 100.0, 10.0, 0.0, nil, 3000.0},
			},
		})
	}))
	t.Cleanup(osSrv.Close)

	store := cache.New(&opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}, opensky.Wroclaw)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("live status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["live"] != true {
		t.Fatalf("live body = %#v", body)
	}
	if hits < 1 {
		t.Fatalf("expected OpenSky hit on live start, hits=%d", hits)
	}

	list, _, err := store.Snapshot()
	if err != nil || len(list) != 1 || list[0].Callsign != "LIVE1" {
		t.Fatalf("snapshot after live: %v %v", list, err)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var health map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health["live"] != true {
		t.Fatalf("health live = %#v", health)
	}

	// Second heartbeat should not require waiting; poller already running.
	before := hits
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("live heartbeat status = %d", rec.Code)
	}
	// Allow a brief moment; heartbeat must not force an extra immediate refresh.
	time.Sleep(20 * time.Millisecond)
	if hits != before {
		t.Fatalf("heartbeat should not sync-refresh again, hits %d → %d", before, hits)
	}
}
