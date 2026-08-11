package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/opensky"
)

// handleFocus lists known focus airports or switches the active focus at runtime.
// GET  /api/focus — current + known ICAOs
// POST /api/focus — body/query: icao, optional lat/lon/city/radius_km
func (s *Server) handleFocus(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.writeFocusJSON(w)
	case http.MethodPost:
		if !s.authorizedLive(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		icao := strings.TrimSpace(r.URL.Query().Get("icao"))
		latStr := strings.TrimSpace(r.URL.Query().Get("lat"))
		lonStr := strings.TrimSpace(r.URL.Query().Get("lon"))
		city := strings.TrimSpace(r.URL.Query().Get("city"))
		radiusStr := strings.TrimSpace(r.URL.Query().Get("radius_km"))
		if r.Header.Get("Content-Type") != "" && strings.Contains(r.Header.Get("Content-Type"), "json") {
			var body struct {
				ICAO     string  `json:"icao"`
				Lat      string  `json:"lat"`
				Lon      string  `json:"lon"`
				City     string  `json:"city"`
				RadiusKM float64 `json:"radius_km"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.ICAO != "" {
				icao = body.ICAO
			}
			if body.Lat != "" {
				latStr = body.Lat
			}
			if body.Lon != "" {
				lonStr = body.Lon
			}
			if body.City != "" {
				city = body.City
			}
			if body.RadiusKM > 0 && radiusStr == "" {
				radiusStr = strconv.FormatFloat(body.RadiusKM, 'f', -1, 64)
			}
		}
		focus, err := geo.ResolveFocus(icao, latStr, lonStr, city)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		radiusKm := s.focusRadiusKM
		if radiusStr != "" {
			v, err := strconv.ParseFloat(radiusStr, 64)
			if err != nil || v <= 0 {
				http.Error(w, "radius_km invalid", http.StatusBadRequest)
				return
			}
			radiusKm = v
		}
		if radiusKm <= 0 {
			radiusKm = 80
		}
		s.SetFocus(focus)
		s.focusRadiusKM = radiusKm
		s.store.SetBBox(opensky.BBoxAround(focus.Lat, focus.Lon, radiusKm))
		// Reset alert bootstrap so new focus does not replay old alerts.
		s.alerts.mu.Lock()
		s.alerts.bootstrapped = false
		s.alerts.approach = nil
		s.alerts.lowPass = nil
		s.alerts.mu.Unlock()
		s.writeFocusJSON(w)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) writeFocusJSON(w http.ResponseWriter) {
	clat, clon := s.store.BBox().Center()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"focus":     s.focus,
		"label":     s.label,
		"radius_km": s.focusRadiusKM,
		"center":    []float64{clat, clon},
		"known":     geo.KnownFocusICAOs(),
		"bbox":      s.store.BBox(),
	})
}
