package meta_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"wroclaw-sky/internal/meta"
)

func TestLookupAirport(t *testing.T) {
	a, ok := meta.LookupAirport("EPWR")
	if !ok || a.City == "" {
		t.Fatalf("EPWR = %+v ok=%v", a, ok)
	}
	if got := meta.FormatAirport("EPWA"); got == "" || got == "EPWA" {
		t.Fatalf("FormatAirport EPWA = %q", got)
	}
}

func TestEnrichRouteAndAircraft(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/aircraft/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"Registration":     "SP-LWA",
			"Manufacturer":     "Boeing",
			"ICAOTypeCode":     "B738",
			"Type":             "737-800",
			"RegisteredOwners": "LOT",
		})
	})
	mux.HandleFunc("/api/v1/route/icao/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"flight": "LOT381",
			"route":  "EPWA-EDDF",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e := meta.NewEnricher()
	e.HTTP = srv.Client()
	e.BaseURL = srv.URL

	d := e.Enrich(meta.Detail{
		ICAO24:   "48abcd",
		Callsign: "LOT381",
		Lat:      51.1,
		Lon:      17.0,
	})
	if d.Registration != "SP-LWA" || d.TypeCode != "B738" {
		t.Fatalf("aircraft fields: %+v", d)
	}
	if d.Origin != "EPWA" || d.Destination != "EDDF" {
		t.Fatalf("route: %+v", d)
	}
	if d.OriginCity == "" || d.DestCity == "" {
		t.Fatalf("expected city names, got origin=%q dest=%q", d.OriginCity, d.DestCity)
	}
}
