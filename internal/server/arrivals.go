package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/meta"
)

// arrivalRow is an inbound focus-airport flight for the arrivals board.
type arrivalRow struct {
	ICAO24   string  `json:"icao24"`
	Callsign string  `json:"callsign"`
	Origin   string  `json:"origin,omitempty"`
	Airline  string  `json:"airline,omitempty"`
	DistM    float64 `json:"dist_m"`
	ETASec   int     `json:"eta_sec"`
	Hint     string  `json:"hint"`
	Approach bool    `json:"approach"`
}

// buildArrivals returns airborne focus-bound flights sorted by ETA then distance.
func buildArrivals(focus geo.Focus, rows []flightRow, radiusM float64) []arrivalRow {
	if radiusM <= 0 {
		radiusM = geo.ApproachRadiusM
	}
	out := make([]arrivalRow, 0)
	for _, r := range rows {
		if r.OnGround || !strings.EqualFold(strings.TrimSpace(r.Destination), focus.ICAO) {
			continue
		}
		if r.Lat == 0 && r.Lon == 0 {
			continue
		}
		dist := geo.HaversineM(r.Lat, r.Lon, focus.Lat, focus.Lon)
		eta := geo.ETASeconds(dist, r.Velocity)
		hint := geo.FormatDistKm(dist)
		if e := geo.FormatETA(eta); e != "" {
			hint += " · " + e
		}
		out = append(out, arrivalRow{
			ICAO24:   r.ICAO24,
			Callsign: r.Callsign,
			Origin:   r.Origin,
			Airline:  meta.AirlineHint(r.Callsign),
			DistM:    dist,
			ETASec:   eta,
			Hint:     hint,
			Approach: dist <= radiusM,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aj := out[i], out[j]
		if (ai.ETASec == 0) != (aj.ETASec == 0) {
			return ai.ETASec != 0
		}
		if ai.ETASec != aj.ETASec {
			return ai.ETASec < aj.ETASec
		}
		if ai.DistM != aj.DistM {
			return ai.DistM < aj.DistM
		}
		return ai.Callsign < aj.Callsign
	})
	return out
}

func (s *Server) currentArrivals() []arrivalRow {
	list, _, _ := s.store.Snapshot()
	rows := make([]flightRow, 0, len(list))
	for _, a := range list {
		row := flightRow{Aircraft: a}
		if hint, ok := s.enricher.CachedRoute(a.ICAO24, a.Callsign); ok {
			row.Origin = hint.Origin
			row.Destination = hint.Destination
		}
		rows = append(rows, row)
	}
	return buildArrivals(s.focus, rows, s.approachRadiusM)
}

func (s *Server) handleArrivalsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	arrivals := s.currentArrivals()
	payload := map[string]any{
		"focus":    s.focus.ICAO,
		"count":    len(arrivals),
		"arrivals": arrivals,
	}
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("download")), "1") ||
		strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("export")), "1") {
		payload["exported_at"] = time.Now().UTC().Format(time.RFC3339)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="arrivals.json"`)
		_ = json.NewEncoder(w).Encode(payload)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// departureRow is an outbound focus-airport flight for the departures board.
type departureRow struct {
	ICAO24      string  `json:"icao24"`
	Callsign    string  `json:"callsign"`
	Destination string  `json:"destination,omitempty"`
	Airline     string  `json:"airline,omitempty"`
	DistM       float64 `json:"dist_m"`
	ETASec      int     `json:"eta_sec"`
	Hint        string  `json:"hint"`
	Approach    bool    `json:"approach"`
}

// buildDepartures returns airborne focus-origin flights sorted by ETA then distance.
func buildDepartures(focus geo.Focus, rows []flightRow, radiusM float64) []departureRow {
	if radiusM <= 0 {
		radiusM = geo.ApproachRadiusM
	}
	out := make([]departureRow, 0)
	for _, r := range rows {
		if r.OnGround || !strings.EqualFold(strings.TrimSpace(r.Origin), focus.ICAO) {
			continue
		}
		if r.Lat == 0 && r.Lon == 0 {
			continue
		}
		dist := geo.HaversineM(r.Lat, r.Lon, focus.Lat, focus.Lon)
		eta := geo.ETASeconds(dist, r.Velocity)
		hint := geo.FormatDistKm(dist)
		if e := geo.FormatETA(eta); e != "" {
			hint += " · " + e
		}
		out = append(out, departureRow{
			ICAO24:      r.ICAO24,
			Callsign:    r.Callsign,
			Destination: r.Destination,
			Airline:     meta.AirlineHint(r.Callsign),
			DistM:       dist,
			ETASec:      eta,
			Hint:        hint,
			Approach:    dist <= radiusM,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aj := out[i], out[j]
		if (ai.ETASec == 0) != (aj.ETASec == 0) {
			return ai.ETASec != 0
		}
		if ai.ETASec != aj.ETASec {
			return ai.ETASec < aj.ETASec
		}
		if ai.DistM != aj.DistM {
			return ai.DistM < aj.DistM
		}
		return ai.Callsign < aj.Callsign
	})
	return out
}

func (s *Server) currentDepartures() []departureRow {
	list, _, _ := s.store.Snapshot()
	rows := make([]flightRow, 0, len(list))
	for _, a := range list {
		row := flightRow{Aircraft: a}
		if hint, ok := s.enricher.CachedRoute(a.ICAO24, a.Callsign); ok {
			row.Origin = hint.Origin
			row.Destination = hint.Destination
		}
		rows = append(rows, row)
	}
	return buildDepartures(s.focus, rows, s.approachRadiusM)
}

func (s *Server) handleDeparturesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	departures := s.currentDepartures()
	payload := map[string]any{
		"focus":      s.focus.ICAO,
		"count":      len(departures),
		"departures": departures,
	}
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("download")), "1") ||
		strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("export")), "1") {
		payload["exported_at"] = time.Now().UTC().Format(time.RFC3339)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="departures.json"`)
		_ = json.NewEncoder(w).Encode(payload)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
