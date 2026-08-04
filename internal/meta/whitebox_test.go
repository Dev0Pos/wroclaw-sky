package meta

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultsAndCachedRouteMiss(t *testing.T) {
	e := &Enricher{} // zero value — exercises hexBase/adsbdbBase/client defaults
	if e.hexBase() == "" || e.adsbdbBase() == "" || e.client() == nil {
		t.Fatal("defaults")
	}
	if _, ok := e.CachedRoute("nope", "X"); ok {
		t.Fatal("miss")
	}
	// Expired cache entry
	e.cache = map[string]cacheEntry{
		"aa|CS": {at: time.Now().Add(-time.Hour), data: Detail{Origin: "EPWA", Destination: "EPWR"}},
	}
	if _, ok := e.CachedRoute("aa", "CS"); ok {
		t.Fatal("expired")
	}
	e.cache["aa|CS"] = cacheEntry{at: time.Now(), data: Detail{Registration: "SP"}}
	if _, ok := e.CachedRoute("aa", "CS"); ok {
		t.Fatal("no route fields")
	}
}

func TestFormatAirportBranches(t *testing.T) {
	loadAirports()
	airportsMap["TSTA"] = Airport{Name: "Name Only"}
	airportsMap["TSTB"] = Airport{City: "CityOnly"}
	airportsMap["TSTC"] = Airport{}
	if got := FormatAirport("TSTA"); got != "Name Only (TSTA)" {
		t.Fatalf("name = %q", got)
	}
	if got := FormatAirport("TSTB"); got != "CityOnly (TSTB)" {
		t.Fatalf("city = %q", got)
	}
	if got := FormatAirport("TSTC"); got != "TSTC" {
		t.Fatalf("empty meta = %q", got)
	}
}

func TestWarmRoutesBudgetAndEmpty(t *testing.T) {
	e := NewEnricher()
	e.WarmRoutes(nil, time.Second)
	e.WarmRoutes([]WarmItem{{}}, 0)
	e.HTTP = &http.Client{Timeout: time.Millisecond}
	e.ADSBdbBaseURL = "http://127.0.0.1:1"
	e.BaseURL = "http://127.0.0.1:1"
	e.WarmRoutes([]WarmItem{{ICAO24: "aa", Callsign: "LOT1"}, {ICAO24: "bb", Callsign: "LOT2"}}, time.Millisecond)
}

func TestFetchHexEmptyICAO(t *testing.T) {
	e := NewEnricher()
	if _, err := e.fetchHexAircraft(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnrichHexDBAlreadyComplete(t *testing.T) {
	e := NewEnricher()
	out := e.enrichHexDB(Detail{Registration: "SP", TypeCode: "B738"})
	if out.Registration != "SP" {
		t.Fatal(out)
	}
}

func TestMergeEnrichmentPhotoOperator(t *testing.T) {
	base := Detail{}
	extra := Detail{
		Registration: "R", TypeCode: "T", TypeName: "N", Manufacturer: "M",
		Operator: "O", PhotoURL: "P", Route: "A-B", RouteSource: "x",
		Origin: "A", Destination: "B", OriginName: "An", DestName: "Bn",
		OriginCity: "Ac", DestCity: "Bc",
	}
	got := mergeEnrichment(base, extra)
	if got.PhotoURL != "P" || got.Operator != "O" || got.Route != "A-B" {
		t.Fatalf("%+v", got)
	}
}

func TestFetchADSBdbErrors(t *testing.T) {
	e := NewEnricher()
	e.HTTP = &http.Client{Timeout: time.Millisecond}
	e.ADSBdbBaseURL = "http://127.0.0.1:1"
	if _, err := e.fetchADSBdbAircraft(""); err == nil {
		t.Fatal("empty icao")
	}
	if _, err := e.fetchADSBdbRoute(""); err == nil {
		t.Fatal("empty cs")
	}
	if _, err := e.fetchADSBdbAircraft("ABC"); err == nil {
		t.Fatal("dial")
	}
}

func TestEnrichADSBdbNoCallsignAndAirline(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/aircraft/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"aircraft": map[string]string{
					"type": "A320", "icao_type": "A320", "manufacturer": "Airbus",
					"registration": "SP-AAA", "registered_owner": "",
				},
			},
		})
	})
	mux.HandleFunc("/callsign/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"flightroute": map[string]any{
					"airline": map[string]string{"name": "Test Air"},
					"origin": map[string]string{
						"icao_code": "EPWA", "name": "Warsaw", "municipality": "Warsaw",
					},
					"destination": map[string]string{
						"icao_code": "EPWR", "name": "Wroclaw", "municipality": "Wroclaw",
					},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e := NewEnricher()
	e.HTTP = srv.Client()
	e.ADSBdbBaseURL = srv.URL
	e.BaseURL = "http://127.0.0.1:1"

	// No callsign / callsign == icao → rtErr = no callsign
	out := e.enrichADSBdb(Detail{ICAO24: "abc123", Callsign: "abc123"})
	if out.Registration != "SP-AAA" {
		t.Fatalf("aircraft: %+v", out)
	}
	out = e.enrichADSBdb(Detail{ICAO24: "abc123", Callsign: ""})
	if out.Registration != "SP-AAA" {
		t.Fatalf("empty cs: %+v", out)
	}
	// Airline fills operator when aircraft owner empty
	out = e.enrichADSBdb(Detail{ICAO24: "abc123", Callsign: "TST123"})
	if out.Operator != "Test Air" || out.Route != "EPWA-EPWR" {
		t.Fatalf("airline/route: %+v", out)
	}
}

