package opensky_test

import (
	"testing"

	"wroclaw-sky/internal/opensky"
)

func TestBBoxAround(t *testing.T) {
	b := opensky.BBoxAround(52.16, 20.96, 80)
	if !b.Contains(52.16, 20.96) {
		t.Fatalf("center not inside %+v", b)
	}
	if b.LaMin >= b.LaMax || b.LoMin >= b.LoMax {
		t.Fatalf("invalid %+v", b)
	}
	b0 := opensky.BBoxAround(0, 0, 0) // default radius
	if !b0.Contains(0, 0) {
		t.Fatal("default radius")
	}
	bp := opensky.BBoxAround(89, 0, 5000)
	if bp.LaMax > 90 || bp.LaMin < -90 {
		t.Fatalf("pole clamp %+v", bp)
	}
	if bp.LaMax != 90 {
		t.Fatalf("expected LaMax clamped to 90, got %v", bp.LaMax)
	}
	// Force lon clamps via huge radius near antimeridian
	bw := opensky.BBoxAround(0, 179, 5000)
	if bw.LoMax > 180 || bw.LoMin < -180 {
		t.Fatalf("lon clamp %+v", bw)
	}
	bs := opensky.BBoxAround(-89, -179, 5000)
	if bs.LaMin < -90 || bs.LoMin < -180 {
		t.Fatalf("south clamp %+v", bs)
	}
}
