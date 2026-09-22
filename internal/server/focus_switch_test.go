package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
)

func stubLocalEnricher(srv *Server) {
	enr := meta.NewEnricher()
	enr.BaseURL = "http://127.0.0.1:1"
	enr.ADSBdbBaseURL = "http://127.0.0.1:1"
	enr.HTTP = &http.Client{Timeout: 50 * time.Millisecond}
	srv.enricher = enr
}

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
	start := strings.Index(body, "function applyRemoteFocus(data)")
	end := strings.Index(body, "function applyAircraftData(data)")
	if start < 0 || end <= start {
		t.Fatal("applyRemoteFocus block")
	}
	fn := body[start:end]
	for _, want := range []string{
		"Number.isNaN(lat)",
		"Number.isNaN(lon)",
		"radius: APPROACH_RADIUS_M",
		"Math.abs(FOCUS.lat - lat) < 1e-6",
		"approachBootstrapped = false",
	} {
		if !strings.Contains(fn, want) {
			t.Fatalf("applyRemoteFocus missing %q", want)
		}
	}
	if strings.Contains(fn, "40000") {
		t.Fatal("remote approach ring must use APPROACH_RADIUS_M, not a hardcoded 40 km")
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

// TestShareURLFocusDoesNotRefreshOrFireWebhooks locks the GET /?focus= blast-radius
// contract: share links switch process focus/bbox and reset alert bootstrap, but
// must not spend OpenSky credits, hit the fetcher, evaluate webhooks, or replace
// the in-memory snapshot. POST /api/focus is the operator path that refreshes.
func TestShareURLFocusDoesNotRefreshOrFireWebhooks(t *testing.T) {
	var osHits, upHits, hookHits atomic.Int64
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		osHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"time": time.Now().Unix(),
			"states": [][]any{{
				"waw3", "LOT77", "Poland",
				nil, nil,
				21.00, 52.18,
				800.0, false,
				100.0, 270.0, -1.0,
			}},
		})
	}))
	t.Cleanup(osSrv.Close)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "update",
			"aircraft": []map[string]any{{
				"icao24": "up1", "callsign": "UP1", "lat": 52.2, "lon": 21.0,
			}},
		})
	}))
	t.Cleanup(up.Close)

	client := &opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}
	zero := 0
	client.Retries = &zero
	store := cache.New(client, opensky.Wroclaw)
	store.UpstreamURL = up.URL
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "epwr1", Callsign: "LOT1", Lat: 51.11, Lon: 16.90, Velocity: 80},
	}, time.Now(), nil)

	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	hook := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hookHits.Add(1)
	}))
	t.Cleanup(hook.Close)
	srv.SetAlertWebhook(hook.URL)
	srv.SetAlertWebhookDigest(false)
	srv.evaluateAlerts()
	if n := len(srv.recentAlerts()); n != 0 {
		t.Fatalf("bootstrap events %d", n)
	}

	before := srv.refreshTotal.Load()
	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/?focus=EPWA", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("share URL %d", rec.Code)
	}
	if srv.focus.ICAO != "EPWA" {
		t.Fatalf("focus %s", srv.focus.ICAO)
	}
	wantFocus, ok := geo.LookupFocus("EPWA")
	if !ok {
		t.Fatal("EPWA lookup")
	}
	wantBox := opensky.BBoxAround(wantFocus.Lat, wantFocus.Lon, srv.focusRadiusKM)
	if store.BBox() != wantBox {
		t.Fatalf("bbox %+v want %+v", store.BBox(), wantBox)
	}
	if osHits.Load() != 0 {
		t.Fatalf("GET /?focus= must not query OpenSky (%d hits)", osHits.Load())
	}
	if upHits.Load() != 0 {
		t.Fatalf("GET /?focus= must not hit UpstreamURL (%d hits)", upHits.Load())
	}
	if hookHits.Load() != 0 {
		t.Fatalf("share URL must not POST webhooks, hits=%d", hookHits.Load())
	}
	if got := srv.refreshTotal.Load(); got != before {
		t.Fatalf("share URL must not refresh, refreshTotal %d → %d", before, got)
	}
	list, _, snapErr := store.Snapshot()
	if snapErr != nil {
		t.Fatal(snapErr)
	}
	if len(list) != 1 || list[0].ICAO24 != "epwr1" {
		t.Fatalf("share URL must keep the previous snapshot, got %+v", list)
	}
	srv.alerts.mu.Lock()
	reset := !srv.alerts.bootstrapped
	srv.alerts.mu.Unlock()
	if !reset {
		t.Fatal("GET /?focus= must still reset alert bootstrap")
	}
}

