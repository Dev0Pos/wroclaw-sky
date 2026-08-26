package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	var bodies []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
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
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(bodies) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(bodies) == 0 || !strings.Contains(bodies[0], `"type":"digest"`) || !strings.Contains(bodies[0], `"count":2`) {
		t.Fatalf("digest bodies %#v", bodies)
	}

	// evaluateAlerts → digest (low_pass only; no route needed)
	bodies = nil
	srv.alerts.bootstrapped = true
	srv.alerts.approach = map[string]bool{}
	srv.alerts.lowPass = map[string]bool{}
	srv.SetLowPassAltM(5000)
	srv.SetApproachRadiusM(100000)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "d1", Callsign: "LOT1", Lat: 51.1027, Lon: 16.8858, AltitudeM: 400, Velocity: 50},
	}, time.Now(), nil)
	srv.evaluateAlerts()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(bodies) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(bodies) == 0 || !strings.Contains(bodies[0], "digest") {
		t.Fatalf("evaluate digest %#v", bodies)
	}

	// emitAlert with empty webhook (SSE only)
	srv.SetAlertWebhook("")
	srv.emitAlert(AlertEvent{Type: AlertApproach, ICAO24: "m"})

	// non-digest evaluateAlerts path
	bodies = nil
	srv.SetAlertWebhook(hook.URL)
	srv.SetAlertWebhookDigest(false)
	srv.alerts.bootstrapped = true
	srv.alerts.approach = map[string]bool{}
	srv.alerts.lowPass = map[string]bool{}
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "z1", Callsign: "X", Lat: 51.1027, Lon: 16.8858, AltitudeM: 400},
	}, time.Now(), nil)
	srv.evaluateAlerts()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(bodies) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(bodies) == 0 {
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
	got := srv.currentArrivals()
	if len(got) == 0 {
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
