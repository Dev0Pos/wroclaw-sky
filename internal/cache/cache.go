package cache

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"wroclaw-sky/internal/opensky"
)

// Store holds the latest aircraft snapshot for the UI.
// Refresh only happens on demand (e.g. Refresh button) — no background polling.
//
// If UpstreamURL is set, Refresh() pulls from that fetcher (which talks to OpenSky)
// instead of calling OpenSky directly — useful when the UI runs on a cloud IP
// blocked by OpenSky (e.g. Render).
type Store struct {
	mu        sync.RWMutex
	aircraft  []opensky.Aircraft
	updatedAt time.Time
	err       error
	client    *opensky.Client
	bbox      opensky.BBox

	UpstreamURL   string
	UpstreamToken string
	HTTP          *http.Client
}

func New(client *opensky.Client, bbox opensky.BBox) *Store {
	if client == nil {
		client = &opensky.Client{}
	}
	return &Store{
		client: client,
		bbox:   bbox,
		HTTP:   &http.Client{Timeout: 90 * time.Second},
	}
}

func (s *Store) Snapshot() ([]opensky.Aircraft, time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]opensky.Aircraft, len(s.aircraft))
	copy(out, s.aircraft)
	return out, s.updatedAt, s.err
}

// Find returns one aircraft from the latest snapshot by ICAO24.
func (s *Store) Find(icao24 string) (opensky.Aircraft, bool) {
	icao24 = strings.ToLower(strings.TrimSpace(icao24))
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.aircraft {
		if a.ICAO24 == icao24 {
			return a, true
		}
	}
	return opensky.Aircraft{}, false
}

// ApplySnapshot replaces the in-memory snapshot (used by /api/fetch and tests).
func (s *Store) ApplySnapshot(list []opensky.Aircraft, updatedAt time.Time, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.err = err
		return
	}
	s.aircraft = list
	s.updatedAt = updatedAt
	s.err = nil
}

// Refresh pulls a fresh snapshot from OpenSky or from UpstreamURL.
func (s *Store) Refresh() {
	start := time.Now()
	if strings.TrimSpace(s.UpstreamURL) != "" {
		s.refreshUpstream(start)
		return
	}
	s.refreshOpenSky(start)
}

// RefreshOpenSky always queries OpenSky (used by /api/fetch on the fetcher host).
func (s *Store) RefreshOpenSky() {
	s.refreshOpenSky(time.Now())
}

func (s *Store) refreshOpenSky(start time.Time) {
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

type upstreamPayload struct {
	UpdatedAt string            `json:"updated_at"`
	Error     string            `json:"error"`
	Aircraft  []opensky.Aircraft `json:"aircraft"`
}

func (s *Store) refreshUpstream(start time.Time) {
	base := strings.TrimRight(s.UpstreamURL, "/")
	url := base + "/api/fetch"
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		s.fail(err, start, "upstream")
		return
	}
	if s.UpstreamToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.UpstreamToken)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "wroclaw-sky-ui")

	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		s.fail(err, start, "upstream")
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		s.fail(fmt.Errorf("upstream returned %s: %s", resp.Status, truncate(string(body), 200)), start, "upstream")
		return
	}
	var payload upstreamPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		s.fail(err, start, "upstream")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if payload.Error != "" {
		s.err = fmt.Errorf("%s", payload.Error)
		slog.Warn("upstream refresh error", "err", payload.Error, "duration_ms", time.Since(start).Milliseconds())
		return
	}
	s.aircraft = payload.Aircraft
	if ts, err := time.Parse(time.RFC3339, payload.UpdatedAt); err == nil {
		s.updatedAt = ts
	} else {
		s.updatedAt = time.Now().UTC()
	}
	s.err = nil
	slog.Info("upstream refresh", "aircraft", len(s.aircraft), "duration_ms", time.Since(start).Milliseconds())
}

func (s *Store) fail(err error, start time.Time, kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
	slog.Warn(kind+" refresh failed", "err", err, "duration_ms", time.Since(start).Milliseconds())
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
