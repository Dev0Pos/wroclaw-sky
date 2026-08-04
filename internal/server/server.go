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
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
)

//go:embed templates/*.html
var templateFS embed.FS

type Server struct {
	store    *cache.Store
	enricher *meta.Enricher
	tmpl     *template.Template
}

func New(store *cache.Store, enricher *meta.Enricher) (*Server, error) {
	if enricher == nil {
		enricher = meta.NewEnricher()
	}
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"alt":   opensky.FormatAlt,
		"speed": opensky.FormatSpeed,
	}).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{store: store, enricher: enricher, tmpl: tmpl}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/flights", s.handleFlights)
	mux.HandleFunc("/refresh", s.handleRefresh)
	mux.HandleFunc("/api/aircraft/", s.handleAircraftDetail)
	mux.HandleFunc("/api/aircraft", s.handleAPI)
	mux.HandleFunc("/api/meta", s.handleMeta)
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
		"trails":     s.store.Trails(),
	})
}

// handleAircraftDetail returns live state + route/type enrichment for one ICAO24.
// GET /api/aircraft/{icao24}
func (s *Server) handleAircraftDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	icao := strings.TrimPrefix(r.URL.Path, "/api/aircraft/")
	icao = strings.Trim(icao, "/")
	if icao == "" {
		http.NotFound(w, r)
		return
	}
	ac, ok := s.store.Find(icao)
	if !ok {
		http.Error(w, "aircraft not in current snapshot", http.StatusNotFound)
		return
	}
	detail := s.enricher.Enrich(meta.Detail{
		ICAO24:    ac.ICAO24,
		Callsign:  ac.Callsign,
		Country:   ac.Country,
		Lon:       ac.Lon,
		Lat:       ac.Lat,
		AltitudeM: ac.AltitudeM,
		Velocity:  ac.Velocity,
		Track:     ac.Track,
		Vertical:  ac.Vertical,
		OnGround:  ac.OnGround,
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(detail)
}

// handleMeta enriches icao/callsign via hexdb (no live snapshot required).
// Used by a cloud UI that proxies enrichment through the fetcher host.
// GET /api/meta?icao24=…&callsign=…
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	icao := strings.TrimSpace(r.URL.Query().Get("icao24"))
	callsign := strings.TrimSpace(r.URL.Query().Get("callsign"))
	if icao == "" && callsign == "" {
		http.Error(w, "icao24 or callsign required", http.StatusBadRequest)
		return
	}
	// Force local hexdb on the fetcher — never recurse through UpstreamURL.
	local := meta.NewEnricher()
	local.HTTP = s.enricher.HTTP
	local.BaseURL = s.enricher.BaseURL
	detail := local.Enrich(meta.Detail{ICAO24: icao, Callsign: callsign})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(detail)
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

// handleHealthz is a liveness probe: always 200 if the process is up.
// Do not fail on OpenSky/upstream errors — that would make Render mark the
// service unhealthy and stop routing traffic after a failed Refresh.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	_, updated, err := s.store.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "ok",
		"upstream":   strings.TrimSpace(s.store.UpstreamURL) != "",
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
