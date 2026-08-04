package cache_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestTrailsAccumulate(t *testing.T) {
	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "A1", Lat: 51.10, Lon: 17.00},
	}, time.Now(), nil)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "A1", Lat: 51.11, Lon: 17.01},
	}, time.Now(), nil)
	trails := store.Trails()
	pts := trails["aa"]
	if len(pts) != 2 {
		t.Fatalf("trail len = %d, want 2", len(pts))
	}
	if pts[0].At == 0 || pts[1].At == 0 {
		t.Fatalf("expected trail timestamps, got %#v", pts)
	}
}

func TestTrailsSurviveBriefAbsence(t *testing.T) {
	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "A1", Lat: 51.10, Lon: 17.00},
	}, time.Now(), nil)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "A1", Lat: 51.11, Lon: 17.01},
	}, time.Now(), nil)
	// Aircraft briefly leaves the bbox — trail should remain (grace window).
	store.ApplySnapshot(nil, time.Now(), nil)
	trails := store.Trails()
	if len(trails["aa"]) != 2 {
		t.Fatalf("expected trail kept during grace, got %#v", trails)
	}
	// Returns and continues the trail.
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "A1", Lat: 51.12, Lon: 17.02},
	}, time.Now(), nil)
	if len(store.Trails()["aa"]) != 3 {
		t.Fatalf("expected continued trail, got %#v", store.Trails())
	}
}

func TestTrailsExpireAfterGrace(t *testing.T) {
	prev := cache.TrailGraceForTest(5 * time.Millisecond)
	t.Cleanup(func() { cache.TrailGraceForTest(prev) })

	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "zz", Callsign: "Z1", Lat: 51.10, Lon: 17.00},
	}, time.Now(), nil)
	store.ApplySnapshot(nil, time.Now(), nil)
	if len(store.Trails()["zz"]) == 0 {
		t.Fatal("expected trail during grace")
	}
	time.Sleep(15 * time.Millisecond)
	store.ApplySnapshot(nil, time.Now(), nil) // prune pass
	if len(store.Trails()) != 0 {
		t.Fatalf("expected trail expired, got %#v", store.Trails())
	}
}

func TestFindAndRefreshOpenSky(t *testing.T) {
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"time": 1700000000,
			"states": [][]any{
				{"cc", "FIND1", "Poland", nil, nil, 17.0, 51.1, 1000.0, false, 50.0, 10.0, 0.0, nil, 1000.0},
			},
		})
	}))
	t.Cleanup(osSrv.Close)

	store := cache.New(&opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}, opensky.Wroclaw)
	if _, ok := store.Find("cc"); ok {
		t.Fatal("expected miss before refresh")
	}
	store.RefreshOpenSky()
	ac, ok := store.Find("CC")
	if !ok || ac.Callsign != "FIND1" {
		t.Fatalf("Find = %+v ok=%v", ac, ok)
	}
	if _, ok := store.Find("missing"); ok {
		t.Fatal("expected miss")
	}
}

func TestUpstreamErrorsAndTruncate(t *testing.T) {
	// Unauthorized / bad payload exercise fail()+truncate paths.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, strings.Repeat("x", 300), http.StatusBadGateway)
	}))
	t.Cleanup(up.Close)

	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	store.UpstreamURL = up.URL
	store.HTTP = up.Client()
	store.Refresh()
	_, _, err := store.Snapshot()
	if err == nil {
		t.Fatal("expected upstream error")
	}
	if !strings.Contains(err.Error(), "…") && !strings.Contains(err.Error(), "502") {
		// truncate adds … for long bodies; status should appear either way
		t.Fatalf("err = %v", err)
	}

	// Invalid JSON body
	up2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{not-json"))
	}))
	t.Cleanup(up2.Close)
	store.UpstreamURL = up2.URL
	store.HTTP = up2.Client()
	store.Refresh()
	if _, _, err := store.Snapshot(); err == nil {
		t.Fatal("expected json error")
	}

	// Payload with error field
	up3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"updated_at": "2026-01-02T03:04:05Z",
			"error":      "opensky down",
			"aircraft":   []any{},
		})
	}))
	t.Cleanup(up3.Close)
	store.UpstreamURL = up3.URL
	store.HTTP = up3.Client()
	store.Refresh()
	if _, _, err := store.Snapshot(); err == nil || !strings.Contains(err.Error(), "opensky down") {
		t.Fatalf("payload error = %v", err)
	}
}

func TestNearDuplicateTrailSkipped(t *testing.T) {
	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "dd", Callsign: "D1", Lat: 51.10, Lon: 17.00},
	}, time.Now(), nil)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "dd", Callsign: "D1", Lat: 51.10, Lon: 17.00}, // near-identical
	}, time.Now(), nil)
	if len(store.Trails()["dd"]) != 1 {
		t.Fatalf("expected skip duplicate, got %#v", store.Trails())
	}
}

func TestStoreBBox(t *testing.T) {
	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	if store.BBox() != opensky.Wroclaw {
		t.Fatalf("bbox = %+v", store.BBox())
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
