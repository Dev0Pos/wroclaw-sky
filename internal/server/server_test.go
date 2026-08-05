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
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func TestIndexAndHealthz(t *testing.T) {
	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("index status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Refresh") || !strings.Contains(body, "wroclaw-sky") || !strings.Contains(body, "Live") {
		t.Fatalf("unexpected index body")
	}
	if !strings.Contains(body, "filter-epwr") || !strings.Contains(body, "follow-sel") {
		t.Fatalf("expected EPWR filter and Follow control in index")
	}
	if !strings.Contains(body, "refreshMap()") {
		t.Fatalf("expected map bootstrap on load")
	}
	if !strings.Contains(body, "https://github.com/Dev0Pos/wroclaw-sky") || !strings.Contains(body, "@Dev0Pos") {
		t.Fatalf("expected GitHub footer links in index")
	}
	if !strings.Contains(body, "PREDICT_SEC") || !strings.Contains(body, "setShareIcao") || !strings.Contains(body, "detail-share") {
		t.Fatalf("expected predicted track + share link UI in index")
	}
	if !strings.Contains(body, "sort-by") || !strings.Contains(body, "/api/live") {
		t.Fatalf("expected sort control and shared live API in index")
	}
	if !strings.Contains(body, "filter-airline") || !strings.Contains(body, "alert-approach") || !strings.Contains(body, "syncViewURL") {
		t.Fatalf("expected airline filter, approach alert, and shareable view URL sync")
	}
	if strings.Contains(body, "unpkg.com/htmx") || strings.Contains(body, "unpkg.com/leaflet") {
		t.Fatalf("expected vendored HTMX/Leaflet, still referencing unpkg")
	}
	if !strings.Contains(body, "/static/htmx.min.js") || !strings.Contains(body, "/static/leaflet.js") {
		t.Fatalf("expected /static assets in index")
	}
	if !strings.Contains(body, "plane-glyph approach") {
		t.Fatalf("expected approach highlight styles")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/htmx.min.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "htmx") {
		t.Fatalf("static htmx status=%d len=%d", rec.Code, rec.Body.Len())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/leaflet.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Leaflet") {
		t.Fatalf("static leaflet status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/leaflet.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("static leaflet.css status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", rec.Code)
	}
	var health map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health["status"] != "ok" {
		t.Fatalf("health = %#v", health)
	}

	// Failed refresh must not poison liveness (Render health checks).
	store.ApplySnapshot(nil, time.Time{}, errors.New("boom"))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz after error status = %d", rec.Code)
	}
}

