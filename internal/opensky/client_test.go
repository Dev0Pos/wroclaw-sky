package opensky_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"wroclaw-sky/internal/opensky"
)

func TestFetchStatesParsesVector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/states/all" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("lamin") == "" || q.Get("lomax") == "" {
			t.Fatalf("missing bbox params: %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"time": 1700000000,
			"states": [][]any{
				{
					"abc123", " LOT123  ", "Poland",
					nil, nil,
					17.01, 51.10,
					10000.0, false,
					200.0, 90.0, 0.0,
					nil, 10000.0,
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	client := &opensky.Client{HTTP: srv.Client(), BaseURL: srv.URL}
	got, ts, err := client.FetchStates(opensky.Wroclaw)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Unix() != 1700000000 {
		t.Fatalf("time = %v", ts)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	ac := got[0]
	if ac.ICAO24 != "abc123" || ac.Callsign != "LOT123" || ac.Country != "Poland" {
		t.Fatalf("aircraft = %+v", ac)
	}
	if ac.Lat != 51.10 || ac.Lon != 17.01 || ac.OnGround {
		t.Fatalf("pos = %+v", ac)
	}
	if ac.AltitudeM != 10000 || ac.Velocity != 200 || ac.Track != 90 {
		t.Fatalf("motion = %+v", ac)
	}
}

func TestFetchStatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	client := &opensky.Client{HTTP: srv.Client(), BaseURL: srv.URL}
	_, _, err := client.FetchStates(opensky.Wroclaw)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchStatesNullStates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"time":1,"states":null}`))
	}))
	t.Cleanup(srv.Close)

	client := &opensky.Client{HTTP: srv.Client(), BaseURL: srv.URL}
	got, _, err := client.FetchStates(opensky.Wroclaw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d", len(got))
	}
}

func TestFormatHelpers(t *testing.T) {
	if opensky.FormatAlt(0) != "—" {
		t.Fatal("alt zero")
	}
	if opensky.FormatSpeed(0) != "—" {
		t.Fatal("speed zero")
	}
	if got := opensky.FormatAlt(1000); got == "—" {
		t.Fatalf("alt = %q", got)
	}
}
