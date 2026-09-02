package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
)

func TestV014DigestAndArrivalsGaps(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu     sync.Mutex
		bodies []string
	)
	snapBodies := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(bodies))
		copy(out, bodies)
		return out
	}
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(hook.Close)
	srv.SetAlertWebhook(hook.URL)
	srv.SetAlertWebhookDigest(true)

	srv.postWebhookDigest(nil)
	srv.postWebhookDigest([]AlertEvent{
		{Type: AlertApproach, ICAO24: "a", Focus: "EPWR", At: time.Now().UTC().Format(time.RFC3339)},
		{Type: AlertLowPass, ICAO24: "b", Focus: "EPWR"},
	})
	got := waitWebhookBodies(t, 1, snapBodies)
	if !strings.Contains(got[0], `"type":"digest"`) || !strings.Contains(got[0], `"count":2`) {
		t.Fatalf("digest bodies %#v", got)
	}

	// evaluateAlerts → digest (low_pass only; no route needed)
	mu.Lock()
	bodies = nil
	mu.Unlock()
	srv.alerts.bootstrapped = true
	srv.alerts.approach = map[string]bool{}
	srv.alerts.lowPass = map[string]bool{}
	srv.SetLowPassAltM(5000)
	srv.SetApproachRadiusM(100000)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "d1", Callsign: "LOT1", Lat: 51.1027, Lon: 16.8858, AltitudeM: 400, Velocity: 50},
	}, time.Now(), nil)
	srv.evaluateAlerts()
	got = waitWebhookBodies(t, 1, snapBodies)
	if !strings.Contains(got[0], "digest") {
		t.Fatalf("evaluate digest %#v", got)
	}

	// emitAlert with empty webhook (SSE only)
	srv.SetAlertWebhook("")
	srv.emitAlert(AlertEvent{Type: AlertApproach, ICAO24: "m"})

	// non-digest evaluateAlerts path
	mu.Lock()
	bodies = nil
	mu.Unlock()
	srv.SetAlertWebhook(hook.URL)
	srv.SetAlertWebhookDigest(false)
	srv.alerts.bootstrapped = true
	srv.alerts.approach = map[string]bool{}
	srv.alerts.lowPass = map[string]bool{}
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "z1", Callsign: "X", Lat: 51.1027, Lon: 16.8858, AltitudeM: 400},
	}, time.Now(), nil)
	srv.evaluateAlerts()
	if len(waitWebhookBodies(t, 1, snapBodies)) == 0 {
		t.Fatal("expected single webhook")
	}

	// arrivals with CachedRoute hit
	routeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"flightroute": map[string]any{
					"origin":      map[string]any{"icao_code": "EPWA"},
					"destination": map[string]any{"icao_code": "EPWR"},
				},
			},
		})
	}))
	t.Cleanup(routeSrv.Close)
	enr := meta.NewEnricher()
	enr.ADSBdbBaseURL = routeSrv.URL
	enr.BaseURL = "http://127.0.0.1:1"
	srv.enricher = enr
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "LOT2", Lat: 51.15, Lon: 16.95, Velocity: 100},
	}, time.Now(), nil)
	enr.WarmRoutes([]meta.WarmItem{{ICAO24: "aa", Callsign: "LOT2"}}, time.Second)
	arrivals := srv.currentArrivals()
	if len(arrivals) == 0 {
		t.Fatal("expected arrivals with route")
	}

	rec := httptest.NewRecorder()
	srv.handleArrivalsAPI(rec, httptest.NewRequest(http.MethodPost, "/api/arrivals", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatal(rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.handleArrivalsAPI(rec, httptest.NewRequest(http.MethodGet, "/api/arrivals?export=1", nil))
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "arrivals.json") {
		t.Fatal(rec.Header())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["exported_at"] == nil {
		t.Fatal(payload)
	}

	prevM := jsonMarshal
	t.Cleanup(func() { jsonMarshal = prevM })
	jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("m") }
	srv.postWebhookDigest([]AlertEvent{{Type: AlertApproach, ICAO24: "x"}})
}

func waitWebhookBodies(t *testing.T, n int, get func() []string) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := get()
		if len(got) >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("webhook bodies want >= %d, got %#v", n, get())
	return nil
}

