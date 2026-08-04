package opensky_test

import (
	"testing"

	"wroclaw-sky/internal/opensky"
)

func TestParseBBox(t *testing.T) {
	b, err := opensky.ParseBBox("50.90,16.70,51.30,17.40")
	if err != nil {
		t.Fatal(err)
	}
	if b != opensky.Wroclaw {
		t.Fatalf("got %+v want Wrocław", b)
	}
	lat, lon := b.Center()
	if lat < 51.0 || lat > 51.2 || lon < 17.0 || lon > 17.1 {
		t.Fatalf("center = %v,%v", lat, lon)
	}
	if !b.Contains(51.1, 17.0) || b.Contains(52.0, 17.0) {
		t.Fatalf("Contains broken")
	}
}

func TestParseBBoxErrors(t *testing.T) {
	cases := []string{"", "1,2,3", "a,b,c,d", "51,17,50,16", "91,0,92,1"}
	for _, c := range cases {
		if _, err := opensky.ParseBBox(c); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}
