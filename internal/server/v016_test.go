package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func TestV016RefreshRequiresLiveToken(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	// Open deploy (no LIVE_TOKEN): /refresh stays public.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/refresh", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("open refresh = %d", rec.Code)
	}

	srv.SetLiveToken("sekret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/refresh", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh without token = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/refresh", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET refresh without token = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer refresh = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "wroclaw_sky_live", Value: "sekret"})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie refresh = %d %s", rec.Code, rec.Body.String())
	}

	// /flights (no upstream fetch) stays open so HTMX polling still works.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/flights", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("flights = %d", rec.Code)
	}
}

func TestV016RefreshAuthUIGate(t *testing.T) {
	srv, err := server.New(mockOS11(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetLiveToken("sekret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"htmx:confirm",
		"elt.id !== 'refresh-btn'",
		"e.detail.issueRequest(true)",
		"Live token required for Refresh",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing refresh auth gate %q", want)
		}
	}
	if strings.Contains(body, "sekret") {
		t.Fatal("token must not leak into HTML")
	}
}

func TestV016BoardTogglesSurviveSwap(t *testing.T) {
	srv, err := server.New(mockOS11(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"function applyBoardVisibility()",
		"arrivalsBoard.classList.toggle('hidden', !arrivalsToggle.checked)",
		"departuresBoard.classList.toggle('hidden', !departuresToggle.checked)",
		"function syncExportLinks()",
		"data-export-board",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}

	start := strings.Index(body, "document.body.addEventListener('htmx:afterSwap'")
	end := strings.Index(body, "document.body.addEventListener('htmx:confirm'")
	if start < 0 || end <= start {
		t.Fatal("afterSwap block")
	}
	swap := body[start:end]
	for _, want := range []string{"applyBoardVisibility();", "applyListFilter();", "syncExportLinks();", "refreshMap();"} {
		if !strings.Contains(swap, want) {
			t.Fatalf("afterSwap must re-apply %q: %s", want, swap)
		}
	}
}

// boardStore returns a store + enricher with two inbound and two outbound flights.
func boardStore(t *testing.T) (*cache.Store, *meta.Enricher) {
	t.Helper()
	store := cache.New(nil, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa11", Callsign: "LOT101", Lat: 51.15, Lon: 16.95, Velocity: 120},
		{ICAO24: "bb22", Callsign: "RYR202", Lat: 51.16, Lon: 16.96, Velocity: 120},
		{ICAO24: "cc33", Callsign: "LOT303", Lat: 51.17, Lon: 16.97, Velocity: 120},
		{ICAO24: "dd44", Callsign: "RYR404", Lat: 51.18, Lon: 16.98, Velocity: 120},
	}, time.Now(), nil)

	routes := map[string][3]string{
		"aa11": {"LOT101", "EPWA", "EPWR"},
		"bb22": {"RYR202", "EGSS", "EPWR"},
		"cc33": {"LOT303", "EPWR", "EPWA"},
		"dd44": {"RYR404", "EPWR", "EGSS"},
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		icao := r.URL.Query().Get("icao24")
		od := routes[icao]
		_ = json.NewEncoder(w).Encode(meta.Detail{
			ICAO24: icao, Callsign: od[0], Origin: od[1], Destination: od[2],
			Route: od[1] + "-" + od[2],
		})
	}))
	t.Cleanup(up.Close)
	enrich := meta.NewEnricher()
	enrich.UpstreamURL = up.URL
	enrich.HTTP = &http.Client{Timeout: time.Second}
	enrich.ADSBdbBaseURL = "http://127.0.0.1:1"
	enrich.BaseURL = "http://127.0.0.1:1"
	for icao, od := range routes {
		_ = enrich.Enrich(meta.Detail{ICAO24: icao, Callsign: od[0]})
	}
	return store, enrich
}

