package cache_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
)

func TestStoreSetBBoxStaleCircuit(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		http.Error(w, "fail", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	client := &opensky.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	zero := 0
	client.Retries = &zero
	store := cache.New(client, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{{ICAO24: "a", Lat: 1, Lon: 2}}, time.Now(), nil)

	store.Refresh()
	if !store.Stale() {
		t.Fatal("expected stale after failure with prior snapshot")
	}
	store.Refresh()
	store.Refresh()
	if !store.CircuitOpen() {
		t.Fatal("expected circuit open after failures")
	}
	store.Refresh() // blocked by circuit
	_, _, err := store.Snapshot()
	if err == nil || err.Error() != "opensky circuit open" {
		t.Fatalf("err=%v", err)
	}

	store.SetBBox(opensky.BBox{LaMin: 1, LoMin: 2, LaMax: 3, LoMax: 4})
	if store.BBox().LaMin != 1 {
		t.Fatalf("bbox %+v", store.BBox())
	}
	_ = fmt.Sprintf("%d", n)
}

func TestCircuitOpenNilBreaker(t *testing.T) {
	s := &cache.Store{}
	if s.CircuitOpen() {
		t.Fatal("nil breaker should not be open")
	}
}

// Upstream/fetcher errors set stale only. Opening the circuit here would 503
// /readyz?strict=1 on the UI host when OpenSky itself is fine.
func TestUpstreamFailuresDoNotOpenCircuit(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fetcher down", http.StatusBadGateway)
	}))
	t.Cleanup(up.Close)

	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{{ICAO24: "a", Lat: 1, Lon: 2}}, time.Now(), nil)
	store.UpstreamURL = up.URL
	store.HTTP = up.Client()

	for i := 0; i < 5; i++ {
		store.Refresh()
	}
	if !store.Stale() {
		t.Fatal("expected stale after upstream failures with a prior snapshot")
	}
	if store.CircuitOpen() {
		t.Fatal("upstream failures must not open the OpenSky circuit")
	}
	_, _, err := store.Snapshot()
	if err == nil {
		t.Fatal("expected snapshot error")
	}
	if err.Error() == "opensky circuit open" {
		t.Fatal("circuit must stay closed on fetcher errors")
	}
}

func TestSnapshotAndTrailsAreCopies(t *testing.T) {
	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "A1", Lat: 51.10, Lon: 17.00},
	}, time.Now(), nil)

	list, _, _ := store.Snapshot()
	if len(list) != 1 {
		t.Fatalf("len=%d", len(list))
	}
	list[0].Callsign = "MUTATED"
	list2, _, _ := store.Snapshot()
	if list2[0].Callsign != "A1" {
		t.Fatalf("snapshot alias: %q", list2[0].Callsign)
	}

	trails := store.Trails()
	pts := trails["aa"]
	if len(pts) == 0 {
		t.Fatal("expected trail")
	}
	pts[0].Lat = 0
	trails2 := store.Trails()
	if trails2["aa"][0].Lat == 0 {
		t.Fatal("trails alias")
	}
}
