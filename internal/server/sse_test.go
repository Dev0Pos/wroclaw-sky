package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func TestFocusAirportAndPlaybackUI(t *testing.T) {
	srv, err := server.New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := geo.ParseFocus("EPWA")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetFocus(f)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `icao: "EPWA"`) {
		t.Fatalf("expected EPWA focus in page")
	}
	if !strings.Contains(body, "To EPWA") || !strings.Contains(body, "/api/events") {
		t.Fatalf("expected focus labels + SSE endpoint ref")
	}
	if !strings.Contains(body, "Trail playback") || !strings.Contains(body, "EventSource") {
		t.Fatalf("expected playback + EventSource UI")
	}
	if !strings.Contains(body, "positionAt") || !strings.Contains(body, "openLiveSource") {
		t.Fatalf("expected playback/SSE helpers")
	}
}
