package server_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

// A dead Cloudflare quick tunnel surfaces as NXDOMAIN on the UI host. The
// banner must blame the fetcher hop, not OpenSky, and point at UPSTREAM_URL.
func TestFlightsBannerUpstreamDNSFailure(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	store.UpstreamURL = "https://gone-tunnel.trycloudflare.com"
	store.HTTP = &http.Client{Transport: &http.Transport{
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		},
	}}
	store.Refresh()

	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/flights", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "Fetcher (UPSTREAM_URL):") {
		t.Fatalf("banner should name the fetcher: %s", body)
	}
	if strings.Contains(body, "OpenSky: ") {
		t.Fatalf("banner must not blame OpenSky for an upstream DNS error: %s", body)
	}
	if !strings.Contains(body, "UPSTREAM_URL host does not resolve") {
		t.Fatalf("missing DNS hint: %s", body)
	}
}

func TestFlightsBannerDirectOpenSkyKeepsLabel(t *testing.T) {
	store := cache.New(&opensky.Client{BaseURL: "http://127.0.0.1:1"}, opensky.Wroclaw)
	store.Refresh()

	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/flights", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "OpenSky: ") || strings.Contains(body, "UPSTREAM_URL") {
		t.Fatalf("direct mode should keep the OpenSky label without upstream hint: %s", body)
	}
}
