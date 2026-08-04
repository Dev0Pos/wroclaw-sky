package meta_test

import (
	"testing"

	"wroclaw-sky/internal/meta"
)

func TestAirlineFromCallsign(t *testing.T) {
	icao, name := meta.AirlineFromCallsign("LOT381")
	if icao != "LOT" || name != "LOT" {
		t.Fatalf("LOT381 → %q %q", icao, name)
	}
	icao, name = meta.AirlineFromCallsign("RYR12AB")
	if icao != "RYR" || name != "Ryanair" {
		t.Fatalf("RYR → %q %q", icao, name)
	}
	if h := meta.AirlineHint("WZZ1A"); h != "Wizz Air" {
		t.Fatalf("hint = %q", h)
	}
	if icao, name := meta.AirlineFromCallsign("ABCDEF"); icao != "" || name != "" {
		t.Fatalf("hex-like should be empty, got %q %q", icao, name)
	}
	if icao, name := meta.AirlineFromCallsign("GA123"); icao != "" {
		t.Fatalf("short/non-icao prefix: %q %q", icao, name)
	}
	if icao, name := meta.AirlineFromCallsign("AB"); icao != "" || name != "" {
		t.Fatalf("too short: %q %q", icao, name)
	}
	if icao, name := meta.AirlineFromCallsign("LOT"); icao != "" {
		t.Fatalf("no flight number: %q %q", icao, name)
	}
	if icao, name := meta.AirlineFromCallsign("LOTX"); icao != "" {
		t.Fatalf("no digit in rest: %q %q", icao, name)
	}
	// Unknown but valid pattern → ICAO prefix as name.
	icao, name = meta.AirlineFromCallsign("XYZ1234")
	if icao != "XYZ" || name != "XYZ" {
		t.Fatalf("unknown = %q %q", icao, name)
	}
}
