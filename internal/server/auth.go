package server

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const liveCookieName = "wroclaw_sky_live"

// Defaults for LIVE_TOKEN cookie / auth POST rate limit (overridable).
const (
	defaultLiveCookieTTL = 8 * time.Hour
	defaultAuthRateMax   = 10
	defaultAuthRateWin   = time.Minute
)

// Overridable in tests.
var authNow = time.Now

type authLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	window time.Duration
	max    int
}

func newAuthLimiter(max int, window time.Duration) *authLimiter {
	if max <= 0 {
		max = defaultAuthRateMax
	}
	if window <= 0 {
		window = defaultAuthRateWin
	}
	return &authLimiter{hits: make(map[string][]time.Time), window: window, max: max}
}

func (l *authLimiter) allow(key string) bool {
	if l == nil {
		return true
	}
	now := authNow()
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

func clientKey(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			xff = xff[:i]
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) liveCookieMaxAge() int {
	ttl := s.liveCookieTTL
	if ttl <= 0 {
		ttl = defaultLiveCookieTTL
	}
	return int(ttl.Seconds())
}

func (s *Server) setLiveCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     liveCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   s.liveCookieMaxAge(),
	})
}

func (s *Server) clearLiveCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     liveCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// SetLiveCookieTTL sets HttpOnly cookie lifetime (default 8h).
func (s *Server) SetLiveCookieTTL(d time.Duration) {
	if d > 0 {
		s.liveCookieTTL = d
	}
}

// SetAuthRateLimit configures POST /api/auth/live rate limit per client.
func (s *Server) SetAuthRateLimit(max int, window time.Duration) {
	s.authLimit = newAuthLimiter(max, window)
}

// handleLiveAuth sets or clears an HttpOnly cookie for LIVE_TOKEN.
// POST /api/auth/live  body/query token=… → Set-Cookie (rate-limited)
// GET  /api/auth/live  → status; refreshes cookie TTL when already authorized (rotate)
// DELETE /api/auth/live → clear cookie
func (s *Server) handleLiveAuth(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if s.authLimit == nil {
			s.authLimit = newAuthLimiter(defaultAuthRateMax, defaultAuthRateWin)
		}
		if !s.authLimit.allow(clientKey(r)) {
			http.Error(w, "rate limit", http.StatusTooManyRequests)
			return
		}
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
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "required": false})
			return
		}
		s.setLiveCookie(w, token)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "required": true, "ttl_sec": s.liveCookieMaxAge(),
		})
	case http.MethodDelete:
		s.clearLiveCookie(w)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	case http.MethodGet:
		ok := s.authorizedLive(r)
		if ok && s.liveToken != "" {
			// Rotate / refresh TTL while the session is still valid.
			s.setLiveCookie(w, s.liveToken)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"required": s.liveToken != "",
			"ok":       ok,
			"ttl_sec":  s.liveCookieMaxAge(),
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
