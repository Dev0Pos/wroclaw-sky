package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const liveCookieName = "wroclaw_sky_live"

// handleLiveAuth sets or clears an HttpOnly cookie for LIVE_TOKEN.
// POST /api/auth/live  body/query token=… → Set-Cookie
// DELETE /api/auth/live → clear cookie
func (s *Server) handleLiveAuth(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		if token == "" {
			var body struct {
				Token string `json:"token"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			token = strings.TrimSpace(body.Token)
		}
		if token == "" {
			got := strings.TrimSpace(r.Header.Get("Authorization"))
			if strings.HasPrefix(strings.ToLower(got), "bearer ") {
				token = strings.TrimSpace(got[7:])
			}
		}
		if s.liveToken != "" && token != s.liveToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if s.liveToken == "" {
			// No token configured — nothing to set; report ok.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "required": false})
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     liveCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int((24 * time.Hour).Seconds()),
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "required": true})
	case http.MethodDelete:
		http.SetCookie(w, &http.Cookie{
			Name:     liveCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			MaxAge:   -1,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"required": s.liveToken != "",
			"ok":       s.authorizedLive(r),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func tokenFromCookie(r *http.Request) string {
	c, err := r.Cookie(liveCookieName)
	if err != nil || c == nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}
