package server_test

import (
	"encoding/json"
	"errors"
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

func TestV012AuthFailedAttemptsCountTowardLimit(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLiveToken("sekret")
	srv.SetAuthRateLimit(2, time.Minute)
	h := srv.Handler()

	post := func(token, ip string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/live?token="+token, nil)
		req.RemoteAddr = ip
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("wrong", "10.1.0.1:1"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("first fail: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("wrong", "10.1.0.1:1"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("second fail: %d %s", rec.Code, rec.Body.String())
	}
	// Quota consumed by failures — even the correct token is rejected.
	if rec := post("sekret", "10.1.0.1:1"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 after failed attempts, got %d %s", rec.Code, rec.Body.String())
	}

	// A different client still has budget.
	if rec := post("sekret", "10.1.0.2:1"); rec.Code != http.StatusOK {
		t.Fatalf("other client: %d %s", rec.Code, rec.Body.String())
	}

	// GET status checks must not consume POST quota (fresh client).
	srv.SetAuthRateLimit(1, time.Minute)
	for i := 0; i < 4; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/live", nil)
		req.RemoteAddr = "10.1.0.3:1"
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get %d: %d", i, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/live?token=sekret", nil)
	req.RemoteAddr = "10.1.0.3:1"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST after GETs: %d %s", rec.Code, rec.Body.String())
	}
}

func TestV012AuthTokenPrecedenceAndCookieFlags(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLiveToken("sekret")
	srv.SetLiveCookieTTL(90 * time.Minute)
	h := srv.Handler()

	// Query token wins over a conflicting JSON body.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/live?token=sekret", strings.NewReader(`{"token":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("query wins: %d %s", rec.Code, rec.Body.String())
	}

	// Query wrong + body correct → unauthorized (query takes precedence).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/live?token=wrong", strings.NewReader(`{"token":"sekret"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("query wrong: %d", rec.Code)
	}

	// JSON body wins over Bearer.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/live", strings.NewReader(`{"token":"sekret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("body over bearer: %d %s", rec.Code, rec.Body.String())
	}

	// Bearer with mixed-case scheme; cookie flags must stay session-safe.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/live", nil)
	req.Header.Set("Authorization", "BEARER  sekret ")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer: %d %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("missing Set-Cookie")
	}
	c := cookies[0]
	if c.Name != "wroclaw_sky_live" || c.Value != "sekret" {
		t.Fatalf("cookie %+v", c)
	}
	if !c.HttpOnly || c.Path != "/" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("insecure cookie flags %+v", c)
	}
	if c.MaxAge != 5400 {
		t.Fatalf("max-age %d", c.MaxAge)
	}

	// Cookie session authorizes runtime focus switch (no query token).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA", nil)
	req.AddCookie(&http.Cookie{Name: "wroclaw_sky_live", Value: "sekret"})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"icao":"EPWA"`) {
		t.Fatalf("cookie focus: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/auth/live", nil)
	req.AddCookie(&http.Cookie{Name: "wroclaw_sky_live", Value: "sekret"})
	h.ServeHTTP(rec, req)
	var status map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["ok"] != true || status["required"] != true {
		t.Fatalf("auth status %#v", status)
	}
}

func TestV012ReadyzStaleStillReady(t *testing.T) {
	store := mockOS11(t)
	store.ApplySnapshot([]opensky.Aircraft{{ICAO24: "aa", Lat: 51.1, Lon: 17.0}}, time.Now(), nil)
	store.ApplySnapshot(nil, time.Time{}, errors.New("opensky timeout"))
	if !store.Stale() || store.CircuitOpen() {
		t.Fatalf("stale=%v circuit=%v", store.Stale(), store.CircuitOpen())
	}
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("stale should stay ready, got %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"ready":true`) || !strings.Contains(body, `"stale":true`) {
		t.Fatalf("readyz body %s", body)
	}
	if !strings.Contains(body, `"circuit_open":false`) {
		t.Fatalf("circuit %s", body)
	}
}

func TestV012MuteShareURLSeedsJS(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?mute=%20AA%2C%2Cbb%2CAA", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `"aa"`) || !strings.Contains(body, `"bb"`) {
		t.Fatalf("bootMutes seed: %s", body)
	}
}
