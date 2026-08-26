package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wroclaw-sky/internal/server"
)

func TestV014ArrivalsAndTilesUI(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/arrivals", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"arrivals"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/arrivals?download=1", nil))
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "arrivals.json") {
		t.Fatal(rec.Header())
	}

	store.UpstreamURL = "https://fetcher.example"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?tiles=light&arrivals=0", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"tiles-toggle", "arrivals-toggle", "BOOT_TILES", "light_all",
		"upstream-banner", "UPSTREAM_CONFIGURED = true",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/aircraft", nil))
	if !strings.Contains(rec.Body.String(), `"upstream":true`) {
		t.Fatal(rec.Body.String())
	}
}
