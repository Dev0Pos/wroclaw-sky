package opensky

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientHelpersDefaults(t *testing.T) {
	c := &Client{}
	if c.base() != defaultBaseURL {
		t.Fatal("base")
	}
	if c.retryCount() != defaultRetries {
		t.Fatal("retries")
	}
	hc := c.httpClient()
	if hc.Timeout != defaultTimeout {
		t.Fatal("timeout")
	}
	c.Timeout = time.Second
	if c.httpClient().Timeout != time.Second {
		t.Fatal("custom timeout")
	}
	c.HTTP = &http.Client{}
	if c.retryCount() != 0 {
		t.Fatal("injected http retries")
	}
}

func TestParseStateEdges(t *testing.T) {
	if _, ok := parseState(nil); ok {
		t.Fatal("short")
	}
	// Missing lat/lon
	row := make([]json.RawMessage, 14)
	row[0] = []byte(`"aa"`)
	row[1] = []byte(`""`)
	row[2] = []byte(`"PL"`)
	row[5] = []byte(`null`)
	row[6] = []byte(`null`)
	if _, ok := parseState(row); ok {
		t.Fatal("null pos")
	}
	// Geo alt fallback + empty callsign → icao
	row[5] = []byte(`17.0`)
	row[6] = []byte(`51.0`)
	row[7] = []byte(`null`)
	row[8] = []byte(`false`)
	row[9] = []byte(`10.0`)
	row[10] = []byte(`20.0`)
	row[11] = []byte(`1.0`)
	row[13] = []byte(`900.0`)
	ac, ok := parseState(row)
	if !ok || ac.AltitudeM != 900 || ac.Callsign != "aa" {
		t.Fatalf("%+v ok=%v", ac, ok)
	}
}

func TestFetchStatesAuthAndTimeZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "u" || p != "p" {
			t.Fatalf("auth %v %q %q", ok, u, p)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"time": 0, "states": []any{}})
	}))
	t.Cleanup(srv.Close)
	c := &Client{HTTP: srv.Client(), BaseURL: srv.URL, Username: "u", Password: "p"}
	_, ts, err := c.FetchStates(Wroclaw)
	if err != nil || ts.IsZero() {
		t.Fatalf("ts=%v err=%v", ts, err)
	}
}

func TestFetchStatesNetworkError(t *testing.T) {
	retries := 0
	c := &Client{HTTP: &http.Client{Timeout: 50 * time.Millisecond}, BaseURL: "http://127.0.0.1:1", Retries: &retries}
	if _, _, err := c.FetchStates(Wroclaw); err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchStatesOnceBadURLAndDecode(t *testing.T) {
	c := &Client{BaseURL: "http://[", HTTP: &http.Client{Timeout: time.Second}}
	if _, _, err := c.fetchStatesOnce(Wroclaw); err == nil {
		t.Fatal("bad url parse")
	}

	prev := newHTTPRequest
	t.Cleanup(func() { newHTTPRequest = prev })
	newHTTPRequest = func(string, string, io.Reader) (*http.Request, error) {
		return nil, fmt.Errorf("newreq")
	}
	c = &Client{BaseURL: "http://example.com", HTTP: &http.Client{Timeout: time.Second}}
	if _, _, err := c.fetchStatesOnce(Wroclaw); err == nil {
		t.Fatal("newreq")
	}
	newHTTPRequest = prev

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not-json`))
	}))
	t.Cleanup(srv.Close)
	c = &Client{HTTP: srv.Client(), BaseURL: srv.URL}
	if _, _, err := c.fetchStatesOnce(Wroclaw); err == nil {
		t.Fatal("decode")
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"time": 1,
			"states": []any{
				[]any{"xx"}, // too short → skip
				[]any{"aa", "CS1", "PL", nil, nil, 17.0, 51.1, 1000.0, false, 10.0, 20.0, 0.0, nil, 1000.0},
			},
		})
	}))
	t.Cleanup(srv2.Close)
	c = &Client{HTTP: srv2.Client(), BaseURL: srv2.URL}
	list, _, err := c.fetchStatesOnce(Wroclaw)
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %v", list, err)
	}
}
