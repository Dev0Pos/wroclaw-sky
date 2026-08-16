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

func TestV012AuthRateLimitAndCookieRotate(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLiveToken("sekret")
	srv.SetLiveCookieTTL(2 * time.Hour)
	srv.SetAuthRateLimit(3, time.Minute)
	h := srv.Handler()

	post := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/live?token=sekret", nil)
		req.RemoteAddr = "10.0.0.9:1234"
		h.ServeHTTP(rec, req)
		return rec
	}
	for i := 0; i < 3; i++ {
		rec := post()
		if rec.Code != http.StatusOK {
			t.Fatalf("post %d: %d %s", i, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Header().Get("Set-Cookie"), "wroclaw_sky_live=") {
			t.Fatal("missing cookie")
		}
		if !strings.Contains(rec.Body.String(), `"ttl_sec":7200`) {
			t.Fatalf("ttl body %s", rec.Body.String())
		}
	}
	rec := post()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", rec.Code)
	}

	// Rotate via GET with cookie
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/live", nil)
	req.AddCookie(&http.Cookie{Name: "wroclaw_sky_live", Value: "sekret"})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Set-Cookie"), "wroclaw_sky_live=") {
		t.Fatalf("rotate: %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/auth/live", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
}

func TestV012ReadyzAndPresetsUI(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ready":true`) {
		t.Fatalf("readyz ok: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{"focus-presets", "data-preset=\"EPWR\"", "alert-airline", "bootMutes"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in index", want)
		}
	}
	if !strings.Contains(body, "mute=") && !strings.Contains(body, "alert_airline") {
		// syncViewURL hooks present
		if !strings.Contains(body, "alert_airline") {
			t.Fatal("alert_airline sync missing")
		}
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/focus", nil))
	if !strings.Contains(rec.Body.String(), `"presets"`) || !strings.Contains(rec.Body.String(), "EPWA") {
		t.Fatal(rec.Body.String())
	}

	// Share URL seeds mute + alert airline into page
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?mute=aa,bb&alert_airline=LO&alert=1", nil))
	if !strings.Contains(rec.Body.String(), `"aa"`) || !strings.Contains(rec.Body.String(), `value="LO"`) {
		t.Fatalf("seed: %s", rec.Body.String())
	}
}

func TestV012ReadyzCircuitOpen(t *testing.T) {
	c := &opensky.Client{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{Timeout: 20 * time.Millisecond}}
	zero := 0
	c.Retries = &zero
	store := cache.New(c, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{{ICAO24: "a", Lat: 1, Lon: 2}}, time.Now(), nil)
	store.Refresh()
	store.Refresh()
	store.Refresh()
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ready":false`) {
		t.Fatal(rec.Body.String())
	}
}
