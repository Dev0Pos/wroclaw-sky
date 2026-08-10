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

	one := []cache.Point{{Lat: 1, Lon: 2, At: 10}}
	lat, lon, ok = cache.PositionAt(one, 10)
	if !ok || lat != 1 || lon != 2 {
		t.Fatalf("single equal %v %v %v", lat, lon, ok)
	}
	lat, lon, ok = cache.PositionAt(one, 99)
	if !ok || lat != 1 || lon != 2 {
		t.Fatalf("single after %v %v %v", lat, lon, ok)
	}

	lat, lon, ok = cache.PositionAt(pts, 100)
	if !ok || lat != 51.0 || lon != 17.0 {
		t.Fatalf("at first %v %v %v", lat, lon, ok)
	}

	// continue past first segment into the second
	three := []cache.Point{
		{Lat: 0, Lon: 0, At: 100},
		{Lat: 1, Lon: 1, At: 200},
		{Lat: 3, Lon: 3, At: 400},
	}
	lat, lon, ok = cache.PositionAt(three, 300)
	if !ok || lat < 1.9 || lat > 2.1 {
		t.Fatalf("second segment mid = %v %v ok=%v", lat, lon, ok)
	}

	// duplicate trailing stamps: seek past them hits final return
	dup := []cache.Point{
		{Lat: 0, Lon: 0, At: 100},
		{Lat: 1, Lon: 1, At: 200},
		{Lat: 1, Lon: 1, At: 200},
	}
	lat, lon, ok = cache.PositionAt(dup, 201)
	if !ok || lat != 1 || lon != 1 {
		t.Fatalf("past dup = %v %v ok=%v", lat, lon, ok)
	}
}

func TestTrailTimeRange(t *testing.T) {
	min, max := cache.TrailTimeRange(map[string][]cache.Point{
		"a": {{At: 100}, {At: 300}},
		"b": {{At: 200}, {At: 0}},
		"c": {{At: 50}},
	})
	if min != 50 || max != 300 {
		t.Fatalf("%d %d", min, max)
	}
	min, max = cache.TrailTimeRange(nil)
	if min != 0 || max != 0 {
		t.Fatalf("empty %d %d", min, max)
	}
	min, max = cache.TrailTimeRange(map[string][]cache.Point{
		"z": {{At: 0}, {At: 0}},
	})
	if min != 0 || max != 0 {
		t.Fatalf("all zero %d %d", min, max)
	}
}
