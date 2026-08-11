package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/opensky"
)

// Alert kinds pushed to webhook / SSE.
const (
	AlertApproach = "approach"
	AlertLowPass  = "low_pass"
)

// AlertEvent is a single approach / low-pass notification.
type AlertEvent struct {
	Type      string  `json:"type"`
	ICAO24    string  `json:"icao24"`
	Callsign  string  `json:"callsign,omitempty"`
	Origin    string  `json:"origin,omitempty"`
	Dest      string  `json:"destination,omitempty"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	AltitudeM float64 `json:"altitude_m,omitempty"`
	Focus     string  `json:"focus"`
	At        string  `json:"at"`
}

type alertState struct {
	mu           sync.Mutex
	approach     map[string]bool
	lowPass      map[string]bool
	bootstrapped bool
}

func (s *Server) SetAlertWebhook(url string) {
	s.alertWebhook = strings.TrimSpace(url)
}

func (s *Server) SetApproachRadiusM(m float64) {
	if m > 0 {
		s.approachRadiusM = m
	}
}

func (s *Server) SetLowPassAltM(m float64) {
	if m >= 0 {
		s.lowPassAltM = m
	}
}

func (s *Server) onApproach(a opensky.Aircraft, dest string) bool {
	radius := s.approachRadiusM
	if radius <= 0 {
		radius = geo.ApproachRadiusM
	}
	if a.OnGround || !strings.EqualFold(strings.TrimSpace(dest), s.focus.ICAO) {
		return false
	}
	if a.Lat == 0 && a.Lon == 0 {
		return false
	}
	return geo.HaversineM(a.Lat, a.Lon, s.focus.Lat, s.focus.Lon) <= radius
}

func (s *Server) isLowPass(a opensky.Aircraft) bool {
	if s.lowPassAltM <= 0 || a.OnGround {
		return false
	}
	if a.AltitudeM <= 0 || a.AltitudeM > s.lowPassAltM {
		return false
	}
	if a.Lat == 0 && a.Lon == 0 {
		return false
	}
	radius := s.approachRadiusM
	if radius <= 0 {
		radius = geo.ApproachRadiusM
	}
	return geo.HaversineM(a.Lat, a.Lon, s.focus.Lat, s.focus.Lon) <= radius
}

func (s *Server) evaluateAlerts() {
	list, _, _ := s.store.Snapshot()
	nowApproach := make(map[string]bool)
	nowLow := make(map[string]bool)
	byID := make(map[string]opensky.Aircraft)
	destOf := make(map[string]string)
	originOf := make(map[string]string)
	for _, a := range list {
		byID[a.ICAO24] = a
		dest, origin := "", ""
		if hint, ok := s.enricher.CachedRoute(a.ICAO24, a.Callsign); ok {
			dest, origin = hint.Destination, hint.Origin
		}
		destOf[a.ICAO24] = dest
		originOf[a.ICAO24] = origin
		if s.onApproach(a, dest) {
			nowApproach[a.ICAO24] = true
		}
		if s.isLowPass(a) {
			nowLow[a.ICAO24] = true
		}
	}

	s.alerts.mu.Lock()
	defer s.alerts.mu.Unlock()
	if !s.alerts.bootstrapped {
		s.alerts.approach = nowApproach
		s.alerts.lowPass = nowLow
		s.alerts.bootstrapped = true
		return
	}
	var events []AlertEvent
	at := time.Now().UTC().Format(time.RFC3339)
	for id := range nowApproach {
		if s.alerts.approach[id] {
			continue
		}
		a := byID[id]
		events = append(events, AlertEvent{
			Type: AlertApproach, ICAO24: id, Callsign: a.Callsign,
			Origin: originOf[id], Dest: destOf[id],
			Lat: a.Lat, Lon: a.Lon, AltitudeM: a.AltitudeM,
			Focus: s.focus.ICAO, At: at,
		})
	}
	for id := range nowLow {
		if s.alerts.lowPass[id] {
			continue
		}
		a := byID[id]
		events = append(events, AlertEvent{
			Type: AlertLowPass, ICAO24: id, Callsign: a.Callsign,
			Origin: originOf[id], Dest: destOf[id],
			Lat: a.Lat, Lon: a.Lon, AltitudeM: a.AltitudeM,
			Focus: s.focus.ICAO, At: at,
		})
	}
	s.alerts.approach = nowApproach
	s.alerts.lowPass = nowLow
	for _, ev := range events {
		s.emitAlert(ev)
	}
}

func (s *Server) emitAlert(ev AlertEvent) {
	payload, err := jsonMarshal(map[string]any{"type": "alert", "alert": ev})
	if err == nil {
		s.hub.broadcast(string(payload))
	}
	if s.alertWebhook == "" {
		return
	}
	go s.postWebhook(ev)
}

// Overridable in tests.
var alertHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

func (s *Server) postWebhook(ev AlertEvent) {
	body, err := jsonMarshal(ev)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, s.alertWebhook, bytes.NewReader(body))
	if err != nil {
		slog.Warn("alert webhook request", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wroclaw-sky-alerts")
	resp, err := alertHTTPClient().Do(req)
	if err != nil {
		slog.Warn("alert webhook", "err", err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		slog.Warn("alert webhook status", "status", resp.Status)
	}
}