// TestPostFocusPublishesSSEWithNewFocusAndAircraft locks the Live-tab sync
// half of the airport-switch fix: refreshAndWarm must broadcast an update
// with the new ARP and the new-bbox snapshot so other browsers can
// applyRemoteFocus immediately.
func TestPostFocusPublishesSSEWithNewFocusAndAircraft(t *testing.T) {
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"time": time.Now().Unix(),
			"states": [][]any{{
				"waw3", "LOT77", "Poland",
				nil, nil,
				21.00, 52.18,
				800.0, false,
				100.0, 270.0, -1.0,
			}},
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
	stubLocalEnricher(srv)
	ch := srv.hub.subscribe()
	t.Cleanup(func() { srv.hub.unsubscribe(ch) })

	rec := httptest.NewRecorder()
	srv.handleFocus(rec, httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("focus switch %d %s", rec.Code, rec.Body.String())
	}

	select {
	case msg := <-ch:
		var payload struct {
			Type     string  `json:"type"`
			Focus    string  `json:"focus"`
			FocusLat float64 `json:"focus_lat"`
			FocusLon float64 `json:"focus_lon"`
			Aircraft []struct {
				ICAO24 string `json:"icao24"`
			} `json:"aircraft"`
		}
		if err := json.Unmarshal([]byte(msg), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Type != "update" || payload.Focus != "EPWA" {
			t.Fatalf("sse envelope %+v", payload)
		}
		want, ok := geo.LookupFocus("EPWA")
		if !ok {
			t.Fatal("EPWA lookup")
		}
		if payload.FocusLat != want.Lat || payload.FocusLon != want.Lon {
			t.Fatalf("sse focus coords lat=%v lon=%v want %+v", payload.FocusLat, payload.FocusLon, want)
		}
		if len(payload.Aircraft) != 1 || payload.Aircraft[0].ICAO24 != "waw3" {
			t.Fatalf("sse must carry the new-bbox snapshot, got %+v", payload.Aircraft)
		}
	case <-time.After(time.Second):
		t.Fatal("POST /api/focus must publish an SSE update for other Live clients")
	}
}

// TestPostFocusUsesUpstreamNotOpenSky locks UI-behind-fetcher deploys:
// refreshAndWarm uses store.Refresh() (UpstreamURL /api/fetch), not
// RefreshOpenSky(). Copying the fetcher path here would fail on cloud IPs
// that OpenSky blocks.
func TestPostFocusUsesUpstreamNotOpenSky(t *testing.T) {
	var osHits, upHits atomic.Int64
	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		osHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"time": 1, "states": []any{}})
	}))
	t.Cleanup(osSrv.Close)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upHits.Add(1)
		if r.URL.Path != "/api/fetch" {
			t.Errorf("upstream path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"updated_at": time.Unix(1_700_000_000, 0).UTC().Format(time.RFC3339),
			"aircraft": []map[string]any{{
				"icao24": "up1", "callsign": "UP1", "lat": 52.20, "lon": 21.00,
			}},
		})
	}))
	t.Cleanup(up.Close)

	client := &opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}
	zero := 0
	client.Retries = &zero
	store := cache.New(client, opensky.Wroclaw)
	store.UpstreamURL = up.URL
	store.HTTP = up.Client()
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "epwr1", Callsign: "LOT1", Lat: 51.11, Lon: 16.90},
	}, time.Now(), nil)

	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	stubLocalEnricher(srv)
	rec := httptest.NewRecorder()
	srv.handleFocus(rec, httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("focus %d %s", rec.Code, rec.Body.String())
	}
	if osHits.Load() != 0 {
		t.Fatalf("UI host POST /api/focus must not query OpenSky (%d hits)", osHits.Load())
	}
	if upHits.Load() == 0 {
		t.Fatal("UI host POST /api/focus must Refresh() via UpstreamURL")
	}
	list, _, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ICAO24 != "up1" {
		t.Fatalf("expected upstream snapshot, got %+v", list)
	}
}

func TestPostFocusRadiusKMAndCustomICAO(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetFocusRadiusKM(80)

	rec := httptest.NewRecorder()
	srv.handleFocus(rec, httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA&radius_km=0", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("radius 0: %d %s", rec.Code, rec.Body.String())
	}
	if srv.focus.ICAO != "EPWR" {
		t.Fatalf("invalid radius must not switch focus, got %s", srv.focus.ICAO)
	}

	rec = httptest.NewRecorder()
	srv.handleFocus(rec, httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA&radius_km=nope", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("radius nope: %d %s", rec.Code, rec.Body.String())
	}
	if srv.focus.ICAO != "EPWR" {
		t.Fatalf("invalid radius must not switch focus, got %s", srv.focus.ICAO)
	}

	rec = httptest.NewRecorder()
	srv.handleFocus(rec, httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA&radius_km=50", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("radius 50: %d %s", rec.Code, rec.Body.String())
	}
	if srv.focus.ICAO != "EPWA" || srv.focusRadiusKM != 50 {
		t.Fatalf("focus=%s radius=%v", srv.focus.ICAO, srv.focusRadiusKM)
	}
	want, ok := geo.LookupFocus("EPWA")
	if !ok {
		t.Fatal("EPWA")
	}
	if store.BBox() != opensky.BBoxAround(want.Lat, want.Lon, 50) {
		t.Fatalf("bbox %+v", store.BBox())
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWR", strings.NewReader(`{"icao":"XXXX","lat":"51.1000","lon":"17.0000","city":"Lab","radius_km":40}`))
	req.Header.Set("Content-Type", "application/json")
	srv.handleFocus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("custom json %d %s", rec.Code, rec.Body.String())
	}
	if srv.focus.ICAO != "XXXX" || srv.focus.City != "Lab" || srv.focus.Lat != 51.1 || srv.focus.Lon != 17.0 {
		t.Fatalf("custom focus %+v", srv.focus)
	}
	if srv.focusRadiusKM != 40 {
		t.Fatalf("json radius_km %v", srv.focusRadiusKM)
	}
	if store.BBox() != opensky.BBoxAround(51.1, 17.0, 40) {
		t.Fatalf("custom bbox %+v", store.BBox())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA&radius_km=55", strings.NewReader(`{"icao":"EPKK","radius_km":30}`))
	req.Header.Set("Content-Type", "application/json")
	srv.handleFocus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("precedence %d %s", rec.Code, rec.Body.String())
	}
	if srv.focus.ICAO != "EPKK" {
		t.Fatalf("JSON icao must win over query, got %s", srv.focus.ICAO)
	}
	if srv.focusRadiusKM != 55 {
		t.Fatalf("query radius_km must win when set, got %v", srv.focusRadiusKM)
	}
}
