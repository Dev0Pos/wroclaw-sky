package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
)

func mockOpenSkyStore(t *testing.T) (*cache.Store, *httptest.Server) {
	t.Helper()
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"time": 1, "states": []any{}})
	}))
	t.Cleanup(osSrv.Close)
	client := &opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}
	zero := 0
	client.Retries = &zero
	return cache.New(client, opensky.Wroclaw), osSrv
}

func TestCoverageGapsV010(t *testing.T) {
	store, _ := mockOpenSkyStore(t)

	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(meta.Detail{
			ICAO24: "ap1", Callsign: "LOT1", Origin: "EPWA", Destination: "EPWR",
			Route: "EPWA-EPWR", Registration: "SP-TEST", TypeCode: "B738",
		})
	}))
	t.Cleanup(metaSrv.Close)
	enr := meta.NewEnricher()
	enr.HTTP = metaSrv.Client()
	enr.UpstreamURL = metaSrv.URL
	enr.BaseURL = metaSrv.URL
	enr.ADSBdbBaseURL = metaSrv.URL
	got := enr.Enrich(meta.Detail{ICAO24: "ap1", Callsign: "LOT1"})
	if got.Destination != "EPWR" {
		t.Fatalf("enrich dest=%q", got.Destination)
	}
	hint, ok := enr.CachedRoute("ap1", "LOT1")
	if !ok || hint.Destination != "EPWR" {
		t.Fatalf("cached %#v ok=%v", hint, ok)
	}

	srv, err := New(store, enr)
	if err != nil {
		t.Fatal(err)
	}
	srv.approachRadiusM = 0
	srv.SetLowPassAltM(3000)
	srv.focus = geo.DefaultFocus()
	srv.SetAlertWebhook("")

	if !srv.onApproach(opensky.Aircraft{Lat: 51.12, Lon: 16.90}, "EPWR") {
		t.Fatal("default radius approach")
	}
	if srv.isLowPass(opensky.Aircraft{AltitudeM: 500}) {
		t.Fatal("zero pos low pass")
	}
	if srv.isLowPass(opensky.Aircraft{Lat: 51.12, Lon: 16.90, AltitudeM: 0}) {
		t.Fatal("zero alt")
	}
	if srv.isLowPass(opensky.Aircraft{Lat: 51.12, Lon: 16.90, AltitudeM: 9000}) {
		t.Fatal("high alt")
	}
	if !srv.isLowPass(opensky.Aircraft{Lat: 51.12, Lon: 16.90, AltitudeM: 500}) {
		t.Fatal("low pass default radius")
	}

	srv.evaluateAlerts() // bootstrap empty
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "ap1", Callsign: "LOT1", Lat: 51.12, Lon: 16.90, AltitudeM: 500, Velocity: 80},
	}, time.Now(), nil)
	srv.evaluateAlerts() // approach + low_pass
	if len(srv.alerts.approach) == 0 {
		t.Fatal("expected approach after evaluate")
	}
	srv.evaluateAlerts() // continue branches

	// hit default alertHTTPClient closure
	prev := alertHTTPClient
	cli := alertHTTPClient()
	if cli == nil || cli.Timeout != 10*time.Second {
		t.Fatal("default client")
	}
	alertHTTPClient = prev

	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	t.Cleanup(hook.Close)
	srv.SetAlertWebhook(hook.URL)
	srv.emitAlert(AlertEvent{Type: AlertApproach, ICAO24: "z"})
	time.Sleep(30 * time.Millisecond)

	req := httptest.NewRequest(http.MethodPost, "/api/focus", strings.NewReader(
		`{"icao":"CUSTOM","lat":"51.2","lon":"17.1","city":"Lab","radius_km":70}`,
	))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("custom focus %d %s", rec.Code, rec.Body.String())
	}

	srv.focusRadiusKM = 0
	req = httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWR", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || srv.focusRadiusKM != 80 {
		t.Fatalf("default radius km %v code %d", srv.focusRadiusKM, rec.Code)
	}

	srv.focusRadiusKM = 0
	srv.focus = geo.DefaultFocus()
	req = httptest.NewRequest(http.MethodGet, "/?focus=EPWA", nil)
	rec = httptest.NewRecorder()
	srv.handleIndex(rec, req)
	if srv.focus.ICAO != "EPWA" {
		t.Fatalf("index focus %s", srv.focus.ICAO)
	}
}

func TestLiveTokenAuthFast(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLiveToken("secret")
	prevI, prevL := LiveTimingForTest(5*time.Millisecond, 30*time.Millisecond)
	t.Cleanup(func() { LiveTimingForTest(prevI, prevL) })

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/live", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatal(rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/live", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	ereq := httptest.NewRequest(http.MethodGet, "/api/events?token=secret", nil).WithContext(ctx)
	erec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(erec, ereq)
		close(done)
	}()
	time.Sleep(15 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("events hang")
	}
}

func TestPostWebhookBadRequest(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.alertWebhook = string([]byte{0x7f})
	srv.postWebhook(AlertEvent{Type: AlertApproach, ICAO24: "x"})
}

func TestAlertWebhookLowPassViaEvaluate(t *testing.T) {
	var hits atomic.Int64
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(hook.Close)

	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetAlertWebhook(hook.URL)
	srv.SetLowPassAltM(2000)
	srv.evaluateAlerts() // bootstrap empty

	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "x1", Callsign: "LOT1", Lat: 51.12, Lon: 16.90, AltitudeM: 800, Velocity: 80},
	}, time.Now(), nil)
	srv.evaluateAlerts()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && hits.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if hits.Load() == 0 {
		t.Fatal("webhook")
	}
}
