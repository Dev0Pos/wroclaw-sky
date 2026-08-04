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

const maxTrailPoints = 24

// Point is a trail breadcrumb (lat/lon).
type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Store holds the latest aircraft snapshot for the UI.
// Refresh only happens on demand (e.g. Refresh button) — no background polling
// unless the browser Live toggle hits /refresh.
//
// If UpstreamURL is set, Refresh() pulls from that fetcher (which talks to OpenSky)
// instead of calling OpenSky directly — useful when the UI runs on a cloud IP
// blocked by OpenSky (e.g. Render).
type Store struct {
	mu        sync.RWMutex
	aircraft  []opensky.Aircraft
	updatedAt time.Time
	err       error
	trails    map[string][]Point // icao24 → recent positions
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
		trails: make(map[string][]Point),
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

// Trails returns a copy of recent position history keyed by ICAO24.
func (s *Store) Trails() map[string][]Point {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]Point, len(s.trails))
	for k, pts := range s.trails {
		cp := make([]Point, len(pts))
		copy(cp, pts)
		out[k] = cp
	}
	return out
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
	s.applyLocked(list, updatedAt, err)
}

func (s *Store) applyLocked(list []opensky.Aircraft, updatedAt time.Time, err error) {
	if err != nil {
		s.err = err
		return
	}
	s.aircraft = list
	s.updatedAt = updatedAt
	s.err = nil
	s.recordTrailsLocked(list)
}

func (s *Store) recordTrailsLocked(list []opensky.Aircraft) {
	if s.trails == nil {
		s.trails = make(map[string][]Point)
	}
	seen := make(map[string]struct{}, len(list))
	for _, a := range list {
		if a.ICAO24 == "" || a.Lat == 0 && a.Lon == 0 {
			continue
		}
		seen[a.ICAO24] = struct{}{}
		pts := s.trails[a.ICAO24]
		n := len(pts)
		if n > 0 {
			last := pts[n-1]
			// Skip near-duplicates (OpenSky noise / same tick).
			if abs(last.Lat-a.Lat) < 1e-5 && abs(last.Lon-a.Lon) < 1e-5 {
				continue
			}
		}
		pts = append(pts, Point{Lat: a.Lat, Lon: a.Lon})
		if len(pts) > maxTrailPoints {
			pts = pts[len(pts)-maxTrailPoints:]
		}
		s.trails[a.ICAO24] = pts
	}
	for icao := range s.trails {
		if _, ok := seen[icao]; !ok {
			delete(s.trails, icao)
		}
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
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
	s.applyLocked(list, ts, nil)
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
	ts := time.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339, payload.UpdatedAt); err == nil {
		ts = parsed
	}
	s.applyLocked(payload.Aircraft, ts, nil)
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
