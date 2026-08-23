package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wroclaw-sky/internal/server"
)

func TestV013SecureCookieAndAlertsExport(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLiveToken("sekret")
	srv.SetLiveCookieSecure(true)
	srv.SetLiveCookieSameSite("strict")
	h := srv.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/live?token=sekret", nil)
	h.ServeHTTP(rec, req)
	cookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "Secure") || !strings.Contains(cookie, "SameSite=Strict") {
		t.Fatalf("cookie: %q", cookie)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"focus-presets-eu", "data-preset=\"EDDF\"", "alert-hist-filter",
		"alert-export", "Export JSON",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/focus", nil))
	if !strings.Contains(rec.Body.String(), `"presets_eu"`) || !strings.Contains(rec.Body.String(), "EHAM") {
		t.Fatal(rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/alerts?download=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "alerts.json") {
		t.Fatal(rec.Header())
	}
	if !strings.Contains(rec.Body.String(), `"exported_at"`) {
		t.Fatal(rec.Body.String())
	}
}
