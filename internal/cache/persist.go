package cache

import (
	"encoding/json"
	"os"
	"time"
)

type trailFileEntry struct {
	Points []Point   `json:"points"`
	SeenAt time.Time `json:"seen_at"`
}

// LoadTrailsFile reads persisted trails from path (JSON). Expired entries are dropped.
func LoadTrailsFile(path string) (map[string]*trailEntry, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]*trailEntry{}, nil
		}
		return nil, err
	}
	var data map[string]trailFileEntry
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	now := time.Now()
	out := make(map[string]*trailEntry, len(data))
	for k, v := range data {
		if len(v.Points) == 0 {
			continue
		}
		if now.Sub(v.SeenAt) > trailGrace {
			continue
		}
		pts := append([]Point(nil), v.Points...)
		if len(pts) > maxTrailPoints {
			pts = pts[len(pts)-maxTrailPoints:]
		}
		out[k] = &trailEntry{Points: pts, SeenAt: v.SeenAt}
	}
	return out, nil
}

// SaveTrailsFile writes trails to path as JSON.
func SaveTrailsFile(path string, trails map[string]*trailEntry) error {
	if path == "" {
		return nil
	}
	data := make(map[string]trailFileEntry, len(trails))
	now := time.Now()
	for k, ent := range trails {
		if ent == nil || len(ent.Points) == 0 {
			continue
		}
		if now.Sub(ent.SeenAt) > trailGrace {
			continue
		}
		data[k] = trailFileEntry{Points: ent.Points, SeenAt: ent.SeenAt}
	}
	raw, err := marshalJSON(data)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := writeFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Overridable in tests.
var (
	writeFile   = os.WriteFile
	marshalJSON = json.Marshal
)

// SetTrailsFile enables JSON persistence of session trails at path.
func (s *Store) SetTrailsFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trailsFile = path
	if path == "" {
		return nil
	}
	loaded, err := LoadTrailsFile(path)
	if err != nil {
		return err
	}
	if s.trails == nil {
		s.trails = make(map[string]*trailEntry)
	}
	for k, v := range loaded {
		s.trails[k] = v
	}
	return nil
}

func (s *Store) persistTrailsLocked() {
	if s.trailsFile == "" {
		return
	}
	// Copy under lock, write outside would need unlock — keep sync write for simplicity/tests.
	_ = SaveTrailsFile(s.trailsFile, s.trails)
}