func TestV014DigestEvaluateBatchesOnePOST(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu     sync.Mutex
		bodies []string
		uas    []string
		ctypes []string
	)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		uas = append(uas, r.Header.Get("User-Agent"))
		ctypes = append(ctypes, r.Header.Get("Content-Type"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(hook.Close)
	srv.SetAlertWebhook(hook.URL)
	srv.SetAlertWebhookDigest(true)
	srv.SetLowPassAltM(5000)
	srv.SetApproachRadiusM(100000)

	// First snapshot only bootstraps — must not storm webhooks on deploy/Live start.
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "d1", Callsign: "LOT1", Lat: 51.1027, Lon: 16.8858, AltitudeM: 400, Velocity: 50},
		{ICAO24: "d2", Callsign: "LOT2", Lat: 51.11, Lon: 16.90, AltitudeM: 350, Velocity: 60},
	}, time.Now(), nil)
	srv.evaluateAlerts()
	mu.Lock()
	if len(bodies) != 0 {
		mu.Unlock()
		t.Fatalf("bootstrap must not POST digest, got %#v", bodies)
	}
	mu.Unlock()

	// Clear edge state so the same aircraft look newly on low-pass.
	srv.alerts.mu.Lock()
	srv.alerts.bootstrapped = true
	srv.alerts.approach = map[string]bool{}
	srv.alerts.lowPass = map[string]bool{}
	srv.alerts.mu.Unlock()
	srv.evaluateAlerts()

	waitWebhookBodies(t, 1, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(bodies))
		copy(out, bodies)
		return out
	})
	time.Sleep(40 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("want exactly 1 digest POST, got %d %#v", len(bodies), bodies)
	}
	if uas[0] != "wroclaw-sky-alerts" {
		t.Fatalf("User-Agent %q", uas[0])
	}
	if !strings.Contains(ctypes[0], "application/json") {
		t.Fatalf("Content-Type %q", ctypes[0])
	}
	var digest AlertDigest
	if err := json.Unmarshal([]byte(bodies[0]), &digest); err != nil {
		t.Fatal(err)
	}
	if digest.Type != "digest" || digest.Count != 2 || digest.Focus != "EPWR" {
		t.Fatalf("digest %+v", digest)
	}
	if digest.At == "" || len(digest.Alerts) != 2 {
		t.Fatalf("digest payload %+v", digest)
	}
	seen := map[string]bool{}
	for _, ev := range digest.Alerts {
		if ev.Type != AlertLowPass || ev.Focus != "EPWR" {
			t.Fatalf("alert %+v", ev)
		}
		seen[ev.ICAO24] = true
	}
	if !seen["d1"] || !seen["d2"] {
		t.Fatalf("icao set %#v", seen)
	}
}

func TestV014FocusSwitchDoesNotReplayAlerts(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	routeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"flightroute": map[string]any{
					"origin":      map[string]any{"icao_code": "EPWR"},
					"destination": map[string]any{"icao_code": "EPWA"},
				},
			},
		})
	}))
	t.Cleanup(routeSrv.Close)
	enr := meta.NewEnricher()
	enr.ADSBdbBaseURL = routeSrv.URL
	enr.BaseURL = "http://127.0.0.1:1"
	srv.enricher = enr
	srv.SetApproachRadiusM(100000)

	ac := opensky.Aircraft{ICAO24: "waw1", Callsign: "LOT99", Lat: 52.18, Lon: 21.00, AltitudeM: 800, Velocity: 100}
	store.ApplySnapshot([]opensky.Aircraft{ac}, time.Now(), nil)
	enr.WarmRoutes([]meta.WarmItem{{ICAO24: ac.ICAO24, Callsign: ac.Callsign}}, time.Second)
	hint, ok := enr.CachedRoute(ac.ICAO24, ac.Callsign)
	if !ok || hint.Destination != "EPWA" {
		t.Fatalf("need cached dest EPWA, got %#v ok=%v", hint, ok)
	}

	srv.evaluateAlerts() // bootstrap at EPWR — dest EPWA is not on approach
	if n := len(srv.recentAlerts()); n != 0 {
		t.Fatalf("bootstrap events %d", n)
	}

	rec := httptest.NewRecorder()
	srv.handleFocus(rec, httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("focus switch %d %s", rec.Code, rec.Body.String())
	}
	srv.alerts.mu.Lock()
	reset := !srv.alerts.bootstrapped
	srv.alerts.mu.Unlock()
	if !reset {
		t.Fatal("focus switch must reset alert bootstrap")
	}
	if !srv.onApproach(ac, "EPWA") {
		t.Fatal("aircraft should be inbound to the new focus")
	}

	srv.evaluateAlerts() // re-bootstrap at EPWA with aircraft already inbound
	if n := len(srv.recentAlerts()); n != 0 {
		t.Fatalf("replayed %d alerts after focus switch", n)
	}
	srv.evaluateAlerts()
	if n := len(srv.recentAlerts()); n != 0 {
		t.Fatalf("stable inbound still fired %d alerts", n)
	}
}

func TestV014DigestWebhookErrorAndHeaders(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}

	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "wroclaw-sky-alerts" {
			t.Errorf("User-Agent %q", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type %q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(hook.Close)
	srv.SetAlertWebhook(hook.URL)
	before := srv.webhookErrors.Load()
	fixedAt := "2026-09-02T10:00:00Z"
	srv.postWebhookDigest([]AlertEvent{
		{Type: AlertApproach, ICAO24: "a", Focus: "EPWR", At: fixedAt},
		{Type: AlertLowPass, ICAO24: "b", Focus: "EPWR", At: "ignored"},
	})
	if srv.webhookErrors.Load() <= before {
		t.Fatal("5xx digest should increment webhook errors")
	}

	prev := alertHTTPClient
	t.Cleanup(func() { alertHTTPClient = prev })
	alertHTTPClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("net")
		})}
	}
	before = srv.webhookErrors.Load()
	srv.postWebhookDigest([]AlertEvent{{Type: AlertApproach, ICAO24: "z", At: fixedAt}})
	if srv.webhookErrors.Load() <= before {
		t.Fatal("network error should increment webhook errors")
	}
}
