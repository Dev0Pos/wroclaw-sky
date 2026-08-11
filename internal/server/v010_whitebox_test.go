package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/opensky"
)

func TestEvaluateAlertsApproachAndWebhookErrors(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetApproachRadiusM(50000)
	srv.SetLowPassAltM(0) // disable low pass
	srv.focus = geo.DefaultFocus()

	// bootstrap empty
	srv.evaluateAlerts()

	// approach needs dest match — without enricher cache, dest empty → no approach
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "a1", Callsign: "LOT", Lat: 51.12, Lon: 16.90, AltitudeM: 1000},
	}, time.Now(), nil)
	srv.evaluateAlerts() // still bootstrap? already bootstrapped with empty
	// second call with aircraft but no dest — no events

	// force approach via onApproach helper path with matching dest in CachedRoute — skip,
	// call onApproach directly
	if !srv.onApproach(opensky.Aircraft{Lat: 51.12, Lon: 16.90}, "EPWR") {
		t.Fatal("onApproach")
	}
	if srv.onApproach(opensky.Aircraft{OnGround: true, Lat: 51.12, Lon: 16.90}, "EPWR") {
		t.Fatal("ground")
	}
	if srv.onApproach(opensky.Aircraft{}, "EPWR") {
		t.Fatal("zero pos")
	}
	if srv.isLowPass(opensky.Aircraft{Lat: 51.12, Lon: 16.90, AltitudeM: 500}) {
		t.Fatal("low pass disabled")
	}
	srv.SetLowPassAltM(2000)
	if !srv.isLowPass(opensky.Aircraft{Lat: 51.12, Lon: 16.90, AltitudeM: 500}) {
		t.Fatal("low pass")
	}

	// webhook with bad URL / marshal
	srv.SetAlertWebhook("://bad")
	srv.emitAlert(AlertEvent{Type: AlertApproach, ICAO24: "x"})

	prev := alertHTTPClient
	t.Cleanup(func() { alertHTTPClient = prev })
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(hook.Close)
	srv.SetAlertWebhook(hook.URL)
	alertHTTPClient = func() *http.Client { return hook.Client() }
	srv.postWebhook(AlertEvent{Type: AlertLowPass, ICAO24: "y"})

	alertHTTPClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("net")
		})}
	}
	srv.postWebhook(AlertEvent{Type: AlertApproach, ICAO24: "z"})

	prevM := jsonMarshal
	t.Cleanup(func() { jsonMarshal = prevM })
	jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("m") }
	srv.emitAlert(AlertEvent{Type: AlertApproach, ICAO24: "m"})
	srv.postWebhook(AlertEvent{Type: AlertApproach, ICAO24: "m2"})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAuthorizedLiveAndFocusPostAuth(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLiveToken("x")
	req := httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA&token=x", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}

	// radius invalid
	req = httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWR&radius_km=bad&token=x", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%d", rec.Code)
	}

	// events unauthorized
	req = httptest.NewRequest(http.MethodGet, "/api/events", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("%d", rec.Code)
	}
}

func TestFocusQueryOnIndex(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/?focus=EPGD", nil)
	rec := httptest.NewRecorder()
	srv.handleIndex(rec, req)
	if srv.focus.ICAO != "EPGD" {
		t.Fatalf("focus=%s", srv.focus.ICAO)
	}
}

func TestSettersNoop(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetApproachRadiusM(-1)
	srv.SetLowPassAltM(-5)
	srv.SetFocusRadiusKM(-1)
	srv.SetFocus(geo.Focus{})
	srv.SetFetchToken(" f ")
	if srv.fetchToken != "f" {
		t.Fatal(srv.fetchToken)
	}
}

func TestEventsWithTokenContextCancel(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("hang")
	}
	if !strings.Contains(rec.Body.String(), "hello") {
		t.Fatalf("%s", rec.Body.String())
	}
}
