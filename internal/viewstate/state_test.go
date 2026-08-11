package viewstate_test

import (
	"net/url"
	"testing"

	"wroclaw-sky/internal/viewstate"
)

func TestParseAndEncodeRoundTrip(t *testing.T) {
	raw := "q=lot&airborne=1&alt=low&epwr=to&sort=epwr&airline=LOT&live=1&follow=0&alert=1&icao=abc123&focus=EPWA"
	s := viewstate.Parse(mustQuery(t, raw))
	if s.Q != "lot" || !s.Airborne || s.Alt != "low" || s.EPWR != "to" {
		t.Fatalf("filters: %+v", s)
	}
	if s.Sort != "epwr" || s.Airline != "LOT" || !s.Live || s.Follow || !s.Alert {
		t.Fatalf("flags: %+v", s)
	}
	if s.ICAO != "abc123" || s.Focus != "EPWA" {
		t.Fatalf("icao/focus = %q %q", s.ICAO, s.Focus)
	}
	enc := s.Encode()
	s2 := viewstate.Parse(mustQuery(t, enc))
	if s2 != s {
		t.Fatalf("round-trip\n got %+v\nwant %+v\nenc %q", s2, s, enc)
	}
}

func TestParseDefaultsAndInvalid(t *testing.T) {
	s := viewstate.Parse(nil)
	if s.Alt != "any" || s.EPWR != "any" || s.Sort != "callsign" || !s.Follow {
		t.Fatalf("defaults: %+v", s)
	}
	s = viewstate.Parse(url.Values{})
	if s.Alt != "any" || s.EPWR != "any" || s.Sort != "callsign" || !s.Follow {
		t.Fatalf("empty values: %+v", s)
	}
	s = viewstate.Parse(mustQuery(t, "alt=nope&epwr=x&sort=y&follow=1"))
	if s.Alt != "any" || s.EPWR != "any" || s.Sort != "callsign" || !s.Follow {
		t.Fatalf("invalid fallback: %+v", s)
	}
	// Omit defaults from encode
	if got := viewstate.Default().Encode(); got != "" {
		t.Fatalf("default encode = %q", got)
	}
}

func TestEncodeOmitsDefaults(t *testing.T) {
	s := viewstate.Default()
	s.Live = true
	s.ICAO = "aa"
	got := s.Encode()
	if got != "icao=aa&live=1" && got != "live=1&icao=aa" {
		t.Fatalf("encode = %q", got)
	}
}

func TestNewlyOnApproach(t *testing.T) {
	prev := map[string]bool{"a": true, "b": false}
	curr := map[string]bool{"a": true, "b": true, "c": true, "d": false}
	got := viewstate.NewlyOnApproach(prev, curr)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("got %#v", got)
	}
	if len(viewstate.NewlyOnApproach(nil, nil)) != 0 {
		t.Fatal("nil")
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
