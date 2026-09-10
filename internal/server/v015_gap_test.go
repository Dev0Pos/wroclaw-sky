package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
)

func TestV015DeparturesGaps(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}

	routeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"flightroute": map[string]any{
					"origin":      map[string]any{"icao_code": "EPWR"},
					"destination": map[string]any{"icao_code": "EPWA"},
				},
			},
		})
	}))
	t.Cleanup(routeSrv.Close)
	enr := meta.NewEnricher()
	enr.ADSBdbBaseURL = routeSrv.URL
	enr.BaseURL = "http://127.0.0.1:1"
	srv.enricher = enr
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "dd", Callsign: "LOT3", Lat: 51.15, Lon: 16.95, Velocity: 100, Vertical: 1.2},
	}, time.Now(), nil)
	enr.WarmRoutes([]meta.WarmItem{{ICAO24: "dd", Callsign: "LOT3"}}, time.Second)

	deps := srv.currentDepartures()
	if len(deps) == 0 {
		t.Fatal("expected departures with route")
	}
	if deps[0].Destination != "EPWA" {
		t.Fatalf("%+v", deps[0])
	}

	rec := httptest.NewRecorder()
	srv.handleDeparturesAPI(rec, httptest.NewRequest(http.MethodPost, "/api/departures", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatal(rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.handleDeparturesAPI(rec, httptest.NewRequest(http.MethodGet, "/api/departures?export=1", nil))
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "departures.json") {
		t.Fatal(rec.Header())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["exported_at"] == nil {
		t.Fatal(payload)
	}

	data := srv.snapshotData()
	if len(data.Departures) == 0 {
		t.Fatal("snapshot departures")
	}
	if data.UpdatedAtMs == 0 || data.UpdatedAt == "" {
		t.Fatalf("updated %+v", data)
	}
	if !data.ShareFocus {
		t.Fatal("default share focus")
	}

	rec = httptest.NewRecorder()
	srv.handleFlights(rec, httptest.NewRequest(http.MethodGet, "/flights", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `id="departures"`) {
		t.Fatal("departures board missing")
	}
	if !strings.Contains(body, "data-vertical=") {
		t.Fatal("data-vertical missing")
	}
	if !strings.Contains(body, "data-updated-ms=") {
		t.Fatal("data-updated-ms missing")
	}
	if !strings.Contains(body, "title=\"Climbing\"") && !strings.Contains(body, "↑") {
		t.Fatal("climb badge missing for vertical>0.5")
	}
}

func TestV015ShareFocusAndPredictHTML(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetShareFocus(false)
	prevFocus := srv.focus.ICAO

	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/?focus=EPWA", nil))
	if srv.focus.ICAO != prevFocus {
		t.Fatalf("shareFocus=false must ignore GET focus, got %s", srv.focus.ICAO)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"SHARE_FOCUS_ENABLED = false",
		"Share focus disabled",
		"copy-view-link",
		"predict-mode",
		"approachCircle",
		"paintUpdatedAge",
		"departures-toggle",
		"Focus → ",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}

	srv.SetShareFocus(true)
	rec = httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/?focus=EPWA&predict=0&departures=0", nil))
	if srv.focus.ICAO != "EPWA" {
		t.Fatalf("share on must switch, got %s", srv.focus.ICAO)
	}
	body = rec.Body.String()
	if !strings.Contains(body, `BOOT_PREDICT = "0"`) {
		t.Fatal(body)
	}
	if !strings.Contains(body, `id="departures-toggle" type="checkbox" class="rounded border-slate-600 bg-slate-900 text-sky-500" />`) {
		t.Fatal("departures=0 unchecked")
	}

	// POST focus still works with share off
	srv.SetShareFocus(false)
	srv.SetFocus(geo.DefaultFocus())
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPKK", nil)
	srv.handleFocus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if srv.focus.ICAO != "EPKK" {
		t.Fatalf("POST focus %s", srv.focus.ICAO)
	}
}

func TestV015BuildDeparturesZeroRadius(t *testing.T) {
	got := buildDepartures(geo.DefaultFocus(), []flightRow{
		{Aircraft: opensky.Aircraft{ICAO24: "x", Callsign: "X", Lat: 51.11, Lon: 16.89, Velocity: 80}, Origin: "EPWR", Destination: "EPWA"},
	}, -1)
	if len(got) != 1 || !got[0].Approach {
		t.Fatalf("%+v", got)
	}
}
