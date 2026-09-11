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

func TestV015BuildDeparturesOriginFold(t *testing.T) {
	got := buildDepartures(geo.DefaultFocus(), []flightRow{
		{Aircraft: opensky.Aircraft{ICAO24: "a", Callsign: "LOT1", Lat: 51.15, Lon: 16.95, Velocity: 80}, Origin: " epwr ", Destination: "EPWA"},
		{Aircraft: opensky.Aircraft{ICAO24: "b", Callsign: "LOT2", Lat: 51.16, Lon: 16.96, Velocity: 80}, Origin: "EpWr", Destination: "EGLL"},
		{Aircraft: opensky.Aircraft{ICAO24: "c", Callsign: "LOT3", Lat: 51.17, Lon: 16.97, Velocity: 80}, Origin: "EPWA", Destination: "EPWR"},
	}, 0)
	if len(got) != 2 {
		t.Fatalf("origin fold/trim len=%d %+v", len(got), got)
	}
	seen := map[string]string{}
	for _, d := range got {
		seen[d.Callsign] = d.Destination
	}
	if seen["LOT1"] != "EPWA" || seen["LOT2"] != "EGLL" {
		t.Fatalf("%+v", got)
	}
}

func TestV015ShareFocusOffDoesNotResetAlertsOrBBox(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	routeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"flightroute": map[string]any{
					"origin":      map[string]any{"icao_code": "EPWA"},
					"destination": map[string]any{"icao_code": "EPWR"},
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

	boot := opensky.Aircraft{ICAO24: "boot1", Callsign: "LOT11", Lat: 51.15, Lon: 16.95, AltitudeM: 800, Velocity: 100}
	store.ApplySnapshot([]opensky.Aircraft{boot}, time.Now(), nil)
	enr.WarmRoutes([]meta.WarmItem{{ICAO24: boot.ICAO24, Callsign: boot.Callsign}}, time.Second)
	srv.evaluateAlerts()
	if n := len(srv.recentAlerts()); n != 0 {
		t.Fatalf("bootstrap events %d", n)
	}
	srv.alerts.mu.Lock()
	if !srv.alerts.bootstrapped {
		srv.alerts.mu.Unlock()
		t.Fatal("expected bootstrapped after first evaluate")
	}
	srv.alerts.mu.Unlock()

	prevBBox := store.BBox()
	srv.SetShareFocus(false)
	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/?focus=EPWA", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("index %d", rec.Code)
	}
	if srv.focus.ICAO != "EPWR" {
		t.Fatalf("SHARE_FOCUS=false must keep EPWR, got %s", srv.focus.ICAO)
	}
	if store.BBox() != prevBBox {
		t.Fatalf("SHARE_FOCUS=false must not recentre bbox: %+v → %+v", prevBBox, store.BBox())
	}
	srv.alerts.mu.Lock()
	still := srv.alerts.bootstrapped
	srv.alerts.mu.Unlock()
	if !still {
		t.Fatal("ignored GET ?focus= must not reset alert bootstrap")
	}

	next := opensky.Aircraft{ICAO24: "new1", Callsign: "LOT22", Lat: 51.16, Lon: 16.96, AltitudeM: 700, Velocity: 110}
	store.ApplySnapshot([]opensky.Aircraft{boot, next}, time.Now(), nil)
	enr.WarmRoutes([]meta.WarmItem{{ICAO24: next.ICAO24, Callsign: next.Callsign}}, time.Second)
	srv.evaluateAlerts()
	got := srv.recentAlerts()
	if len(got) == 0 {
		t.Fatal("new inbound after ignored share URL must still fire (bootstrap was not reset)")
	}
	seen := false
	for _, ev := range got {
		if ev.ICAO24 == "new1" && ev.Type == AlertApproach && ev.Focus == "EPWR" {
			seen = true
		}
		if ev.ICAO24 == "boot1" {
			t.Fatalf("bootstrapped inbound replayed: %+v", ev)
		}
	}
	if !seen {
		t.Fatalf("expected approach for new1, got %+v", got)
	}
}

