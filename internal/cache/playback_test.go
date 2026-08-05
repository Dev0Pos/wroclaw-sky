package cache_test

import (
	"testing"

	"wroclaw-sky/internal/cache"
)

func TestPositionAt(t *testing.T) {
	pts := []cache.Point{
		{Lat: 51.0, Lon: 17.0, At: 100},
		{Lat: 51.2, Lon: 17.2, At: 200},
	}
	lat, lon, ok := cache.PositionAt(pts, 150)
	if !ok || lat < 51.09 || lat > 51.11 || lon < 17.09 || lon > 17.11 {
		t.Fatalf("mid = %v %v ok=%v", lat, lon, ok)
	}
	_, _, ok = cache.PositionAt(pts, 50)
	if ok {
		t.Fatal("before start")
	}
	lat, lon, ok = cache.PositionAt(pts, 250)
	if !ok || lat != 51.2 || lon != 17.2 {
		t.Fatalf("after end = %v %v", lat, lon)
	}
	_, _, ok = cache.PositionAt(nil, 1)
	if ok {
		t.Fatal("empty")
	}
}

func TestTrailTimeRange(t *testing.T) {
	min, max := cache.TrailTimeRange(map[string][]cache.Point{
		"a": {{At: 100}, {At: 300}},
		"b": {{At: 200}, {At: 0}},
	})
	if min != 100 || max != 300 {
		t.Fatalf("%d %d", min, max)
	}
	min, max = cache.TrailTimeRange(nil)
	if min != 0 || max != 0 {
		t.Fatalf("empty %d %d", min, max)
	}
}
