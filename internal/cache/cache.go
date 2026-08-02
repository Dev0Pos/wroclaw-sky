package cache

import (
	"log/slog"
	"sync"
	"time"

	"wroclaw-sky/internal/opensky"
)

// Store holds the latest OpenSky snapshot for the UI.
// Refresh only happens on demand (e.g. Refresh button) — no background polling.
type Store struct {
	mu        sync.RWMutex
	aircraft  []opensky.Aircraft
	updatedAt time.Time
	err       error
	client    *opensky.Client
	bbox      opensky.BBox
}

func New(client *opensky.Client, bbox opensky.BBox) *Store {
	if client == nil {
		client = &opensky.Client{}
	}
	return &Store{client: client, bbox: bbox}
}

func (s *Store) Snapshot() ([]opensky.Aircraft, time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]opensky.Aircraft, len(s.aircraft))
	copy(out, s.aircraft)
	return out, s.updatedAt, s.err
}

// Refresh pulls a fresh snapshot from OpenSky.
func (s *Store) Refresh() {
	start := time.Now()
	list, ts, err := s.client.FetchStates(s.bbox)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.err = err
		slog.Warn("opensky refresh failed", "err", err, "duration_ms", time.Since(start).Milliseconds())
		return
	}
	s.aircraft = list
	s.updatedAt = ts
	s.err = nil
	slog.Info("opensky refresh", "aircraft", len(list), "duration_ms", time.Since(start).Milliseconds())
}