func TestRefreshUpdatesAPI(t *testing.T) {
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"time": 1700000000,
			"states": [][]any{
				{"bb", "LOT9", "Poland", nil, nil, 17.02, 51.12, 3000.0, false, 120.0, 45.0, 0.0, nil, 3000.0},
			},
		})
	}))
	t.Cleanup(osSrv.Close)

	store := cache.New(&opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}, opensky.Wroclaw)

	hex := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.Contains(path, "/callsign/") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"response": map[string]any{
					"flightroute": map[string]any{
						"callsign": "LOT9",
						"origin": map[string]string{
							"icao_code": "EPWA", "name": "Warsaw Chopin", "municipality": "Warsaw",
						},
						"destination": map[string]string{
							"icao_code": "EPWR", "name": "Copernicus", "municipality": "Wroclaw",
						},
					},
				},
			})
			return
		}
		if strings.HasPrefix(path, "/aircraft/") || strings.Contains(path, "/api/v1/aircraft/") {
			// adsbdb-shaped (also acceptable enough for hex fallback path in other tests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"response": map[string]any{
					"aircraft": map[string]string{
						"registration": "SP-TEST", "icao_type": "B738", "type": "737-800",
						"manufacturer": "Boeing", "registered_owner": "LOT",
					},
				},
				// hexdb fields (ignored by adsbdb parser)
				"Registration": "SP-TEST", "ICAOTypeCode": "B738", "Type": "737-800",
				"Manufacturer": "Boeing", "RegisteredOwners": "LOT",
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(hex.Close)
	enrich := meta.NewEnricher()
	enrich.HTTP = hex.Client()
	enrich.BaseURL = hex.URL
	enrich.ADSBdbBaseURL = hex.URL

	srv, err := server.New(store, enrich)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/refresh", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "LOT9") {
		t.Fatalf("flights partial missing LOT9: %s", body)
	}
	if strings.Contains(body, "template error") || !strings.Contains(body, "#4ade80") {
		t.Fatalf("flights partial broken (altitude colour / template): %s", body)
	}
	if !strings.Contains(body, `data-origin="EPWA"`) || !strings.Contains(body, `data-dest="EPWR"`) {
		t.Fatalf("expected warmed EPWR route on list item: %s", body)
	}
	if !strings.Contains(body, "km") {
		t.Fatalf("expected EPWR distance/ETA hint on inbound flight: %s", body)
	}
	if !strings.Contains(body, "approach") || !strings.Contains(body, "LOT") {
		t.Fatalf("expected approach badge and airline hint: %s", body)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/aircraft", nil))
	var payload struct {
		Aircraft []struct {
			Callsign    string `json:"callsign"`
			Origin      string `json:"origin"`
			Destination string `json:"destination"`
		} `json:"aircraft"`
		Trails map[string][]cache.Point `json:"trails"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Aircraft) != 1 || payload.Aircraft[0].Callsign != "LOT9" {
		t.Fatalf("api = %+v", payload.Aircraft)
	}
	if payload.Aircraft[0].Origin != "EPWA" || payload.Aircraft[0].Destination != "EPWR" {
		t.Fatalf("api routes = %+v", payload.Aircraft[0])
	}
	if len(payload.Trails["bb"]) < 1 {
		t.Fatalf("expected trail for bb, got %#v", payload.Trails)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/aircraft/bb", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d body=%s", rec.Code, rec.Body.String())
	}
	var detail map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail["callsign"] != "LOT9" {
		t.Fatalf("detail = %#v", detail)
	}
}

func TestCustomBBoxCentersMap(t *testing.T) {
	bbox, err := opensky.ParseBBox("52.00,20.70,52.50,21.30")
	if err != nil {
		t.Fatal(err)
	}
	store := cache.New(&opensky.Client{}, bbox)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetMapLabel("EPWA · Warsaw")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "EPWA · Warsaw") {
		t.Fatalf("expected custom map label: %s", body)
	}
	// Center ≈ 52.25, 21.00
	if !strings.Contains(body, "52.25") || !strings.Contains(body, "21") {
		t.Fatalf("expected custom center in map bootstrap: %s", body)
	}
}

func TestFetchRequiresToken(t *testing.T) {
	t.Setenv("FETCH_TOKEN", "s3cret")
	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/fetch", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/fetch", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	// Will fail OpenSky (no mock) but should pass auth — use store with upstream mock instead
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"time": 1, "states": []any{}})
	}))
	t.Cleanup(osSrv.Close)
	store2 := cache.New(&opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}, opensky.Wroclaw)
	srv2, err := server.New(store2, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/fetch", nil)
	req2.Header.Set("Authorization", "Bearer s3cret")
	srv2.Handler().ServeHTTP(rec, req2)
	if rec.Code != http.StatusOK {
		t.Fatalf("fetch status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Query token also works.
	rec = httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/fetch?token=s3cret", nil)
	srv2.Handler().ServeHTTP(rec, req3)
	if rec.Code != http.StatusOK {
		t.Fatalf("fetch query token = %d", rec.Code)
	}
}

func TestHandleMetaAndDetailEdges(t *testing.T) {
	t.Setenv("FETCH_TOKEN", "tok")
	mux := http.NewServeMux()
	mux.HandleFunc("/aircraft/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"aircraft": map[string]string{
					"registration": "SP-META", "icao_type": "B738", "type": "737-800",
				},
			},
		})
	})
	mux.HandleFunc("/callsign/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	adsb := httptest.NewServer(mux)
	t.Cleanup(adsb.Close)

	store := cache.New(&opensky.Client{}, opensky.Wroclaw)
	enrich := meta.NewEnricher()
	enrich.HTTP = adsb.Client()
	enrich.ADSBdbBaseURL = adsb.URL
	enrich.BaseURL = "http://127.0.0.1:1"
	srv, err := server.New(store, enrich)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/meta?icao24=aabbcc&callsign=LOT9", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("meta unauth = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/meta?icao24=aabbcc&callsign=LOT9", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("meta = %d body=%s", rec.Code, rec.Body.String())
	}
	var detail map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail["registration"] != "SP-META" {
		t.Fatalf("meta detail = %#v", detail)
	}

	rec = httptest.NewRecorder()
	reqBad := httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	reqBad.Header.Set("Authorization", "Bearer tok")
	h.ServeHTTP(rec, reqBad)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("meta missing params = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/aircraft/unknown", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("detail miss = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/aircraft/x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("detail method = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/flights", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("flights = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/refresh", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("refresh method = %d", rec.Code)
	}
}
