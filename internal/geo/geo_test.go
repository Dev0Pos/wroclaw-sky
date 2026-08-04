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
	if got := geo.FormatDistKm(12500); got != "13 km" {
		t.Fatalf("FormatDistKm(12500) = %q", got)
	}
	if got := geo.FormatDistKm(800); got != "0.8 km" {
		t.Fatalf("FormatDistKm(800) = %q", got)
	}
	if got := geo.FormatETA(185); got != "~3m" {
		t.Fatalf("FormatETA(185) = %q", got)
	}
	if got := geo.FormatETA(3720); got != "~1h 2m" {
		t.Fatalf("FormatETA(3720) = %q", got)
	}
}
