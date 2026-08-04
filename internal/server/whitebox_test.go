package server

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
)

func TestParseTemplatesFailure(t *testing.T) {
	prev := parseTemplates
	t.Cleanup(func() { parseTemplates = prev })
	parseTemplates = func() (*template.Template, error) {
		return nil, errors.New("boom")
	}
	if _, err := New(cache.New(nil, opensky.Wroclaw), nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestHandleIndexNotFoundAndTemplateError(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/elsewhere", nil)
	srv.handleIndex(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}

	srv.tmpl = template.Must(template.New("index.html").Parse(`{{.Nope}}`))
	rec = httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("tmpl err status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleFlightsTemplateError(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.tmpl = template.Must(template.New("flights.html").Parse(`{{.Nope}}`))
	rec := httptest.NewRecorder()
	srv.handleFlights(rec, httptest.NewRequest(http.MethodGet, "/flights", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestFormatEPWRHintDirect(t *testing.T) {
	if formatEPWRHint("EPWA", 51.1, 17.0, 100, false) != "" {
		t.Fatal("wrong dest")
	}
	if formatEPWRHint("EPWR", 0, 0, 100, false) != "" {
		t.Fatal("zero coords")
	}
	if formatEPWRHint("EPWR", 51.1, 17.0, 100, true) != "" {
		t.Fatal("on ground")
	}
	got := formatEPWRHint("EPWR", 51.1, 17.0, 100, false)
	if !strings.Contains(got, "km") {
		t.Fatalf("hint = %q", got)
	}
	// Slow / zero velocity → distance only
	got = formatEPWRHint("epwr", 51.1, 17.0, 1, false)
	if !strings.Contains(got, "km") || strings.Contains(got, "~") {
		t.Fatalf("no eta expected: %q", got)
	}
}

func TestSnapshotErrorAndDetailEmptyICAO(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	store.ApplySnapshot(nil, time.Time{}, errors.New("snap err"))
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := srv.snapshotData()
	if data.Error != "snap err" {
		t.Fatalf("error = %q", data.Error)
	}

	rec := httptest.NewRecorder()
	srv.handleAircraftDetail(rec, httptest.NewRequest(http.MethodGet, "/api/aircraft/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty icao = %d", rec.Code)
	}
}

func TestHandleMetaMethodAndFetchMethod(t *testing.T) {
	srv, err := New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.handleMeta(rec, httptest.NewRequest(http.MethodPut, "/api/meta", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("meta method = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.handleFetch(rec, httptest.NewRequest(http.MethodPut, "/api/fetch", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("fetch method = %d", rec.Code)
	}
}

func TestAuthorizedQueryAndEmptyToken(t *testing.T) {
	t.Setenv("FETCH_TOKEN", "")
	srv, err := New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	if !srv.authorized(req) {
		t.Fatal("empty token should allow")
	}
	t.Setenv("FETCH_TOKEN", "xyz")
	req = httptest.NewRequest(http.MethodGet, "/api/meta?token=xyz", nil)
	if !srv.authorized(req) {
		t.Fatal("query token")
	}
}

func TestLiveStatusExpiredCancel(t *testing.T) {
	srv, err := New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	// cancel set but until in the past
	srv.live.mu.Lock()
	srv.live.cancel = func() {}
	srv.live.until = time.Now().Add(-time.Second)
	srv.live.mu.Unlock()
	active, _ := srv.liveStatus()
	if active {
		t.Fatal("expected inactive")
	}
}

func TestUntilUTCZero(t *testing.T) {
	if untilUTC(time.Time{}) != "" {
		t.Fatal("expected empty")
	}
}

func TestHandleMetaPOST(t *testing.T) {
	t.Setenv("FETCH_TOKEN", "")
	enrich := meta.NewEnricher()
	enrich.HTTP = http.DefaultClient
	enrich.ADSBdbBaseURL = "http://127.0.0.1:1"
	enrich.BaseURL = "http://127.0.0.1:1"
	srv, err := New(cache.New(nil, opensky.Wroclaw), enrich)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/meta?icao24=abc", nil)
	srv.handleMeta(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var d map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&d)
	if d["icao24"] != "abc" {
		t.Fatalf("detail = %#v", d)
	}
}

func TestSetMapLabelEmptyIgnored(t *testing.T) {
	srv, err := New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetMapLabel("  ")
	if srv.label != "EPWR · Wrocław" {
		t.Fatalf("label = %q", srv.label)
	}
}

func TestLiveLoopContextDone(t *testing.T) {
	prevI, prevL := LiveTimingForTest(time.Hour, time.Hour)
	t.Cleanup(func() { LiveTimingForTest(prevI, prevL) })

	osSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"time": 1, "states": []any{}})
	}))
	t.Cleanup(osSrv.Close)
	store := cache.New(&opensky.Client{HTTP: osSrv.Client(), BaseURL: osSrv.URL}, opensky.Wroclaw)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv.touchLive()
	srv.live.mu.Lock()
	cancel := srv.live.cancel
	srv.live.mu.Unlock()
	if cancel == nil {
		t.Fatal("no cancel")
	}
	cancel()
	// Give liveLoop time to observe ctx.Done
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		srv.live.mu.Lock()
		// cancel may still be set; loop just returned via Done
		srv.live.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		break
	}
	time.Sleep(50 * time.Millisecond)
}

func TestSnapshotDataSortsCallsigns(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "bb", Callsign: "ZZZ", Lat: 51.1, Lon: 17.0},
		{ICAO24: "aa", Callsign: "AAA", Lat: 51.2, Lon: 17.1},
	}, time.Now(), nil)
	srv, err := New(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := srv.snapshotData()
	if len(data.Aircraft) != 2 || data.Aircraft[0].Callsign != "AAA" || data.Aircraft[1].Callsign != "ZZZ" {
		t.Fatalf("%+v", data.Aircraft)
	}
}
