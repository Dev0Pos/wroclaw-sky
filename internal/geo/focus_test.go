package geo

import "testing"

func TestParseFocus(t *testing.T) {
	f, err := ParseFocus("")
	if err != nil || f.ICAO != "EPWR" {
		t.Fatalf("default: %+v %v", f, err)
	}
	f, err = ParseFocus("epwa")
	if err != nil || f.ICAO != "EPWA" || f.Lat == 0 {
		t.Fatalf("EPWA: %+v %v", f, err)
	}
	if _, err := ParseFocus("ZZZZ"); err == nil {
		t.Fatal("expected unknown")
	}
	if DefaultFocus().Label() != "EPWR · Wrocław" {
		t.Fatalf("label = %q", DefaultFocus().Label())
	}
	codes := KnownFocusICAOs()
	if len(codes) < 5 || codes[0] > codes[len(codes)-1] {
		t.Fatalf("codes = %#v", codes)
	}
}

func TestOnApproachTo(t *testing.T) {
	f, _ := ParseFocus("EPWA")
	if !OnApproachTo(f, "EPWA", f.Lat+0.05, f.Lon, false) {
		t.Fatal("expected approach")
	}
	if OnApproachTo(f, "EPWR", f.Lat, f.Lon, false) {
		t.Fatal("wrong dest")
	}
	hint := FormatFocusHint(f, "EPWA", f.Lat+0.05, f.Lon, 100, false)
	if hint == "" {
		t.Fatal("expected hint")
	}
	if FormatFocusHint(f, "EPWR", f.Lat, f.Lon, 100, false) != "" {
		t.Fatal("wrong dest hint")
	}
}
