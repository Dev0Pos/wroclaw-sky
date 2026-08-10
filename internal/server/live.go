package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Overridable in tests via LiveTimingForTest.
var (
	liveInterval = 45 * time.Second
	liveLease    = 90 * time.Second
)

// LiveTimingForTest sets live interval/lease and returns previous values.
func LiveTimingForTest(interval, lease time.Duration) (prevInterval, prevLease time.Duration) {
	prevInterval, prevLease = liveInterval, liveLease
	liveInterval, liveLease = interval, lease
	return prevInterval, prevLease
}

// live state: shared OpenSky poller driven by client heartbeats (POST /api/live).
// Multiple browsers can enable Live — one poller serves them all until the lease expires.
type liveState struct {
	mu     sync.Mutex
	until  time.Time
	cancel context.CancelFunc
}

func (s *Server) refreshAndWarm() {
	s.refreshTotal.Add(1)
	s.store.Refresh()
	_, _, err := s.store.Snapshot()
	if err != nil {
		s.refreshErrors.Add(1)
	} else {
		s.lastRefresh.Store(time.Now().Unix())
	}
	s.warmRoutes()
	s.publishUpdate()
}

// touchLive extends the live lease and starts the shared poller if needed.
// When starting, performs one synchronous refresh so the caller gets fresh data.
func (s *Server) touchLive() (active bool, until time.Time) {
	s.live.mu.Lock()
	s.live.until = time.Now().Add(liveLease)
	until = s.live.until
	start := s.live.cancel == nil
	if start {
		ctx, cancel := context.WithCancel(context.Background())
		s.live.cancel = cancel
		s.live.mu.Unlock()
		s.refreshAndWarm()
		go s.liveLoop(ctx)
		return true, until
	}
	s.live.mu.Unlock()
	return true, until
}

func (s *Server) liveLoop(ctx context.Context) {
	slog.Info("live poller started", "interval_s", int(liveInterval.Seconds()))
	ticker := time.NewTicker(liveInterval)
	defer ticker.Stop()
	defer slog.Info("live poller stopped")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.live.mu.Lock()
			until := s.live.until
			expired := time.Now().After(until)
			var cancel context.CancelFunc
			if expired {
				cancel = s.live.cancel
				s.live.cancel = nil
				s.live.until = time.Time{}
			}
			s.live.mu.Unlock()
			if expired {
				if cancel != nil {
					cancel()
				}
				return
			}
			s.refreshAndWarm()
		}
	}
}

func (s *Server) liveStatus() (active bool, until time.Time) {
	s.live.mu.Lock()
	defer s.live.mu.Unlock()
	if s.live.cancel == nil {
		return false, time.Time{}
	}
	if time.Now().After(s.live.until) {
		return false, s.live.until
	}
	return true, s.live.until
}

// handleLive manages the shared Live poller.
// POST/GET — heartbeat / start (extends 90s lease).
// DELETE — ignored for multi-client safety; lease expires when heartbeats stop.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost, http.MethodGet:
		active, until := s.touchLive()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"live":       active,
			"interval_s": int(liveInterval.Seconds()),
			"until":      until.UTC().Format(time.RFC3339),
		})
	case http.MethodDelete:
		// Do not stop the shared poller — other tabs may still be live.
		// Clients simply stop heartbeating; the lease expires on its own.
		active, until := s.liveStatus()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"live":  active,
			"until": untilUTC(until),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func untilUTC(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
