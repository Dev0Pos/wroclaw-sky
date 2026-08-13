package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func mockOS11(t *testing.T) *cache.Store {
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

func TestLiveAuthCookieNoHTMLSecret(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLiveToken("sekret")
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if strings.Contains(body, "sekret") {
		t.Fatal("token must not appear in HTML")
	}
	if !strings.Contains(body, "LIVE_TOKEN_REQUIRED = true") {
		t.Fatal("expected required flag")
	}
	if !strings.Contains(body, "alert-lowpass") || !strings.Contains(body, "alert-history") {
		t.Fatal("alerts UX missing")
	}
	if !strings.Contains(body, "ensureLiveAuth") {
		t.Fatal("auth helper missing")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/live", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("live without cookie: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/live", strings.NewReader(`{"token":"sekret"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth: %d %s", rec.Code, rec.Body.String())
	}
	cookie := rec.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("expected cookie")
	}

	prevI, prevL := server.LiveTimingForTest(5*time.Millisecond, 40*time.Millisecond)
	t.Cleanup(func() { server.LiveTimingForTest(prevI, prevL) })

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/live", nil)
	req.AddCookie(cookie[0])
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("live with cookie: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/live", nil))
	if !strings.Contains(rec.Body.String(), `"required":true`) {
		t.Fatal(rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/auth/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/auth/live", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatal(rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/live", strings.NewReader(`{"token":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatal(rec.Code)
	}
}

func TestAlertsAPIAndMetricsV011(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLowPassAltM(2000)
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/alerts", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"alerts"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/alerts", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatal(rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"wroclaw_sky_circuit_open",
		"wroclaw_sky_webhook_total",
		"wroclaw_sky_webhook_errors_total",
		"wroclaw_sky_alerts_total",
		"wroclaw_sky_sse_disconnects_total",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
}

func TestAuthLiveWhenNoTokenConfigured(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/live", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"required":false`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
