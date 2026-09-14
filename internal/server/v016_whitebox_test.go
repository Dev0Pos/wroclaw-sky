package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
)

// stubRoute is one pre-warmed origin/destination pair for the route cache.
type stubRoute struct {
	icao24      string
	callsign    string
	origin      string
	destination string
}

// stubRouteEnricher returns an Enricher whose cache already holds the given routes.
func stubRouteEnricher(t *testing.T, routes ...stubRoute) *meta.Enricher {
	t.Helper()
	byICAO := make(map[string]stubRoute, len(routes))
	for _, r := range routes {
		byICAO[r.icao24] = r
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := byICAO[r.URL.Query().Get("icao24")]
		_ = json.NewEncoder(w).Encode(meta.Detail{
			ICAO24:      route.icao24,
			Callsign:    route.callsign,
			Origin:      route.origin,
			Destination: route.destination,
			Route:       route.origin + "-" + route.destination,
		})
	}))
	t.Cleanup(up.Close)
	enrich := meta.NewEnricher()
	enrich.UpstreamURL = up.URL
	enrich.HTTP = &http.Client{Timeout: time.Second}
	enrich.ADSBdbBaseURL = "http://127.0.0.1:1"
	enrich.BaseURL = "http://127.0.0.1:1"
	for _, r := range routes {
		_ = enrich.Enrich(meta.Detail{ICAO24: r.icao24, Callsign: r.callsign})
	}
	return enrich
}

// emptyOpenSky returns a store backed by an OpenSky stub with no state vectors.
func emptyOpenSky(t *testing.T) *cache.Store {
	t.Helper()
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"time": 1, "states": []any{}})
	}))
	t.Cleanup(osSrv.Close)
	c := &opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}
	zero := 0
	c.Retries = &zero
	return cache.New(c, opensky.Wroclaw)
}

func TestV016BootstrapRefreshColdStart(t *testing.T) {
	srv, err := New(emptyOpenSky(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.BootstrapRefresh()
	if got := srv.refreshTotal.Load(); got != 1 {
		t.Fatalf("cold start must refresh once, got %d", got)
	}
	if srv.lastRefresh.Load() == 0 {
		t.Fatal("lastRefresh must be set after bootstrap")
	}

	// Second call is a no-op: lastRefresh already recorded.
	srv.BootstrapRefresh()
	if got := srv.refreshTotal.Load(); got != 1 {
		t.Fatalf("bootstrap must not double-fetch, got %d", got)
	}
}

func TestV016BootstrapRefreshSkipsWarmSnapshot(t *testing.T) {
	store := emptyOpenSky(t)
	// Live (or a fetcher) already populated the snapshot before bootstrap ran.
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "LOT1", Lat: 51.1, Lon: 17.0},
	}, time.Now(), nil)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.BootstrapRefresh()
	if got := srv.refreshTotal.Load(); got != 0 {
		t.Fatalf("existing snapshot must skip bootstrap, got %d", got)
	}
}

