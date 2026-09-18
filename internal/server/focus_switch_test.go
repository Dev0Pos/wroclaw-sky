package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
)

// TestPostFocusRefreshesSnapshotForNewBBox locks the contract that switching
// airports via POST /api/focus must fetch the new bbox before the handler
// returns — otherwise the UI pans to the new ARP over the previous airport's
// aircraft until the next Live poll.
func TestPostFocusRefreshesSnapshotForNewBBox(t *testing.T) {
	var lastLamin string
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastLamin = r.URL.Query().Get("lamin")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"time": time.Now().Unix(),
			"states": [][]any{
				{
					"waw3", " LOT77  ", "Poland",
					nil, nil,
					21.00, 52.18,
					800.0, false,
					100.0, 270.0, -1.0,
					nil, 800.0,
				},
			},
		})
	}))
	t.Cleanup(osSrv.Close)
	client := &opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}
	zero := 0
	client.Retries = &zero
	store := cache.New(client, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "epwr1", Callsign: "LOT1", Lat: 51.11, Lon: 16.90, Velocity: 80},
	}, time.Now(), nil)

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
	srv.SetApproachRadiusM(100000)

	enr.WarmRoutes([]meta.WarmItem{{ICAO24: "waw3", Callsign: "LOT77"}}, time.Second)

	srv.evaluateAlerts() // bootstrap at EPWR
	if n := len(srv.recentAlerts()); n != 0 {
		t.Fatalf("bootstrap events %d", n)
	}

	rec := httptest.NewRecorder()
	srv.handleFocus(rec, httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("focus switch %d %s", rec.Code, rec.Body.String())
	}
	if srv.focus.ICAO != "EPWA" {
		t.Fatalf("focus %s", srv.focus.ICAO)
	}

	lamin, err := strconv.ParseFloat(lastLamin, 64)
	if err != nil {
		t.Fatalf("lamin %q: %v", lastLamin, err)
	}
	// EPWA (~52.17) 80 km box sits north of the default Wrocław lamin (~50.90).
	if lamin < 51.2 {
		t.Fatalf("POST /api/focus must query the new bbox, lamin=%v", lamin)
	}

	list, _, snapErr := store.Snapshot()
	if snapErr != nil {
		t.Fatal(snapErr)
	}
	if len(list) != 1 || list[0].ICAO24 != "waw3" {
		t.Fatalf("expected new-bbox snapshot, got %+v", list)
	}

	if n := len(srv.recentAlerts()); n != 0 {
		t.Fatalf("already-inbound waw3 replayed as %d alerts", n)
	}
	srv.evaluateAlerts()
	if n := len(srv.recentAlerts()); n != 0 {
		t.Fatalf("stable inbound still fired %d alerts", n)
	}

	payload := srv.aircraftPayload()
	if payload["focus"] != "EPWA" {
		t.Fatalf("payload focus %v", payload["focus"])
	}
	want, ok := geo.LookupFocus("EPWA")
	if !ok {
		t.Fatal("EPWA lookup")
	}
	lat, _ := payload["focus_lat"].(float64)
	lon, _ := payload["focus_lon"].(float64)
	if lat != want.Lat || lon != want.Lon {
		t.Fatalf("focus coords lat=%v lon=%v want %+v", lat, lon, want)
	}
}

func TestLivePayloadAndIndexSyncRemoteFocus(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"function applyRemoteFocus(data)",
		"applyRemoteFocus(data);",
		"approachBootstrapped = false",
		"data.focus_lat",
		"data.focus_lon",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}

	payload := srv.aircraftPayload()
	if payload["focus"] != "EPWR" {
		t.Fatalf("default focus %v", payload["focus"])
	}
	if _, ok := payload["focus_lat"].(float64); !ok {
		t.Fatalf("focus_lat %T %v", payload["focus_lat"], payload["focus_lat"])
	}
	if _, ok := payload["focus_lon"].(float64); !ok {
		t.Fatalf("focus_lon %T %v", payload["focus_lon"], payload["focus_lon"])
	}
}
