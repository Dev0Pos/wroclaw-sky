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

const maxTrailPoints = 48

// trailGrace keeps breadcrumbs after an aircraft briefly leaves the bbox.
// Overridable in tests via TrailGraceForTest.
var trailGrace = 3 * time.Minute

// TrailGraceForTest sets trailGrace and returns the previous value.
func TrailGraceForTest(d time.Duration) time.Duration {
	prev := trailGrace
	trailGrace = d
	return prev
}

// Point is a trail breadcrumb (lat/lon + optional timestamp).
type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	At  int64   `json:"at,omitempty"` // unix seconds when recorded
}

type trailEntry struct {
	Points []Point
	SeenAt time.Time
}

// Store holds the latest aircraft snapshot for the UI.
// Refresh happens on demand (Refresh button) or via the shared Live poller
// started by POST /api/live (one OpenSky fetch for all Live clients).
//
// If UpstreamURL is set, Refresh() pulls from that fetcher (which talks to OpenSky)
// instead of calling OpenSky directly — useful when the UI runs on a cloud IP
// blocked by OpenSky (e.g. Render).
type Store struct {
	mu        sync.RWMutex
	aircraft  []opensky.Aircraft
	updatedAt time.Time
	err       error
	trails    map[string]*trailEntry // icao24 → recent positions
	client    *opensky.Client
	bbox      opensky.BBox
	breaker   *opensky.Breaker

	trailsFile string

	UpstreamURL   string
	UpstreamToken string
	HTTP          *http.Client
}

func New(client *opensky.Client, bbox opensky.BBox) *Store {
	if client == nil {
		client = &opensky.Client{}
	}
	return &Store{
		client:  client,
		bbox:    bbox,
		trails:  make(map[string]*trailEntry),
		breaker: opensky.NewBreaker(3, 60*time.Second),
		HTTP:    &http.Client{Timeout: 90 * time.Second},
	}
}

// BBox returns the configured OpenSky bounding box.
func (s *Store) BBox() opensky.BBox {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bbox
}

// SetBBox updates the query bounding box (runtime focus switch).
func (s *Store) SetBBox(bbox opensky.BBox) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bbox = bbox
}

// Stale reports whether the last refresh failed but a previous snapshot is kept.
func (s *Store) Stale() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err != nil && len(s.aircraft) > 0
}

// CircuitOpen reports whether the OpenSky breaker is open.
func (s *Store) CircuitOpen() bool {
	if s.breaker == nil {
		return false
	}
	return s.breaker.Open()
}

func (s *Store) Snapshot() ([]opensky.Aircraft, time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]opensky.Aircraft, len(s.aircraft))
	copy(out, s.aircraft)
	return out, s.updatedAt, s.err
}

// Trails returns a copy of recent position history keyed by ICAO24.
// Includes trails still within the grace window after leaving the snapshot.
func (s *Store) Trails() map[string][]Point {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]Point, len(s.trails))
	now := time.Now()
	for k, ent := range s.trails {
		if ent == nil || len(ent.Points) == 0 {
			continue
		}
		if now.Sub(ent.SeenAt) > trailGrace {
			continue
		}
		cp := make([]Point, len(ent.Points))
		copy(cp, ent.Points)
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
		s.trails = make(map[string]*trailEntry)
	}
	now := time.Now()
	seen := make(map[string]struct{}, len(list))
	for _, a := range list {
		if a.ICAO24 == "" || a.Lat == 0 && a.Lon == 0 {
			continue
		}
		seen[a.ICAO24] = struct{}{}
		ent := s.trails[a.ICAO24]
		if ent == nil {
			ent = &trailEntry{}
			s.trails[a.ICAO24] = ent
		}
		pts := ent.Points
		n := len(pts)
		if n > 0 {
			last := pts[n-1]
			// Skip near-duplicates (OpenSky noise / same tick).
			if abs(last.Lat-a.Lat) < 1e-5 && abs(last.Lon-a.Lon) < 1e-5 {
				ent.SeenAt = now
				continue
			}
		}
		pts = append(pts, Point{Lat: a.Lat, Lon: a.Lon, At: now.Unix()})
		if len(pts) > maxTrailPoints {
			pts = pts[len(pts)-maxTrailPoints:]
		}
		ent.Points = pts
		ent.SeenAt = now
	}
	for icao, ent := range s.trails {
		if _, ok := seen[icao]; ok {
			continue
		}
		// Keep briefly after leaving bbox; drop after trailGrace.
		if ent == nil || now.Sub(ent.SeenAt) > trailGrace {
			delete(s.trails, icao)
		}
	}
	s.persistTrailsLocked()
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
	if s.breaker != nil && !s.breaker.Allow() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.err = fmt.Errorf("opensky circuit open")
		slog.Warn("opensky circuit open", "duration_ms", time.Since(start).Milliseconds())
		return
	}
	s.mu.RLock()
	bbox := s.bbox
	s.mu.RUnlock()
	list, ts, err := s.client.FetchStates(bbox)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if s.breaker != nil {
			s.breaker.Failure()
		}
		s.err = err
		slog.Warn("opensky refresh failed", "err", err, "duration_ms", time.Since(start).Milliseconds())
		return
	}
	if s.breaker != nil {
		s.breaker.Success()
	}
	s.applyLocked(list, ts, nil)
	slog.Info("opensky refresh", "aircraft", len(list), "duration_ms", time.Since(start).Milliseconds())
}

type upstreamPayload struct {
	UpdatedAt string             `json:"updated_at"`
	Error     string             `json:"error"`
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