func TestV016ApproachChipUsesConfiguredRadius(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	// ~60 km north of EPWR: inside a 80 km radius, outside the 40 km default.
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "far", Callsign: "LOT1", Lat: 51.645, Lon: 16.886, Velocity: 200},
	}, time.Now(), nil)
	srv, err := New(store, stubRouteEnricher(t, stubRoute{"far", "LOT1", "EPWA", "EPWR"}))
	if err != nil {
		t.Fatal(err)
	}

	if srv.snapshotData().Aircraft[0].Approach {
		t.Fatal("60 km inbound must not be approach at the 40 km default")
	}
	srv.SetApproachRadiusM(80000)
	if !srv.snapshotData().Aircraft[0].Approach {
		t.Fatal("60 km inbound must be approach at APPROACH_RADIUS_KM=80")
	}

	rec := httptest.NewRecorder()
	srv.handleFlights(rec, httptest.NewRequest(http.MethodGet, "/flights", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `data-approach="1"`) {
		t.Fatalf("chip must follow the configured radius: %s", body)
	}
	for _, want := range []string{
		`data-export-board="/api/arrivals"`,
		`href="/api/arrivals?download=1"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing export link %q", want)
		}
	}
}

func TestV016BoardFilterMatching(t *testing.T) {
	f := boardFilterFrom(httptest.NewRequest(http.MethodGet, "/api/arrivals?q=LOT&airline=any", nil))
	if f.q != "lot" || f.airline != "" {
		t.Fatalf("airline=any must clear the filter: %+v", f)
	}
	if !f.match("abc123", "LOT123", "LOT") {
		t.Fatal("callsign substring")
	}
	if !boardFilterFrom(httptest.NewRequest(http.MethodGet, "/api/arrivals?q=ABC", nil)).
		match("abc123", "RYR1", "Ryanair") {
		t.Fatal("icao24 substring")
	}
	if f.match("dead01", "RYR1", "Ryanair") {
		t.Fatal("non-matching callsign/icao must be dropped")
	}

	airlineOnly := boardFilterFrom(httptest.NewRequest(http.MethodGet, "/api/arrivals?airline=ryan", nil))
	if !airlineOnly.match("abc", "RYR1", "Ryanair") {
		t.Fatal("airline contains")
	}
	if airlineOnly.match("abc", "LOT1", "LOT") {
		t.Fatal("airline mismatch must be dropped")
	}

	arrivals := []arrivalRow{{ICAO24: "a", Callsign: "LOT1", Airline: "LOT"}}
	departures := []departureRow{{ICAO24: "a", Callsign: "LOT1", Airline: "LOT"}}
	empty := boardFilter{}
	if len(empty.arrivals(arrivals)) != 1 || len(empty.departures(departures)) != 1 {
		t.Fatal("empty filter must pass rows through")
	}
	if len(airlineOnly.arrivals(arrivals)) != 0 || len(airlineOnly.departures(departures)) != 0 {
		t.Fatal("filter must drop non-matching rows")
	}
}

func TestV016WebhookMuteAndAirlineFilter(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(hook.Close)

	store := cache.New(nil, opensky.Wroclaw)
	srv, err := New(store, stubRouteEnricher(t,
		stubRoute{"lot1", "LOT1", "EPWA", "EPWR"},
		stubRoute{"ryr1", "RYR1", "EGSS", "EPWR"},
		stubRoute{"lot2", "LOT2", "EDDF", "EPWR"},
	))
	if err != nil {
		t.Fatal(err)
	}
	srv.SetAlertWebhook(hook.URL)
	srv.SetAlertWebhookDigest(true)
	srv.SetAlertMute([]string{"LOT2", " ", ""})
	srv.SetAlertAirline("lot")

	srv.evaluateAlerts() // bootstrap
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "lot1", Callsign: "LOT1", Lat: 51.12, Lon: 16.90, Velocity: 90},
		{ICAO24: "ryr1", Callsign: "RYR1", Lat: 51.13, Lon: 16.91, Velocity: 90},
		{ICAO24: "lot2", Callsign: "LOT2", Lat: 51.14, Lon: 16.92, Velocity: 90},
	}, time.Now(), nil)
	srv.evaluateAlerts()

	// SSE / history stay unfiltered so the browser mute UX still sees everything.
	if got := len(srv.recentAlerts()); got != 3 {
		t.Fatalf("history must keep all edges, got %d", got)
	}

	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	got := append([]string(nil), bodies...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected one digest POST, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], `"LOT1"`) {
		t.Fatalf("digest must keep LOT1: %s", got[0])
	}
	if strings.Contains(got[0], `"RYR1"`) {
		t.Fatalf("airline prefix must drop RYR1: %s", got[0])
	}
	if strings.Contains(got[0], `"LOT2"`) {
		t.Fatalf("mute must drop LOT2: %s", got[0])
	}
	if !strings.Contains(got[0], `"count":1`) {
		t.Fatalf("digest count must reflect the filter: %s", got[0])
	}
}

func TestV016WebhookFilterDropsWholeBatch(t *testing.T) {
	var hits atomic.Int64
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(hook.Close)

	store := cache.New(nil, opensky.Wroclaw)
	srv, err := New(store, stubRouteEnricher(t, stubRoute{"ryr1", "RYR1", "EGSS", "EPWR"}))
	if err != nil {
		t.Fatal(err)
	}
	srv.SetAlertWebhook(hook.URL)
	srv.SetAlertWebhookDigest(false)
	srv.SetAlertAirline("LOT")

	srv.evaluateAlerts()
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "ryr1", Callsign: "RYR1", Lat: 51.12, Lon: 16.90, Velocity: 90},
	}, time.Now(), nil)
	srv.evaluateAlerts()
	time.Sleep(60 * time.Millisecond)
	if got := hits.Load(); got != 0 {
		t.Fatalf("fully filtered batch must not POST, hits=%d", got)
	}

	// Single-event path (digest off) still posts what survives the filter.
	srv.SetAlertAirline("")
	srv.SetAlertMute(nil)
	srv.emitAlert(AlertEvent{Type: AlertApproach, ICAO24: "ryr1", Callsign: "RYR1"})
	time.Sleep(60 * time.Millisecond)
	if got := hits.Load(); got != 1 {
		t.Fatalf("unfiltered emitAlert must POST once, hits=%d", got)
	}
	srv.SetAlertMute([]string{"ryr1"})
	srv.emitAlert(AlertEvent{Type: AlertApproach, ICAO24: "RYR1", Callsign: "RYR1"})
	time.Sleep(60 * time.Millisecond)
	if got := hits.Load(); got != 1 {
		t.Fatalf("muted emitAlert must not POST, hits=%d", got)
	}
}

func TestV016SetAlertMuteBlankOnly(t *testing.T) {
	srv, err := New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetAlertMute([]string{" ", ""})
	if srv.alertMute != nil {
		t.Fatalf("blank-only list must clear mutes: %v", srv.alertMute)
	}
	srv.SetAlertAirline("  lot  ")
	if srv.alertAirline != "LOT" {
		t.Fatalf("airline = %q", srv.alertAirline)
	}
}
