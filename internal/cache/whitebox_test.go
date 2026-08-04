package cache

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/opensky"
)

func TestNewNilClient(t *testing.T) {
	s := New(nil, opensky.Wroclaw)
	if s.client == nil {
		t.Fatal("expected default client")
	}
}

func TestTruncateShort(t *testing.T) {
	if truncate("hi", 10) != "hi" {
		t.Fatal("short")
	}
	if got := truncate(strings.Repeat("a", 5), 3); got != "aaa…" {
		t.Fatalf("got %q", got)
	}
}

func TestRefreshOpenSkyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	store := New(&opensky.Client{HTTP: srv.Client(), BaseURL: srv.URL}, opensky.Wroclaw)
	store.RefreshOpenSky()
	if _, _, err := store.Snapshot(); err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyLockedErrorKeepsSnapshot(t *testing.T) {
	store := New(nil, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{{ICAO24: "aa", Lat: 1, Lon: 2}}, time.Now(), nil)
	store.ApplySnapshot(nil, time.Time{}, errSnap)
	list, _, err := store.Snapshot()
	if err == nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
}

var errSnap = errString("keep")

type errString string

func (e errString) Error() string { return string(e) }

func TestTrailsSkipsEmptyAndZeroPos(t *testing.T) {
	store := New(nil, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "", Lat: 1, Lon: 2},
		{ICAO24: "zz", Lat: 0, Lon: 0},
		{ICAO24: "ok", Lat: 51.1, Lon: 17.0},
	}, time.Now(), nil)
	trails := store.Trails()
	if len(trails) != 1 || len(trails["ok"]) != 1 {
		t.Fatalf("%#v", trails)
	}
}

func TestRefreshUpstreamNilHTTPAndBadURL(t *testing.T) {
	store := New(nil, opensky.Wroclaw)
	store.UpstreamURL = "://bad"
	store.HTTP = nil
	store.Refresh()
	if _, _, err := store.Snapshot(); err == nil {
		t.Fatal("expected error")
	}
}

func TestRefreshUpstreamRequestError(t *testing.T) {
	store := New(nil, opensky.Wroclaw)
	store.UpstreamURL = "http://127.0.0.1:1"
	store.HTTP = &http.Client{Timeout: 50 * time.Millisecond}
	store.Refresh()
	if _, _, err := store.Snapshot(); err == nil {
		t.Fatal("expected dial error")
	}
}

func TestUpstreamSuccessWithToken(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			t.Fatalf("auth = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"updated_at": "not-a-time",
			"aircraft":   []opensky.Aircraft{{ICAO24: "ee", Callsign: "E1", Lat: 51.1, Lon: 17.0}},
		})
	}))
	t.Cleanup(up.Close)
	store := New(nil, opensky.Wroclaw)
	store.UpstreamURL = up.URL
	store.UpstreamToken = "t"
	store.HTTP = up.Client()
	store.Refresh()
	list, _, err := store.Snapshot()
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %v", list, err)
	}
}

func TestTrailsNilEmptyAndExpired(t *testing.T) {
	store := New(nil, opensky.Wroclaw)
	prev := TrailGraceForTest(5 * time.Millisecond)
	t.Cleanup(func() { TrailGraceForTest(prev) })

	store.mu.Lock()
	store.trails = map[string]*trailEntry{
		"nil":   nil,
		"empty": {Points: nil, SeenAt: time.Now()},
		"old":   {Points: []Point{{Lat: 1, Lon: 2}}, SeenAt: time.Now().Add(-time.Hour)},
		"ok":    {Points: []Point{{Lat: 51.1, Lon: 17.0}}, SeenAt: time.Now()},
	}
	store.mu.Unlock()

	trails := store.Trails()
	if len(trails) != 1 || len(trails["ok"]) != 1 {
		t.Fatalf("%#v", trails)
	}
}

func TestRecordTrailsNilMapAndTrim(t *testing.T) {
	store := New(nil, opensky.Wroclaw)
	store.mu.Lock()
	store.trails = nil
	store.mu.Unlock()

	// First point creates map.
	store.ApplySnapshot([]opensky.Aircraft{{ICAO24: "trim", Lat: 51.0, Lon: 17.0}}, time.Now(), nil)

	for i := 1; i <= maxTrailPoints+5; i++ {
		store.ApplySnapshot([]opensky.Aircraft{{
			ICAO24: "trim",
			Lat:    51.0 + float64(i)*0.001,
			Lon:    17.0 + float64(i)*0.001,
		}}, time.Now(), nil)
	}
	trails := store.Trails()
	if n := len(trails["trim"]); n != maxTrailPoints {
		t.Fatalf("points = %d want %d", n, maxTrailPoints)
	}
}

func TestRefreshUpstreamNilHTTPSuccess(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"aircraft":   []opensky.Aircraft{{ICAO24: "nh", Callsign: "N1", Lat: 51.1, Lon: 17.0}},
		})
	}))
	t.Cleanup(up.Close)
	store := New(nil, opensky.Wroclaw)
	store.UpstreamURL = up.URL
	store.HTTP = nil // force default client path
	store.Refresh()
	list, _, err := store.Snapshot()
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %v", list, err)
	}
}
