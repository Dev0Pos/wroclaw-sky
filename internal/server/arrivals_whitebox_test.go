package server

import (
	"testing"

	"wroclaw-sky/internal/opensky"
)

func TestBuildArrivalsSortAndFilter(t *testing.T) {
	rows := []flightRow{
		{Aircraft: opensky.Aircraft{ICAO24: "a", Callsign: "LOT9", Lat: 51.15, Lon: 16.95, Velocity: 80}, Destination: "EPWR"},
		{Aircraft: opensky.Aircraft{ICAO24: "b", Callsign: "RYR1", Lat: 51.5, Lon: 17.5, Velocity: 200}, Destination: "EPWR"},
		{Aircraft: opensky.Aircraft{ICAO24: "c", Callsign: "WZZ1", Lat: 51.2, Lon: 17.0, Velocity: 100}, Destination: "EPWA"},
		{Aircraft: opensky.Aircraft{ICAO24: "d", Callsign: "LOT0", Lat: 51.1, Lon: 16.9, Velocity: 0, OnGround: true}, Destination: "EPWR"},
		{Aircraft: opensky.Aircraft{ICAO24: "e", Callsign: "EZY1", Lat: 0, Lon: 0, Velocity: 100}, Destination: "EPWR"},
		// ETA unknown (slow): sorted by distance, then callsign when equal.
		{Aircraft: opensky.Aircraft{ICAO24: "f", Callsign: "BBB1", Lat: 51.25, Lon: 17.05, Velocity: 1}, Destination: "EPWR"},
		{Aircraft: opensky.Aircraft{ICAO24: "g", Callsign: "AAA1", Lat: 51.25, Lon: 17.05, Velocity: 1}, Destination: "EPWR"},
		{Aircraft: opensky.Aircraft{ICAO24: "h", Callsign: "CCC1", Lat: 51.40, Lon: 17.20, Velocity: 1}, Destination: "EPWR"},
	}
	got := buildArrivals(rows)
	if len(got) != 5 {
		t.Fatalf("len = %d %#v", len(got), got)
	}
	if got[0].Callsign != "LOT9" || got[1].Callsign != "RYR1" {
		t.Fatalf("eta order = %v %v", got[0].Callsign, got[1].Callsign)
	}
	// Unknown ETA last: nearer AAA1/BBB1 before CCC1; AAA before BBB.
	if got[2].Callsign != "AAA1" || got[3].Callsign != "BBB1" || got[4].Callsign != "CCC1" {
		t.Fatalf("zero-eta order = %v %v %v", got[2].Callsign, got[3].Callsign, got[4].Callsign)
	}
	if got[0].Hint == "" || !got[0].Approach {
		t.Fatalf("near approach: %+v", got[0])
	}
	if got[1].Approach {
		t.Fatalf("far should not be approach: %+v", got[1])
	}
}
