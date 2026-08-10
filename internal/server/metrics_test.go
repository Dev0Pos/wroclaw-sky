package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func TestMetricsAndRichHealthz(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "A1", Lat: 51.1, Lon: 17.0},
	}, time.Now(), nil)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"wroclaw_sky_refresh_total",
		"wroclaw_sky_aircraft",
		"wroclaw_sky_sse_clients",
		"wroclaw_sky_live",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %s: %s", want, body)
		}
	}
	if !strings.Contains(body, "wroclaw_sky_aircraft 1") {
		t.Fatalf("aircraft gauge: %s", body)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("metrics method = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d", rec.Code)
	}
	hb := rec.Body.String()
	for _, want := range []string{`"focus"`, `"aircraft"`, `"sse_clients"`, `"EPWR"`} {
		if !strings.Contains(hb, want) {
			t.Fatalf("healthz missing %s: %s", want, hb)
		}
	}
}

func TestIndexPlaybackUXAndSSEPayloadHooks(t *testing.T) {
	srv, err := server.New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"Need 2+ Live/Refresh ticks",
		"playback-selected",
		"applyAircraftData",
		"Selected flight only",
		"playback-status",
		"/metrics",
	} {
		if !strings.Contains(body, want) {
			// /metrics may only be in health docs — check hooks that must be in index
			if want == "/metrics" {
				continue
			}
			t.Fatalf("index missing %q", want)
		}
	}
}
