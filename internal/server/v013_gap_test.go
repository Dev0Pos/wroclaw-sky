package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestV013AuthCookieModes(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if srv.liveCookieSameSiteMode() != http.SameSiteLaxMode {
		t.Fatal("default samesite")
	}
	srv.SetLiveCookieSameSite("none")
	if srv.liveCookieSameSiteMode() != http.SameSiteNoneMode {
		t.Fatal("none")
	}
	srv.SetLiveCookieSameSite("bogus")
	if srv.liveCookieSameSiteMode() != http.SameSiteLaxMode {
		t.Fatal("fallback lax")
	}
	srv.SetLiveCookieSecure(true)
	srv.SetLiveToken("t")
	rec := httptest.NewRecorder()
	srv.handleLiveAuth(rec, httptest.NewRequest(http.MethodPost, "/api/auth/live?token=t", nil))
	if !strings.Contains(rec.Header().Get("Set-Cookie"), "Secure") {
		t.Fatal(rec.Header())
	}

	rec = httptest.NewRecorder()
	srv.handleAlertsAPI(rec, httptest.NewRequest(http.MethodGet, "/api/alerts?export=1", nil))
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "alerts.json") {
		t.Fatal(rec.Header())
	}
}
