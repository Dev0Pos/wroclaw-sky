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
	if (Focus{ICAO: "X"}).Label() != "X" {
		t.Fatal("icao-only label")
	}
	f2, ok := LookupFocus("")
	if !ok || f2.ICAO != "EPWR" {
		t.Fatal("lookup empty")
	}
	if _, ok := LookupFocus("EPWA"); !ok {
		t.Fatal("lookup EPWA")
	}
	if _, ok := LookupFocus("NOPE"); ok {
		t.Fatal("lookup miss")
	}
	codes := KnownFocusICAOs()
	if len(codes) < 5 || codes[0] > codes[len(codes)-1] {
		t.Fatalf("codes = %#v", codes)
	}
	presets := PolishPresets()
	if len(presets) < 3 || presets[0] != "EPWR" {
		t.Fatalf("presets %#v", presets)
	}
	if _, ok := LookupFocus("EPLL"); !ok {
		t.Fatal("EPLL")
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

func TestResolveFocus(t *testing.T) {
	f, err := ResolveFocus("EPWA", "", "", "")
	if err != nil || f.ICAO != "EPWA" {
		t.Fatalf("%+v %v", f, err)
	}
	f, err = ResolveFocus("EPWA", "52.2", "21.0", "Custom City")
	if err != nil || f.Lat != 52.2 || f.Lon != 21.0 || f.City != "Custom City" {
		t.Fatalf("override %+v %v", f, err)
	}
	f, err = ResolveFocus("XXXX", "51.5", "17.0", "Test")
	if err != nil || f.ICAO != "XXXX" {
		t.Fatalf("custom %+v %v", f, err)
	}
	if _, err := ResolveFocus("XXXX", "", "", ""); err == nil {
		t.Fatal("unknown without coords")
	}
	if _, err := ResolveFocus("EPWR", "bad", "", ""); err == nil {
		t.Fatal("bad lat")
	}
	if _, err := ResolveFocus("EPWR", "51", "bad", ""); err == nil {
		t.Fatal("bad lon")
	}
	if _, err := ResolveFocus("ZZ", "91", "0", ""); err == nil {
		t.Fatal("lat range")
	}
	if _, err := ResolveFocus("ZZ", "0", "200", ""); err == nil {
		t.Fatal("lon range")
	}
	// Force zero coords after override
	if _, err := ResolveFocus("XXXX", "0", "0", "Z"); err == nil {
		t.Fatal("zero coords")
	}
	fEmptyCity := Focus{ICAO: "AB", Lat: 1, Lon: 2}
	if fEmptyCity.Label() != "AB" {
		t.Fatal(fEmptyCity.Label())
	}
	// empty icao branch in ResolveFocus uses DefaultFocus
	f, err = ResolveFocus("", "", "", "")
	if err != nil || f.ICAO != "EPWR" {
		t.Fatalf("empty resolve %+v %v", f, err)
	}
	if FormatFocusHint(DefaultFocus(), "EPWR", 0, 0, 100, false) != "" {
		t.Fatal("zero pos hint")
	}
}