func TestFetchADSBdbBadJSONAndNil(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/aircraft/BAD1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not-json`))
	})
	mux.HandleFunc("/aircraft/BAD2", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"aircraft": nil}})
	})
	mux.HandleFunc("/aircraft/OK", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusTeapot)
	})
	mux.HandleFunc("/callsign/BAD1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not-json`))
	})
	mux.HandleFunc("/callsign/BAD2", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"flightroute": nil}})
	})
	mux.HandleFunc("/callsign/OK", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e := NewEnricher()
	e.HTTP = srv.Client()
	e.ADSBdbBaseURL = srv.URL

	if _, err := e.fetchADSBdbAircraft("BAD1"); err == nil {
		t.Fatal("bad json aircraft")
	}
	if _, err := e.fetchADSBdbAircraft("BAD2"); err == nil {
		t.Fatal("nil aircraft")
	}
	if _, err := e.fetchADSBdbAircraft("OK"); err == nil {
		t.Fatal("non-200 aircraft")
	}
	if _, err := e.fetchADSBdbRoute("BAD1"); err == nil {
		t.Fatal("bad json route")
	}
	if _, err := e.fetchADSBdbRoute("BAD2"); err == nil {
		t.Fatal("nil route")
	}
	if _, err := e.fetchADSBdbRoute("OK"); err == nil {
		t.Fatal("non-200 route")
	}
}

func TestEnrichIncompleteGapFill(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/aircraft/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"aircraft": map[string]string{
					"type": "737", "icao_type": "B738", "manufacturer": "Boeing",
					"registration": "SP-GAP", "registered_owner": "LOT",
				},
			},
		})
	})
	mux.HandleFunc("/callsign/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e := NewEnricher()
	e.HTTP = srv.Client()
	e.ADSBdbBaseURL = srv.URL
	e.BaseURL = "http://127.0.0.1:1"
	// Has registration but missing type → incomplete gap-fill
	d := e.Enrich(Detail{ICAO24: "gap01", Callsign: "GAP1", Registration: "SP-OLD"})
	if d.TypeCode != "B738" {
		t.Fatalf("gap-fill: %+v", d)
	}
}

func TestWarmRoutesSkipCacheAndCancel(t *testing.T) {
	e := NewEnricher()
	e.cache = map[string]cacheEntry{
		"aa|CS": {at: time.Now(), data: Detail{Registration: "SP"}},
	}
	// empty ICAO skipped; cached identity skipped
	e.WarmRoutes([]WarmItem{{ICAO24: ""}, {ICAO24: "aa", Callsign: "CS"}}, time.Second)

	// Short budget + many items → ctx cancel paths
	e.HTTP = &http.Client{Timeout: 2 * time.Second}
	e.ADSBdbBaseURL = "http://127.0.0.1:1"
	e.BaseURL = "http://127.0.0.1:1"
	items := make([]WarmItem, 40)
	for i := range items {
		items[i] = WarmItem{ICAO24: fmt.Sprintf("%06x", i), Callsign: fmt.Sprintf("W%03d", i)}
	}
	e.WarmRoutes(items, time.Nanosecond)
}

func TestFetchUpstreamMetaBranches(t *testing.T) {
	e := NewEnricher()
	e.UpstreamURL = "://bad"
	if _, err := e.fetchUpstreamMeta("aa", "CS"); err == nil {
		t.Fatal("parse")
	}

	prev := newHTTPRequest
	t.Cleanup(func() { newHTTPRequest = prev })
	newHTTPRequest = func(string, string, io.Reader) (*http.Request, error) {
		return nil, fmt.Errorf("newreq")
	}
	e.UpstreamURL = "http://example.com"
	if _, err := e.fetchUpstreamMeta("aa", "CS"); err == nil {
		t.Fatal("newreq")
	}
	newHTTPRequest = prev

	e.UpstreamURL = "http://127.0.0.1:1"
	e.UpstreamToken = "tok"
	if _, err := e.fetchUpstreamMeta("aa", "CS"); err == nil {
		t.Fatal("dial")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("icao24") {
		case "badcode":
			http.Error(w, "no", http.StatusBadGateway)
		case "badjson":
			_, _ = w.Write([]byte(`{`))
		default:
			_ = json.NewEncoder(w).Encode(Detail{ICAO24: "ok", Registration: "SP"})
		}
	}))
	t.Cleanup(srv.Close)
	e.UpstreamURL = srv.URL
	e.UpstreamToken = "t"
	if _, err := e.fetchUpstreamMeta("badcode", "CS"); err == nil {
		t.Fatal("status")
	}
	if _, err := e.fetchUpstreamMeta("badjson", "CS"); err == nil {
		t.Fatal("json")
	}
	d, err := e.fetchUpstreamMeta("ok", "CS")
	if err != nil || d.Registration != "SP" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestFetchHexAircraftBranches(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/aircraft/nf", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})
	mux.HandleFunc("/api/v1/aircraft/err", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "x", http.StatusInternalServerError)
	})
	mux.HandleFunc("/api/v1/aircraft/bad", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	e := NewEnricher()
	e.HTTP = srv.Client()
	e.BaseURL = srv.URL
	if _, err := e.fetchHexAircraft("nf"); err == nil {
		t.Fatal("404")
	}
	if _, err := e.fetchHexAircraft("err"); err == nil {
		t.Fatal("500")
	}
	if _, err := e.fetchHexAircraft("bad"); err == nil {
		t.Fatal("json")
	}
}

func TestGetNewRequestError(t *testing.T) {
	e := NewEnricher()
	if _, _, err := e.get(":"); err == nil {
		t.Fatal("expected newrequest error")
	}
}
