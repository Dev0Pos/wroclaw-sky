package server

import (
	"bytes"
	"encoding/json"
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
	maxAlertHist  = 40
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

// AlertDigest is a batched webhook payload for multiple edge-triggered alerts.
type AlertDigest struct {
	Type   string       `json:"type"` // "digest"
	Focus  string       `json:"focus"`
	Count  int          `json:"count"`
	At     string       `json:"at"`
	Alerts []AlertEvent `json:"alerts"`
}

type alertState struct {
	mu           sync.Mutex
	approach     map[string]bool
	lowPass      map[string]bool
	bootstrapped bool
	history      []AlertEvent
}

func (s *Server) SetAlertWebhook(url string) {
	s.alertWebhook = strings.TrimSpace(url)
}

// SetAlertWebhookDigest batches webhook POSTs into one digest per evaluate cycle.
func (s *Server) SetAlertWebhookDigest(on bool) {
	s.alertWebhookDigest = on
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
		s.alerts.history = append([]AlertEvent{ev}, s.alerts.history...)
		if len(s.alerts.history) > maxAlertHist {
			s.alerts.history = s.alerts.history[:maxAlertHist]
		}
		s.alertTotal.Add(1)
		s.emitAlertSSE(ev)
	}
	if len(events) == 0 || s.alertWebhook == "" {
		return
	}
	if s.alertWebhookDigest {
		go s.postWebhookDigest(events)
		return
	}
	for _, ev := range events {
		go s.postWebhook(ev)
	}
}

func (s *Server) recentAlerts() []AlertEvent {
	s.alerts.mu.Lock()
	defer s.alerts.mu.Unlock()
	out := make([]AlertEvent, len(s.alerts.history))
	copy(out, s.alerts.history)
	return out
}

func (s *Server) handleAlertsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	alerts := s.recentAlerts()
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("download")), "1") ||
		strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("export")), "1") {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="alerts.json"`)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"exported_at": time.Now().UTC().Format(time.RFC3339),
			"focus":       s.focus.ICAO,
			"alerts":      alerts,
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"alerts": alerts})
}

func (s *Server) emitAlertSSE(ev AlertEvent) {
	payload, err := jsonMarshal(map[string]any{"type": "alert", "alert": ev})
	if err == nil {
		s.hub.broadcast(string(payload))
	}
}

// emitAlert keeps older tests working: SSE + optional single webhook.
func (s *Server) emitAlert(ev AlertEvent) {
	s.emitAlertSSE(ev)
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
	s.webhookTotal.Add(1)
	body, err := jsonMarshal(ev)
	if err != nil {
		s.webhookErrors.Add(1)
		return
	}
	s.doWebhookPOST(body)
}

func (s *Server) postWebhookDigest(events []AlertEvent) {
	if len(events) == 0 {
		return
	}
	s.webhookTotal.Add(1)
	at := time.Now().UTC().Format(time.RFC3339)
	if events[0].At != "" {
		at = events[0].At
	}
	body, err := jsonMarshal(AlertDigest{
		Type:   "digest",
		Focus:  s.focus.ICAO,
		Count:  len(events),
		At:     at,
		Alerts: events,
	})
	if err != nil {
		s.webhookErrors.Add(1)
		return
	}
	s.doWebhookPOST(body)
}

func (s *Server) doWebhookPOST(body []byte) {
	req, err := http.NewRequest(http.MethodPost, s.alertWebhook, bytes.NewReader(body))
	if err != nil {
		s.webhookErrors.Add(1)
		slog.Warn("alert webhook request", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wroclaw-sky-alerts")
	resp, err := alertHTTPClient().Do(req)
	if err != nil {
		s.webhookErrors.Add(1)
		slog.Warn("alert webhook", "err", err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		s.webhookErrors.Add(1)
		slog.Warn("alert webhook status", "status", resp.Status)
	}
}
