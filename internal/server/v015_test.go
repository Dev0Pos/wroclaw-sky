package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func TestV015DeparturesAPIAndUI(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/departures", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"departures"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/departures?download=1", nil))
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "departures.json") {
		t.Fatal(rec.Header())
	}
	if !strings.Contains(rec.Body.String(), `"exported_at"`) {
		t.Fatal(rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?departures=0&predict=sel", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"departures-toggle", "predict-mode", "copy-view-link", "BOOT_PREDICT",
		"SHARE_FOCUS_ENABLED", "approachCircle", "paintUpdatedAge",
		`id="departures-toggle" type="checkbox" class="rounded border-slate-600 bg-slate-900 text-sky-500" />`,
		`BOOT_PREDICT = "sel"`,
		"Focus → ",
		"vs > 0.5",
		"mode === 'sel'",
		"el.dataset.updatedMs",
		"dashArray: '4 6'",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestV015ShareFocusGate(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetShareFocus(false)
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?focus=EPWA", nil))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "SHARE_FOCUS_ENABLED = false") {
		t.Fatal("expected SHARE_FOCUS_ENABLED false")
	}
	if !strings.Contains(body, "Share focus disabled") {
		t.Fatal("expected share-disabled toast string")
	}
	// Process focus must remain EPWR when shareFocus is off.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/focus", nil))
	var focusPayload struct {
		Focus struct {
			ICAO string `json:"icao"`
		} `json:"focus"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &focusPayload); err != nil {
		t.Fatal(err)
	}
	if focusPayload.Focus.ICAO != "EPWR" {
		t.Fatalf("GET ?focus= must be ignored when shareFocus=false, got %q", focusPayload.Focus.ICAO)
	}

	// POST /api/focus still works.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/focus?icao=EPWA", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST focus %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/focus", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &focusPayload); err != nil {
		t.Fatal(err)
	}
	if focusPayload.Focus.ICAO != "EPWA" {
		t.Fatalf("POST must switch focus, got %q", focusPayload.Focus.ICAO)
	}

	srv.SetFocus(geo.DefaultFocus())
	srv.SetShareFocus(true)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?focus=EPWA", nil))
	if !strings.Contains(rec.Body.String(), "SHARE_FOCUS_ENABLED = true") {
		t.Fatal("share on")
	}
}

func TestV015DeparturesDownloadContract(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/departures", nil))
	if rec.Header().Get("Content-Disposition") != "" {
		t.Fatal(rec.Header())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["focus"] != "EPWR" {
		t.Fatalf("%#v", payload)
	}
	if _, ok := payload["exported_at"]; ok {
		t.Fatal("inline must omit exported_at")
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/departures?export=1", nil))
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "departures.json") {
		t.Fatal(rec.Header())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/departures?download=true", nil))
	if rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("only download=1: %v", rec.Header())
	}
}

func TestV015DefaultTogglesAndClimbBadge(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	def := rec.Body.String()
	if !strings.Contains(def, `id="departures-toggle" type="checkbox" class="rounded border-slate-600 bg-slate-900 text-sky-500" checked`) {
		t.Fatal("default departures checked")
	}
	if !strings.Contains(def, `Predict: all`) || !strings.Contains(def, `BOOT_PREDICT = "all"`) {
		t.Fatal("default predict all")
	}
	for _, want := range []string{
		"mode === '0'", "mode === 'sel'", "el.dataset.updatedMs",
		"paintUpdatedAge()", "dashArray: '4 6'", "vs > 0.5", "vs < -0.5",
		"navigator.clipboard.writeText",
	} {
		if !strings.Contains(def, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestV015DeparturesAPIFiltersInboundGroundAndZeroCoord(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "out", Callsign: "LOT1", Lat: 51.15, Lon: 16.95, Velocity: 100, OnGround: false},
		{ICAO24: "inb", Callsign: "LOT2", Lat: 51.16, Lon: 16.96, Velocity: 100, OnGround: false},
		{ICAO24: "gnd", Callsign: "LOT3", Lat: 51.10, Lon: 16.89, Velocity: 0, OnGround: true},
		{ICAO24: "nocoord", Callsign: "LOT4", Lat: 0, Lon: 0, Velocity: 100, OnGround: false},
	}, time.Now(), nil)

	routes := map[string][2]string{
		"out":     {"EPWR", "EPWA"},
		"inb":     {"EPWA", "EPWR"},
		"gnd":     {"EPWR", "EPWA"},
		"nocoord": {"EPWR", "EGLL"},
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		icao := r.URL.Query().Get("icao24")
		od := routes[icao]
		_ = json.NewEncoder(w).Encode(meta.Detail{
			ICAO24: icao, Registration: "SP-X",
			Origin: od[0], Destination: od[1], Route: od[0] + "-" + od[1],
		})
	}))
	t.Cleanup(up.Close)
	enrich := meta.NewEnricher()
	enrich.UpstreamURL = up.URL
	enrich.HTTP = &http.Client{Timeout: time.Second}
	enrich.ADSBdbBaseURL = "http://127.0.0.1:1"
	enrich.BaseURL = "http://127.0.0.1:1"
	for _, a := range []struct{ icao, cs string }{
		{"out", "LOT1"}, {"inb", "LOT2"}, {"gnd", "LOT3"}, {"nocoord", "LOT4"},
	} {
		_ = enrich.Enrich(meta.Detail{ICAO24: a.icao, Callsign: a.cs})
	}

	srv, err := server.New(store, enrich)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/departures", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatal(rec.Header().Get("Content-Type"))
	}
	var payload struct {
		Focus      string `json:"focus"`
		Count      int    `json:"count"`
		Departures []struct {
			ICAO24      string `json:"icao24"`
			Callsign    string `json:"callsign"`
			Destination string `json:"destination"`
			Hint        string `json:"hint"`
			Approach    bool   `json:"approach"`
		} `json:"departures"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Focus != "EPWR" || payload.Count != 1 || len(payload.Departures) != 1 {
		t.Fatalf("%+v", payload)
	}
	dep := payload.Departures[0]
	if dep.ICAO24 != "out" || dep.Callsign != "LOT1" || dep.Destination != "EPWA" {
		t.Fatalf("%+v", dep)
	}
	if dep.Hint == "" || !dep.Approach {
		t.Fatalf("near outbound should be approach/near: %+v", dep)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/departures", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST %d", rec.Code)
	}
}

func TestV015DeadReckoningAndPredictContracts(t *testing.T) {
	store := mockOS11(t)
	srv, err := server.New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"const DR_MS = 1000;",
		"const DR_MAX_SEC = 50;",
		"const DR_MIN_VEL = 15;",
		"function deadReckonLatLng(a, nowMs)",
		"a.on_ground) return null;",
		"if (vel < DR_MIN_VEL || Number.isNaN(track)) return null;",
		"Math.min(DR_MAX_SEC, (nowMs - base) / 1000)",
		"if (playbackActive) return;",
		"liveBtn.getAttribute('aria-pressed') !== 'true'",
		"parseSnapshotAtMs(data)",
		"data.updated_at",
		"u.searchParams.set('departures', '0')",
		"u.searchParams.set('predict', pred)",
		"Share focus disabled — URL focus= is display-only",
		"method: 'POST'",
		"/api/focus?icao=",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}

	start := strings.Index(body, "function tickDeadReckoning()")
	end := strings.Index(body, "function applyAircraftData(")
	if start < 0 || end <= start {
		t.Fatal("tickDeadReckoning block")
	}
	tick := body[start:end]
	if strings.Contains(tick, "fetch(") || strings.Contains(tick, "/api/live") || strings.Contains(tick, "/api/aircraft") {
		t.Fatalf("dead reckoning must not fetch OpenSky/upstream: %s", tick)
	}

	pStart := strings.Index(body, "function upsertPredict(")
	pEnd := strings.Index(body, "function parseSnapshotAtMs(")
	if pStart < 0 || pEnd <= pStart {
		t.Fatal("upsertPredict block")
	}
	pred := body[pStart:pEnd]
	if !strings.Contains(pred, "if (mode === '0')") {
		t.Fatal("predict=0 must clear tracks")
	}
	if !strings.Contains(pred, "if (mode === 'sel' && icao !== selectedIcao") {
		t.Fatal("predict=sel must keep selected/inbound only")
	}
}
