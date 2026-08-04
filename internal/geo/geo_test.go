package geo_test

import (
	"math"
	"testing"

	"wroclaw-sky/internal/geo"
)

func TestHaversineEPWRLocal(t *testing.T) {
	// ~11 km north of EPWR
	d := geo.HaversineM(geo.EPWRLat+0.1, geo.EPWRLon, geo.EPWRLat, geo.EPWRLon)
	if d < 10000 || d > 12000 {
		t.Fatalf("dist = %.0f m, want ~11 km", d)
	}
}

func TestDestinationPointRoundTrip(t *testing.T) {
	lat2, lon2 := geo.DestinationPoint(geo.EPWRLat, geo.EPWRLon, 90, 1000)
	d := geo.HaversineM(geo.EPWRLat, geo.EPWRLon, lat2, lon2)
	if math.Abs(d-1000) > 5 {
		t.Fatalf("dest dist = %.1f, want ~1000", d)
	}
}

func TestETAAndFormat(t *testing.T) {
	if geo.ETASeconds(10000, 100) != 100 {
		t.Fatalf("eta seconds")
	}
	if geo.ETASeconds(0, 100) != 0 || geo.ETASeconds(100, 1) != 0 {
		t.Fatal("expected zero ETA for invalid inputs")
	}
	if got := geo.FormatDistKm(12500); got != "13 km" {
		t.Fatalf("FormatDistKm(12500) = %q", got)
	}
	if got := geo.FormatDistKm(800); got != "0.8 km" {
		t.Fatalf("FormatDistKm(800) = %q", got)
	}
	if got := geo.FormatDistKm(0); got != "—" {
		t.Fatalf("FormatDistKm(0) = %q", got)
	}
	if got := geo.FormatETA(0); got != "" {
		t.Fatalf("FormatETA(0) = %q", got)
	}
	if got := geo.FormatETA(45); got != "~45s" {
		t.Fatalf("FormatETA(45) = %q", got)
	}
	if got := geo.FormatETA(185); got != "~3m" {
		t.Fatalf("FormatETA(185) = %q", got)
	}
	if got := geo.FormatETA(3600); got != "~1h" {
		t.Fatalf("FormatETA(3600) = %q", got)
	}
	if got := geo.FormatETA(3720); got != "~1h 2m" {
		t.Fatalf("FormatETA(3720) = %q", got)
	}
}

func TestOnApproach(t *testing.T) {
	// ~11 km north of EPWR, inbound
	if !geo.OnApproach("EPWR", geo.EPWRLat+0.1, geo.EPWRLon, false) {
		t.Fatal("expected on approach")
	}
	// Far away
	if geo.OnApproach("EPWR", 52.5, 17.0, false) {
		t.Fatal("too far")
	}
	if geo.OnApproach("EPWA", geo.EPWRLat, geo.EPWRLon, false) {
		t.Fatal("wrong dest")
	}
	if geo.OnApproach("EPWR", geo.EPWRLat, geo.EPWRLon, true) {
		t.Fatal("on ground")
	}
	if geo.OnApproach("EPWR", 0, 0, false) {
		t.Fatal("zero coords")
	}
}
