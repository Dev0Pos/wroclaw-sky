package cache_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
)

func TestTrailsSQLiteRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	store := cache.New(nil, opensky.Wroclaw)
	if err := store.SetTrailsDB(path); err != nil {
		t.Fatal(err)
	}
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Lat: 51.1, Lon: 17.0},
	}, time.Now(), nil)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Lat: 51.11, Lon: 17.01},
	}, time.Now(), nil)

	store2 := cache.New(nil, opensky.Wroclaw)
	if err := store2.SetTrailsDB(path); err != nil {
		t.Fatal(err)
	}
	trails := store2.Trails()
	if len(trails["aa"]) < 2 {
		t.Fatalf("%#v", trails)
	}
	if err := store2.SetTrailsDB(""); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.OpenTrailsDB(""); err == nil {
		t.Fatal("empty path")
	}
}

func TestTrailsRedisRoundTrip(t *testing.T) {
	mr := miniredis.RunT(t)
	r, err := cache.NewRedisTrailsFromURL("redis://" + mr.Addr())
	if err != nil {
		t.Fatal(err)
	}
	store := cache.New(nil, opensky.Wroclaw)
	if err := store.SetTrailsRedis(r); err != nil {
		t.Fatal(err)
	}
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "bb", Lat: 51.2, Lon: 17.1},
	}, time.Now(), nil)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "bb", Lat: 51.21, Lon: 17.11},
	}, time.Now(), nil)

	store2 := cache.New(nil, opensky.Wroclaw)
	r2 := &cache.RedisTrails{Client: redis.NewClient(&redis.Options{Addr: mr.Addr()})}
	if err := store2.SetTrailsRedis(r2); err != nil {
		t.Fatal(err)
	}
	if len(store2.Trails()["bb"]) < 2 {
		t.Fatal(store2.Trails())
	}
	if err := store2.SetTrailsRedis(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.NewRedisTrailsFromURL(""); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.NewRedisTrailsFromURL("://bad"); err == nil {
		t.Fatal("bad url")
	}
	if _, err := cache.NewRedisTrailsFromURL("redis://127.0.0.1:1"); err == nil {
		t.Fatal("ping fail")
	}
	var nilR *cache.RedisTrails
	if _, err := nilR.Get(t.Context(), "k"); err != nil {
		t.Fatal(err)
	}
	if err := nilR.Set(t.Context(), "k", "v", time.Second); err != nil {
		t.Fatal(err)
	}
}
