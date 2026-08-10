package cache_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
)

func TestTrailsFilePersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trails.json")

	store := cache.New(nil, opensky.Wroclaw)
	if err := store.SetTrailsFile(path); err != nil {
		t.Fatal(err)
	}
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "A1", Lat: 51.1, Lon: 17.0},
	}, time.Now(), nil)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "aa", Callsign: "A1", Lat: 51.11, Lon: 17.01},
	}, time.Now(), nil)

	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	store2 := cache.New(nil, opensky.Wroclaw)
	if err := store2.SetTrailsFile(path); err != nil {
		t.Fatal(err)
	}
	trails := store2.Trails()
	if len(trails["aa"]) < 2 {
		t.Fatalf("loaded trails = %#v", trails)
	}
}

func TestLoadTrailsFileMissingAndExpired(t *testing.T) {
	got, err := cache.LoadTrailsFile(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %#v", err, got)
	}
	if err := cache.SaveTrailsFile("", nil); err != nil {
		t.Fatal(err)
	}
	prev := cache.TrailGraceForTest(time.Millisecond)
	t.Cleanup(func() { cache.TrailGraceForTest(prev) })

	path := filepath.Join(t.TempDir(), "old.json")
	store := cache.New(nil, opensky.Wroclaw)
	_ = store.SetTrailsFile(path)
	store.ApplySnapshot([]opensky.Aircraft{
		{ICAO24: "bb", Lat: 51.2, Lon: 17.1},
	}, time.Now(), nil)
	time.Sleep(5 * time.Millisecond)

	loaded, err := cache.LoadTrailsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected expired drop, got %#v", loaded)
	}

	store3 := cache.New(nil, opensky.Wroclaw)
	if err := store3.SetTrailsFile(""); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTrailsFileBadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.LoadTrailsFile(path); err == nil {
		t.Fatal("expected json error")
	}
}
