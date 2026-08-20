package viewstate_test

import (
	"net/url"
	"strings"
	"testing"

	"wroclaw-sky/internal/viewstate"
)

func TestParseAndEncodeRoundTrip(t *testing.T) {
	raw := "q=lot&airborne=1&alt=low&epwr=to&sort=epwr&airline=LOT&live=1&follow=0&alert=1&alert_low=1&alert_airline=LO&mute=aa,bb&icao=abc123&focus=EPWA&pb_at=100&pb_from=50&pb_to=200&pb_speed=2"
	s := viewstate.Parse(mustQuery(t, raw))
	if s.Q != "lot" || !s.Airborne || s.Alt != "low" || s.EPWR != "to" {
		t.Fatalf("filters: %+v", s)
	}
	if s.Sort != "epwr" || s.Airline != "LOT" || !s.Live || s.Follow || !s.Alert || !s.AlertLow {
		t.Fatalf("flags: %+v", s)
	}
	if s.AlertAirline != "LO" || len(s.Mute) != 2 || s.Mute[0] != "aa" || s.Mute[1] != "bb" {
		t.Fatalf("alert rules %+v", s)
	}
	if s.ICAO != "abc123" || s.Focus != "EPWA" {
		t.Fatalf("icao/focus = %q %q", s.ICAO, s.Focus)
	}
	if s.PBAt != 100 || s.PBFrom != 50 || s.PBTo != 200 || s.PBSpeed != "2" {
		t.Fatalf("playback %+v", s)
	}
	enc := s.Encode()
	s2 := viewstate.Parse(mustQuery(t, enc))
	if s2.AlertAirline != s.AlertAirline || len(s2.Mute) != len(s.Mute) {
		t.Fatalf("mute/airline round-trip\n got %+v\nwant %+v", s2, s)
	}
	if s2.Q != s.Q || s2.ICAO != s.ICAO || s2.Focus != s.Focus {
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

func TestParseMuteAndAlertAirline(t *testing.T) {
	s := viewstate.Parse(mustQuery(t, "mute=BB,aa,aa,&alert_airline=lo"))
	if len(s.Mute) != 2 || s.Mute[0] != "aa" || s.Mute[1] != "bb" {
		t.Fatalf("mute %#v", s.Mute)
	}
	if s.AlertAirline != "LO" {
		t.Fatal(s.AlertAirline)
	}
	enc := s.Encode()
	if !strings.Contains(enc, "mute=") {
		t.Fatal(enc)
	}
	if !strings.Contains(enc, "alert_airline=LO") {
		t.Fatal(enc)
	}
	s = viewstate.Parse(mustQuery(t, "mute="))
	if len(s.Mute) != 0 {
		t.Fatal(s.Mute)
	}
}

func TestParseMuteWhitespaceDedupAndStableEncode(t *testing.T) {
	s := viewstate.Parse(mustQuery(t, "mute=%20Aa%20%2C%2C%20BB%20%2Caa"))
	if len(s.Mute) != 2 || s.Mute[0] != "aa" || s.Mute[1] != "bb" {
		t.Fatalf("normalized mute %#v", s.Mute)
	}
	enc := s.Encode()
	s2 := viewstate.Parse(mustQuery(t, enc))
	if len(s2.Mute) != 2 || s2.Mute[0] != "aa" || s2.Mute[1] != "bb" {
		t.Fatalf("round-trip %#v via %q", s2.Mute, enc)
	}
	// Encode must stay sorted so shared mute URLs are stable.
	if !strings.Contains(enc, "mute=aa%2Cbb") && !strings.Contains(enc, "mute=aa,bb") {
		t.Fatalf("expected sorted mute in %q", enc)
	}
}

func TestParseIntItoaEdges(t *testing.T) {
	s := viewstate.Parse(mustQuery(t, "pb_at=12a&pb_from=&pb_to=0"))
	if s.PBAt != 0 || s.PBFrom != 0 || s.PBTo != 0 {
		t.Fatalf("%+v", s)
	}
	s.PBAt = 0
	s.PBFrom = 0
	s.PBTo = 0
	if enc := s.Encode(); enc != "" {
		t.Fatal(enc)
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
