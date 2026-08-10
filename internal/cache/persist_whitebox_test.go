package cache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"wroclaw-sky/internal/opensky"
)

func TestLoadSaveTrailsBranches(t *testing.T) {
	if got, err := LoadTrailsFile(""); err != nil || got != nil {
		t.Fatalf("%v %#v", err, got)
	}
	path := filepath.Join(t.TempDir(), "t.json")
	now := time.Now()
	in := map[string]*trailEntry{
		"nil":   nil,
		"empty": {Points: nil, SeenAt: now},
		"ok":    {Points: []Point{{Lat: 1, Lon: 2, At: 1}}, SeenAt: now},
		"old":   {Points: []Point{{Lat: 1, Lon: 2, At: 1}}, SeenAt: now.Add(-time.Hour)},
	}
	if err := SaveTrailsFile(path, in); err != nil {
		t.Fatal(err)
	}
	long := make([]Point, 60)
	for i := range long {
		long[i] = Point{Lat: float64(i), Lon: 1, At: int64(i)}
	}
	rawPath := filepath.Join(t.TempDir(), "long.json")
	if err := SaveTrailsFile(rawPath, map[string]*trailEntry{
		"empty": {Points: []Point{}, SeenAt: now},
		"long":  {Points: long, SeenAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrailsFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded["empty"]; ok {
		t.Fatal("empty")
	}
	if len(loaded["long"].Points) != maxTrailPoints {
		t.Fatalf("trim = %d", len(loaded["long"].Points))
	}
	if err := SaveTrailsFile(t.TempDir(), map[string]*trailEntry{"a": {Points: []Point{{1, 2, 3}}, SeenAt: now}}); err == nil {
		t.Fatal("expected write error")
	}
	prevW := writeFile
	t.Cleanup(func() { writeFile = prevW })
	writeFile = func(string, []byte, os.FileMode) error { return errors.New("write fail") }
	if err := SaveTrailsFile(filepath.Join(t.TempDir(), "x.json"), map[string]*trailEntry{"a": {Points: []Point{{1, 2, 3}}, SeenAt: now}}); err == nil {
		t.Fatal("injected write fail")
	}
	writeFile = prevW

	prevM := marshalJSON
	t.Cleanup(func() { marshalJSON = prevM })
	marshalJSON = func(any) ([]byte, error) { return nil, errors.New("marshal") }
	if err := SaveTrailsFile(filepath.Join(t.TempDir(), "m.json"), map[string]*trailEntry{"a": {Points: []Point{{1, 2, 3}}, SeenAt: now}}); err == nil {
		t.Fatal("marshal fail")
	}
	marshalJSON = prevM

	// empty points skipped on load
	emptyPath := filepath.Join(t.TempDir(), "e.json")
	_ = os.WriteFile(emptyPath, []byte(`{"e":{"points":[],"seen_at":"2099-01-01T00:00:00Z"}}`), 0o644)
	loadedEmpty, err := LoadTrailsFile(emptyPath)
	if err != nil || len(loadedEmpty) != 0 {
		t.Fatalf("empty points: %v %#v", err, loadedEmpty)
	}

	// ReadFile error that is not IsNotExist
	dirAsFile := t.TempDir()
	if _, err := LoadTrailsFile(dirAsFile); err == nil {
		t.Fatal("expected read dir error")
	}

	s := New(nil, opensky.Wroclaw)
	if err := s.SetTrailsFile(""); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(bad, []byte(`{`), 0o644)
	if err := s.SetTrailsFile(bad); err == nil {
		t.Fatal("expected bad json on set")
	}
	s2 := &Store{}
	okPath := filepath.Join(t.TempDir(), "ok.json")
	_ = SaveTrailsFile(okPath, map[string]*trailEntry{"x": {Points: []Point{{1, 2, 3}}, SeenAt: now}})
	if err := s2.SetTrailsFile(okPath); err != nil {
		t.Fatal(err)
	}
	if s2.trails["x"] == nil {
		t.Fatal("expected load into nil map")
	}
}
