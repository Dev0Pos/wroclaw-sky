package server

import (
	"embed"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/opensky"
)

//go:embed templates/*.html
var templateFS embed.FS

type Server struct {
	store *cache.Store
	tmpl  *template.Template
}

func New(store *cache.Store) (*Server, error) {
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"alt":   opensky.FormatAlt,
		"speed": opensky.FormatSpeed,
	}).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{store: store, tmpl: tmpl}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/flights", s.handleFlights)
	mux.HandleFunc("/refresh", s.handleRefresh)
	mux.HandleFunc("/api/aircraft", s.handleAPI)
	mux.HandleFunc("/api/fetch", s.handleFetch)
	mux.HandleFunc("/healthz", s.handleHealthz)
	return mux
}

type pageData struct {
	Aircraft  []opensky.Aircraft
	Count     int
	Airborne  int
	UpdatedAt string
	Error     string
	CenterLat float64
	CenterLon float64
}

func (s *Server) snapshotData() pageData {
	list, updated, err := s.store.Snapshot()
	sort.Slice(list, func(i, j int) bool {
		return list[i].Callsign < list[j].Callsign
	})
	airborne := 0
	for _, a := range list {
		if !a.OnGround {
			airborne++
		}
	}
	data := pageData{
		Aircraft:  list,
		Count:     len(list),
		Airborne:  airborne,
		CenterLat: 51.1079,
		CenterLon: 17.0385,
	}
	if !updated.IsZero() {
		data.UpdatedAt = updated.Local().Format(time.RFC822)
	}
	if err != nil {
		data.Error = err.Error()
	}
	return data
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "index.html", s.snapshotData()); err != nil {
		slog.Error("template", "err", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (s *Server) handleFlights(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "flights.html", s.snapshotData()); err != nil {
		slog.Error("template", "err", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// handleRefresh fetches OpenSky once, then returns the flights partial.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.store.Refresh()
	s.handleFlights(w, r)
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	list, updated, err := s.store.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"updated_at": updated.UTC().Format(time.RFC3339),
		"error":      errString(err),
		"aircraft":   list,
	})
}

// handleFetch refreshes from OpenSky and returns JSON. Used by a UI instance
// that cannot reach OpenSky (e.g. Render) via UPSTREAM_URL.
// Optional shared secret: FETCH_TOKEN / Authorization: Bearer …
func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.store.RefreshOpenSky()
	s.handleAPI(w, r)
}

func (s *Server) authorized(r *http.Request) bool {
	want := strings.TrimSpace(os.Getenv("FETCH_TOKEN"))
	if want == "" {
		return true
	}
	got := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(got), "bearer ") {
		got = strings.TrimSpace(got[7:])
	}
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	return got == want
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	_, updated, err := s.store.Snapshot()
	status := "ok"
	code := http.StatusOK
	if err != nil && updated.IsZero() {
		status = "error"
		code = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     status,
		"updated_at": updated.UTC().Format(time.RFC3339),
		"error":      errString(err),
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
