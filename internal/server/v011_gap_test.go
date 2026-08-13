package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
)

func TestV011CoverageGaps(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLiveToken("tok")
	srv.SetLowPassAltM(3000)

	// bearer on auth
	req := httptest.NewRequest(http.MethodPost, "/api/auth/live", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	srv.handleLiveAuth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	// force alert history trim
	srv.alerts.mu.Lock()
	srv.alerts.bootstrapped = true
	srv.alerts.approach = map[string]bool{}
	srv.alerts.lowPass = map[string]bool{}
	for i := 0; i < maxAlertHist+3; i++ {
		srv.alerts.history = append(srv.alerts.history, AlertEvent{Type: AlertLowPass, ICAO24: "x"})
	}
	srv.alerts.mu.Unlock()
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "n1", Callsign: "A", Lat: 51.12, Lon: 16.90, AltitudeM: 500},
	}, time.Now(), nil)
	srv.evaluateAlerts()
	if len(srv.recentAlerts()) > maxAlertHist {
		t.Fatal("history cap")
	}

	// circuit open metric branch
	c := &opensky.Client{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{Timeout: 20 * time.Millisecond}}
	zero := 0
	c.Retries = &zero
	s2 := cache.New(c, opensky.Wroclaw)
	s2.ApplySnapshot([]opensky.Aircraft{{ICAO24: "a", Lat: 1, Lon: 2}}, time.Now(), nil)
	s2.Refresh()
	s2.Refresh()
	s2.Refresh()
	srv2, err := New(s2, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	srv2.handleMetrics(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "wroclaw_sky_circuit_open 1") {
		t.Fatal(rec.Body.String())
	}
}
