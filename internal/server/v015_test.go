package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wroclaw-sky/internal/geo"
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
