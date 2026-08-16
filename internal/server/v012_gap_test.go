package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestV012AuthLimiterGaps(t *testing.T) {
	var nilLim *authLimiter
	if !nilLim.allow("x") {
		t.Fatal("nil limiter")
	}
	lim := newAuthLimiter(0, 0) // defaults
	if lim.max != defaultAuthRateMax || lim.window != defaultAuthRateWin {
		t.Fatalf("%+v", lim)
	}
	base := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	prev := authNow
	t.Cleanup(func() { authNow = prev })
	authNow = func() time.Time { return base }
	if !lim.allow("a") {
		t.Fatal("first")
	}
	authNow = func() time.Time { return base.Add(2 * time.Minute) }
	if !lim.allow("a") {
		t.Fatal("window reset")
	}

	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if srv.liveCookieMaxAge() != int(defaultLiveCookieTTL.Seconds()) {
		t.Fatal("default ttl")
	}
	srv.SetLiveCookieTTL(0) // ignore
	srv.SetLiveCookieTTL(-time.Hour)
	if srv.liveCookieMaxAge() != int(defaultLiveCookieTTL.Seconds()) {
		t.Fatal("still default")
	}
	srv.SetAuthRateLimit(0, 0)

	// X-Forwarded-For client key + no-token POST
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/live", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	req.RemoteAddr = "bad-addr"
	srv.handleLiveAuth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	// RemoteAddr without host:port
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/live", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "unix-socket"
	srv.handleLiveAuth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	srv.SetLiveToken("t")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/auth/live", nil)
	srv.handleLiveAuth(rec, req)
	if strings.Contains(rec.Header().Get("Set-Cookie"), "wroclaw_sky_live=t") {
		t.Fatal("should not rotate without auth")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/auth/live", nil)
	srv.handleLiveAuth(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatal(rec.Code)
	}

	if tokenFromCookie(httptest.NewRequest(http.MethodGet, "/", nil)) != "" {
		t.Fatal("empty cookie")
	}
}