func TestV016ArrivalsAPIFilters(t *testing.T) {
	store, enrich := boardStore(t)
	srv, err := server.New(store, enrich)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	type arrivalsPayload struct {
		Count    int `json:"count"`
		Arrivals []struct {
			ICAO24   string `json:"icao24"`
			Callsign string `json:"callsign"`
		} `json:"arrivals"`
	}
	get := func(url string) arrivalsPayload {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d", url, rec.Code)
		}
		var p arrivalsPayload
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if p := get("/api/arrivals"); p.Count != 2 {
		t.Fatalf("unfiltered count = %d", p.Count)
	}
	p := get("/api/arrivals?q=lot")
	if p.Count != 1 || p.Arrivals[0].Callsign != "LOT101" {
		t.Fatalf("q filter = %+v", p)
	}
	p = get("/api/arrivals?q=BB22")
	if p.Count != 1 || p.Arrivals[0].ICAO24 != "bb22" {
		t.Fatalf("icao24 q filter = %+v", p)
	}
	if p := get("/api/arrivals?airline=" + meta.AirlineHint("LOT101")); p.Count != 1 {
		t.Fatalf("airline filter = %+v", p)
	}
	if p := get("/api/arrivals?airline=any"); p.Count != 2 {
		t.Fatalf("airline=any must not filter: %+v", p)
	}
	if p := get("/api/arrivals?q=nope"); p.Count != 0 || len(p.Arrivals) != 0 {
		t.Fatalf("no match = %+v", p)
	}

	// Download path applies the same filter.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/arrivals?download=1&q=lot", nil))
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "arrivals.json") {
		t.Fatal(rec.Header())
	}
	if body := rec.Body.String(); !strings.Contains(body, "LOT101") || strings.Contains(body, "RYR202") {
		t.Fatalf("filtered export = %s", body)
	}
}

func TestV016DeparturesAPIFilters(t *testing.T) {
	store, enrich := boardStore(t)
	srv, err := server.New(store, enrich)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	var p struct {
		Count      int `json:"count"`
		Departures []struct {
			Callsign string `json:"callsign"`
		} `json:"departures"`
	}
	decode := func(url string) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d", url, rec.Code)
		}
		p.Count, p.Departures = 0, nil
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
	}

	decode("/api/departures")
	if p.Count != 2 {
		t.Fatalf("unfiltered = %d", p.Count)
	}
	decode("/api/departures?q=ryr")
	if p.Count != 1 || p.Departures[0].Callsign != "RYR404" {
		t.Fatalf("q filter = %+v", p)
	}
	decode("/api/departures?airline=" + meta.AirlineHint("RYR404"))
	if p.Count != 1 || p.Departures[0].Callsign != "RYR404" {
		t.Fatalf("airline filter = %+v", p)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/departures?export=1&q=ryr", nil))
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "departures.json") {
		t.Fatal(rec.Header())
	}
	if body := rec.Body.String(); !strings.Contains(body, "RYR404") || strings.Contains(body, "LOT303") {
		t.Fatalf("filtered export = %s", body)
	}
}

func TestV016StaleGauge(t *testing.T) {
	fresh := cache.New(nil, opensky.Wroclaw)
	fresh.ApplySnapshot([]opensky.Aircraft{{ICAO24: "aa", Callsign: "LOT1"}}, time.Now(), nil)
	srv, err := server.New(fresh, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"# HELP wroclaw_sky_stale Last refresh failed, serving previous snapshot (0/1)\n",
		"# TYPE wroclaw_sky_stale gauge\n",
		"wroclaw_sky_stale 0\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}

	stale := cache.New(nil, opensky.Wroclaw)
	stale.ApplySnapshot([]opensky.Aircraft{{ICAO24: "aa", Callsign: "LOT1"}}, time.Now(), nil)
	stale.ApplySnapshot(nil, time.Time{}, errAPI{})
	if !stale.Stale() {
		t.Fatal("expected stale store")
	}
	srv, err = server.New(stale, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "wroclaw_sky_stale 1\n") {
		t.Fatalf("expected stale 1: %s", rec.Body.String())
	}
}

type errAPI struct{}

func (errAPI) Error() string { return "opensky down" }
