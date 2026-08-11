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