func TestV015SameFocusShareURLDoesNotResetAlerts(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	routeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"flightroute": map[string]any{
					"origin":      map[string]any{"icao_code": "EPWA"},
					"destination": map[string]any{"icao_code": "EPWR"},
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

	boot := opensky.Aircraft{ICAO24: "same1", Callsign: "LOT33", Lat: 51.15, Lon: 16.95, AltitudeM: 800, Velocity: 100}
	store.ApplySnapshot([]opensky.Aircraft{boot}, time.Now(), nil)
	enr.WarmRoutes([]meta.WarmItem{{ICAO24: boot.ICAO24, Callsign: boot.Callsign}}, time.Second)
	srv.evaluateAlerts()

	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/?focus=EPWR", nil))
	if srv.focus.ICAO != "EPWR" {
		t.Fatalf("same ICAO switched? %s", srv.focus.ICAO)
	}
	srv.alerts.mu.Lock()
	still := srv.alerts.bootstrapped
	srv.alerts.mu.Unlock()
	if !still {
		t.Fatal("GET /?focus= current ICAO must not re-bootstrap alerts")
	}

	rec = httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/?focus=epwr", nil))
	srv.alerts.mu.Lock()
	still = srv.alerts.bootstrapped
	srv.alerts.mu.Unlock()
	if !still || srv.focus.ICAO != "EPWR" {
		t.Fatal("lowercase same ICAO must not reset bootstrap")
	}

	next := opensky.Aircraft{ICAO24: "same2", Callsign: "LOT44", Lat: 51.16, Lon: 16.96, AltitudeM: 700, Velocity: 110}
	store.ApplySnapshot([]opensky.Aircraft{boot, next}, time.Now(), nil)
	enr.WarmRoutes([]meta.WarmItem{{ICAO24: next.ICAO24, Callsign: next.Callsign}}, time.Second)
	srv.evaluateAlerts()
	found := false
	for _, ev := range srv.recentAlerts() {
		if ev.ICAO24 == "same2" {
			found = true
		}
		if ev.ICAO24 == "same1" {
			t.Fatalf("replayed bootstrapped inbound %+v", ev)
		}
	}
	if !found {
		t.Fatal("new inbound after same-ICAO share URL must fire")
	}
}

func TestV015ShareURLLowercasePreservesRadius(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetFocusRadiusKM(50)
	want := opensky.BBoxAround(52.1657, 20.9671, 50)

	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/?focus=epwa", nil))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	if srv.focus.ICAO != "EPWA" {
		t.Fatalf("lowercase share URL focus %s", srv.focus.ICAO)
	}
	if srv.focusRadiusKM != 50 {
		t.Fatalf("share URL must keep configured radius, got %v", srv.focusRadiusKM)
	}
	if store.BBox() != want {
		t.Fatalf("bbox %+v want %+v", store.BBox(), want)
	}
}

func TestV015LivePayloadKeepsDeadReckoningVectors(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	ac := opensky.Aircraft{
		ICAO24: "vec1", Callsign: "LOT55", Country: "Poland",
		Lat: 51.12, Lon: 16.90, AltitudeM: 1200,
		Velocity: 85.5, Track: 270, Vertical: -1.2, OnGround: false,
	}
	store.ApplySnapshot([]opensky.Aircraft{ac}, time.Unix(1_700_000_000, 0).UTC(), nil)

	payload := srv.aircraftPayload()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Type      string `json:"type"`
		UpdatedAt string `json:"updated_at"`
		Focus     string `json:"focus"`
		Aircraft  []struct {
			ICAO24   string  `json:"icao24"`
			Lat      float64 `json:"lat"`
			Lon      float64 `json:"lon"`
			Velocity float64 `json:"velocity"`
			Track    float64 `json:"track"`
			Vertical float64 `json:"vertical"`
			OnGround bool    `json:"on_ground"`
		} `json:"aircraft"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != "update" || decoded.Focus != "EPWR" || decoded.UpdatedAt == "" {
		t.Fatalf("envelope %+v", decoded)
	}
	if len(decoded.Aircraft) != 1 {
		t.Fatalf("aircraft %+v", decoded.Aircraft)
	}
	row := decoded.Aircraft[0]
	if row.ICAO24 != "vec1" || row.Lat != 51.12 || row.Lon != 16.90 {
		t.Fatalf("position %+v", row)
	}
	if row.Velocity != 85.5 || row.Track != 270 || row.Vertical != -1.2 || row.OnGround {
		t.Fatalf("vectors required for dead reckoning / climb badges: %+v", row)
	}

	rec := httptest.NewRecorder()
	srv.handleAPI(rec, httptest.NewRequest(http.MethodGet, "/api/aircraft", nil))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"velocity":85.5`) || !strings.Contains(rec.Body.String(), `"on_ground":false`) {
		t.Fatalf("GET /api/aircraft dropped DR fields: %s", rec.Body.String())
	}
}

func TestV015DescentBadgeOnFlights(t *testing.T) {
	store, _ := mockOpenSkyStore(t)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "up", Callsign: "LOTUP", Lat: 51.15, Lon: 16.95, Velocity: 100, Vertical: 1.2},
		{ICAO24: "dn", Callsign: "LOTDN", Lat: 51.16, Lon: 16.96, Velocity: 100, Vertical: -1.2},
		{ICAO24: "lv", Callsign: "LOTLV", Lat: 51.17, Lon: 16.97, Velocity: 100, Vertical: 0.1},
	}, time.Now(), nil)

	rec := httptest.NewRecorder()
	srv.handleFlights(rec, httptest.NewRequest(http.MethodGet, "/flights", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `title="Climbing"`) || !strings.Contains(body, "↑") {
		t.Fatal("climb badge missing")
	}
	if !strings.Contains(body, `title="Descending"`) || !strings.Contains(body, "↓") {
		t.Fatal("descent badge missing")
	}
	lvIdx := strings.Index(body, "LOTLV")
	if lvIdx < 0 {
		t.Fatal("level flight missing")
	}
	// Level |vs|<=0.5 must not get a climb/descent title on that row.
	rowEnd := strings.Index(body[lvIdx:], "</li>")
	if rowEnd < 0 {
		t.Fatal("row")
	}
	row := body[lvIdx : lvIdx+rowEnd]
	if strings.Contains(row, `title="Climbing"`) || strings.Contains(row, `title="Descending"`) {
		t.Fatalf("level flight should have no vs badge: %s", row)
	}
}
