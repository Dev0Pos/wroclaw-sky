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
}
