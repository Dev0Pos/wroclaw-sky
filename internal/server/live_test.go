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

	// Second heartbeat should not force an extra immediate refresh.
	before := hits
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("live heartbeat status = %d", rec.Code)
	}
	time.Sleep(20 * time.Millisecond)
	if hits != before {
		t.Fatalf("heartbeat should not sync-refresh again, hits %d → %d", before, hits)
	}

	// DELETE reports status without stopping shared poller.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE live = %d", rec.Code)
	}
	var del map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&del)
	if del["live"] != true {
		t.Fatalf("DELETE should not kill poller: %#v", del)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/live", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT status = %d", rec.Code)
	}
}

func TestLiveLoopTicksAndExpires(t *testing.T) {
	prevI, prevL := server.LiveTimingForTest(25*time.Millisecond, 40*time.Millisecond)
	t.Cleanup(func() { server.LiveTimingForTest(prevI, prevL) })

	var hits int
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"time":   1,
			"states": []any{},
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
		t.Fatalf("live = %d", rec.Code)
	}
	startHits := hits
	// Wait for at least one ticker refresh without renewing the lease.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if hits > startHits {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if hits <= startHits {
		t.Fatalf("expected liveLoop tick, hits=%d start=%d", hits, startHits)
	}
	// Wait for lease expiry (no heartbeats).
	time.Sleep(120 * time.Millisecond)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var health map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&health)
	// May still be true briefly until next tick notices expiry; poll a bit.
	for i := 0; i < 20 && health["live"] == true; i++ {
		time.Sleep(20 * time.Millisecond)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		_ = json.NewDecoder(rec.Body).Decode(&health)
	}
	if health["live"] == true {
		t.Fatalf("expected live expired, health=%#v", health)
	}
}
