package meta_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestEnrichADSBdbPrimary(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/aircraft/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"aircraft": map[string]string{
					"type": "737-800", "icao_type": "B738", "manufacturer": "Boeing",
					"registration": "SP-LWA", "registered_owner": "LOT",
					"url_photo_thumbnail": "https://example.com/p.jpg",
				},
			},
		})
	})
	mux.HandleFunc("/callsign/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"flightroute": map[string]any{
					"callsign": "LOT381",
					"airline":  map[string]string{"name": "LOT Polish Airlines"},
					"origin": map[string]string{
						"icao_code": "EPWA", "name": "Warsaw Chopin Airport", "municipality": "Warsaw",
					},
					"destination": map[string]string{
						"icao_code": "EDDF", "name": "Frankfurt am Main Airport", "municipality": "Frankfurt",
					},
				},
			},
		})
	})
	// hexdb paths should not be required
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e := meta.NewEnricher()
	e.HTTP = srv.Client()
	e.ADSBdbBaseURL = srv.URL
	e.BaseURL = "http://127.0.0.1:1" // force hex fallback to fail fast if used incorrectly

	d := e.Enrich(meta.Detail{ICAO24: "48abcd", Callsign: "LOT381", Lat: 51.1, Lon: 17.0})
	if d.Registration != "SP-LWA" || d.TypeCode != "B738" {
		t.Fatalf("aircraft: %+v", d)
	}
	if d.Origin != "EPWA" || d.Destination != "EDDF" || d.RouteSource != "adsbdb" {
		t.Fatalf("route: %+v", d)
	}
	if d.OriginCity != "Warsaw" || d.DestCity != "Frankfurt" {
		t.Fatalf("cities: %+v", d)
	}
	if d.PhotoURL == "" {
		t.Fatalf("expected photo url")
	}
}

func TestEnrichHexFallbackWhenADSBdbMissing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/aircraft/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/callsign/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/v1/aircraft/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"Registration": "SP-TEST", "ICAOTypeCode": "B738", "Type": "737-800",
			"Manufacturer": "Boeing", "RegisteredOwners": "LOT",
		})
	})
	mux.HandleFunc("/api/v1/route/icao/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"flight": "LOT9", "route": "EPWA-EPWR"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e := meta.NewEnricher()
	e.HTTP = srv.Client()
	e.ADSBdbBaseURL = srv.URL
	e.BaseURL = srv.URL

	d := e.Enrich(meta.Detail{ICAO24: "aabbcc", Callsign: "LOT9"})
	if d.Registration != "SP-TEST" || d.Origin != "EPWA" || d.RouteSource != "hexdb" {
		t.Fatalf("hex fallback: %+v", d)
	}
}

func TestEnrichViaUpstreamMeta(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/meta", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"icao24": "abc", "callsign": "LOT1",
			"registration": "SP-LWA", "type_code": "B738",
			"origin": "EPWA", "destination": "EDDF", "route": "EPWA-EDDF",
			"origin_city": "Warsaw", "dest_city": "Frankfurt", "route_source": "adsbdb",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e := meta.NewEnricher()
	e.UpstreamURL = srv.URL
	e.UpstreamToken = "tok"

	d := e.Enrich(meta.Detail{ICAO24: "abc", Callsign: "LOT1", Lat: 1, Lon: 2})
	if d.Registration != "SP-LWA" || d.Origin != "EPWA" || d.DestCity != "Frankfurt" {
		t.Fatalf("upstream enrich: %+v", d)
	}
}
