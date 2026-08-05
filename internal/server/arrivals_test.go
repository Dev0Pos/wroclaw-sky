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

func TestArrivalsBoardAndAirlineFilter(t *testing.T) {
	store := cache.New(nil, opensky.Wroclaw)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "near", Callsign: "LOT1", Lat: 51.15, Lon: 16.95, Velocity: 80, OnGround: false},
		{ICAO24: "far", Callsign: "RYR2", Lat: 51.50, Lon: 17.50, Velocity: 200, OnGround: false},
		{ICAO24: "out", Callsign: "WZZ3", Lat: 51.20, Lon: 17.00, Velocity: 100, OnGround: false},
		{ICAO24: "gnd", Callsign: "LOT4", Lat: 51.10, Lon: 16.89, Velocity: 0, OnGround: true},
	}, time.Now(), nil)

	routes := map[string][2]string{
		"near": {"EPWA", "EPWR"},
		"far":  {"EDDF", "EPWR"},
		"out":  {"EPWR", "EPWA"},
		"gnd":  {"EPWA", "EPWR"},
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
		{"near", "LOT1"}, {"far", "RYR2"}, {"out", "WZZ3"}, {"gnd", "LOT4"},
	} {
		_ = enrich.Enrich(meta.Detail{ICAO24: a.icao, Callsign: a.cs})
	}

	srv, err := server.New(store, enrich)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/flights", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="arrivals"`) {
		t.Fatalf("missing arrivals board: %s", body)
	}
	if !strings.Contains(body, "LOT1") || !strings.Contains(body, "RYR2") {
		t.Fatalf("expected inbound callsigns: %s", body)
	}
	iNear := strings.Index(body, "LOT1")
	iFar := strings.Index(body, "RYR2")
	if iNear < 0 || iFar < 0 || iNear > iFar {
		t.Fatalf("expected LOT1 before RYR2 in document order")
	}
	arrivalsIdx := strings.Index(body, `id="arrivals"`)
	listIdx := strings.Index(body, `id="flight-list"`)
	if arrivalsIdx < 0 || listIdx <= arrivalsIdx {
		t.Fatal("missing arrivals/list sections")
	}
	arrivalsHTML := body[arrivalsIdx:listIdx]
	if strings.Contains(arrivalsHTML, "WZZ3") || strings.Contains(arrivalsHTML, "LOT4") {
		t.Fatalf("arrivals should omit outbound/ground: %s", arrivalsHTML)
	}
	if !strings.Contains(body, `data-airline="LOT"`) || !strings.Contains(body, `data-airline="Ryanair"`) {
		t.Fatalf("expected data-airline attrs: %s", body)
	}
}

func TestIndexViewStateAndAirlines(t *testing.T) {
	srv, err := server.New(cache.New(nil, opensky.Wroclaw), nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?epwr=to&sort=epwr&airline=LOT&live=1&alert=1&q=lot&airborne=1&alt=low&follow=0", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, c := range []string{
		`id="filter-airline"`,
		`id="alert-approach"`,
		`value="to" selected`,
		`value="epwr" selected`,
		`value="LOT" selected`,
		`value="low" selected`,
		`syncViewURL`,
		`newlyOnApproach`,
		`Ryanair`,
		`Notification`,
		`checked`,
	} {
		if !strings.Contains(body, c) {
			t.Fatalf("index missing %q", c)
		}
	}
	if !strings.Contains(body, `id="filter-q" value="lot"`) && !strings.Contains(body, `value="lot"`) {
		t.Fatalf("expected q prefilled")
	}
}
