package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/viewstate"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

const routeWarmBudget = 2500 * time.Millisecond

type Server struct {
	store    *cache.Store
	enricher *meta.Enricher
	tmpl     *template.Template
	live     liveState
	hub      *sseHub
	focus    geo.Focus
	label    string

	liveToken       string
	fetchToken      string
	alertWebhook    string
	approachRadiusM float64
	lowPassAltM     float64
	focusRadiusKM   float64
	liveCookieTTL   time.Duration
	authLimit       *authLimiter
	alerts          alertState

	refreshTotal   atomic.Int64
	refreshErrors  atomic.Int64
	lastRefresh    atomic.Int64 // unix seconds
	webhookTotal   atomic.Int64
	webhookErrors  atomic.Int64
	alertTotal     atomic.Int64
	sseDisconnects atomic.Int64
}

func New(store *cache.Store, enricher *meta.Enricher) (*Server, error) {
	if enricher == nil {
		enricher = meta.NewEnricher()
	}
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	focus := geo.DefaultFocus()
	return &Server{
		store:           store,
		enricher:        enricher,
		tmpl:            tmpl,
		hub:             newSSEHub(),
		focus:           focus,
		label:           focus.Label(),
		approachRadiusM: geo.ApproachRadiusM,
		focusRadiusKM:   80,
		alerts:          alertState{},
	}, nil
}

// parseTemplates builds the HTML templates (overridable in tests).
var parseTemplates = func() (*template.Template, error) {
	return template.New("").Funcs(template.FuncMap{
		"alt":         opensky.FormatAlt,
		"speed":       opensky.FormatSpeed,
		"focusHint":   geo.FormatFocusHint,
		"airlineHint": meta.AirlineHint,
		"onApproach":  geo.OnApproachTo,
	}).ParseFS(templateFS, "templates/*.html")
}

// SetMapLabel overrides the map badge text (e.g. when using a custom bbox).
func (s *Server) SetMapLabel(label string) {
	if strings.TrimSpace(label) != "" {
		s.label = strings.TrimSpace(label)
	}
}

// SetFocus sets the focus airport for arrivals, approach, and map circle.
func (s *Server) SetFocus(focus geo.Focus) {
	if focus.ICAO == "" {
		return
	}
	s.focus = focus
	s.label = focus.Label()
}

// SetLiveToken protects /api/live and /api/events when non-empty.
func (s *Server) SetLiveToken(token string) {
	s.liveToken = strings.TrimSpace(token)
}

// SetFetchToken protects /api/fetch and /api/meta when non-empty.
func (s *Server) SetFetchToken(token string) {
	s.fetchToken = strings.TrimSpace(token)
}

// SetFocusRadiusKM sets the default bbox radius used on runtime focus switch.
func (s *Server) SetFocusRadiusKM(km float64) {
	if km > 0 {
		s.focusRadiusKM = km
	}
}

// formatEPWRHint keeps tests covering the default-focus hint path.
func formatEPWRHint(dest string, lat, lon, velocity float64, onGround bool) string {
	return geo.FormatFocusHint(geo.DefaultFocus(), dest, lat, lon, velocity, onGround)
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
	mux.HandleFunc("/api/live", s.handleLive)
	mux.HandleFunc("/api/auth/live", s.handleLiveAuth)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/focus", s.handleFocus)
	mux.HandleFunc("/api/trails", s.handleTrailsExport)
	mux.HandleFunc("/api/alerts", s.handleAlertsAPI)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/manifest.webmanifest", s.handleManifest)
	mux.HandleFunc("/sw.js", s.handleServiceWorker)

	static, err := fs.Sub(staticFS, "static")
	if err == nil {
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	}
	return mux
}

// flightRow is Aircraft plus optional route for list filters / data attributes.
type flightRow struct {
	opensky.Aircraft
	Origin      string
	Destination string
}

type pageData struct {
	Aircraft          []flightRow
	Arrivals          []arrivalRow
	Airlines          []string
	View              viewstate.State
	Focus             geo.Focus
	FocusOptions      []string
	FocusPresets      []string
	Count             int
	Airborne          int
	UpdatedAt         string
	Error             string
	Stale             bool
	CenterLat         float64
	CenterLon         float64
	MapLabel          string
	LiveTokenRequired bool
	ApproachRadiusM   float64
	LowPassAltM       float64
}

type aircraftJSON struct {
	opensky.Aircraft
	Origin      string `json:"origin,omitempty"`
	Destination string `json:"destination,omitempty"`
	Airline     string `json:"airline,omitempty"`
}

func (s *Server) snapshotData() pageData {
	list, updated, err := s.store.Snapshot()
	sort.Slice(list, func(i, j int) bool {
		return list[i].Callsign < list[j].Callsign
	})
	airborne := 0
	rows := make([]flightRow, 0, len(list))
	for _, a := range list {
		if !a.OnGround {
			airborne++
		}
		row := flightRow{Aircraft: a}
		if hint, ok := s.enricher.CachedRoute(a.ICAO24, a.Callsign); ok {
			row.Origin = hint.Origin
			row.Destination = hint.Destination
		}
		rows = append(rows, row)
	}
	clat, clon := s.store.BBox().Center()
	data := pageData{
		Aircraft:          rows,
		Arrivals:          buildArrivals(s.focus, rows, s.approachRadiusM),
		Airlines:          meta.AirlineOptions(),
		View:              viewstate.Default(),
		Focus:             s.focus,
		FocusOptions:      geo.KnownFocusICAOs(),
		FocusPresets:      geo.PolishPresets(),
		Count:             len(rows),
		Airborne:          airborne,
		CenterLat:         clat,
		CenterLon:         clon,
		MapLabel:          s.label,
		LiveTokenRequired: s.liveToken != "",
		ApproachRadiusM:   s.approachRadiusM,
		LowPassAltM:       s.lowPassAltM,
		Stale:             s.store.Stale(),
	}
	if !updated.IsZero() {
		data.UpdatedAt = updated.Local().Format(time.RFC822)
	}
	if err != nil {
		data.Error = err.Error()
	}
	return data
}

func (s *Server) warmRoutes() {
	list, _, _ := s.store.Snapshot()
	items := make([]meta.WarmItem, 0, len(list))
	for _, a := range list {
		items = append(items, meta.WarmItem{ICAO24: a.ICAO24, Callsign: a.Callsign})
	}
	s.enricher.WarmRoutes(items, routeWarmBudget)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := s.snapshotData()
	data.View = viewstate.Parse(r.URL.Query())
	if data.View.Focus != "" && data.View.Focus != s.focus.ICAO {
		if f, err := geo.ResolveFocus(data.View.Focus, "", "", ""); err == nil {
			s.SetFocus(f)
			radius := s.focusRadiusKM
			if radius <= 0 {
				radius = 80
			}
			s.store.SetBBox(opensky.BBoxAround(f.Lat, f.Lon, radius))
			data = s.snapshotData()
			data.View = viewstate.Parse(r.URL.Query())
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
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

// handleRefresh fetches OpenSky once, warms routes for EPWR filters, then returns the flights partial.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.refreshAndWarm()
	s.handleFlights(w, r)
}

func (s *Server) aircraftPayload() map[string]any {
	list, updated, err := s.store.Snapshot()
	out := make([]aircraftJSON, 0, len(list))
	for _, a := range list {
		row := aircraftJSON{Aircraft: a, Airline: meta.AirlineHint(a.Callsign)}
		if hint, ok := s.enricher.CachedRoute(a.ICAO24, a.Callsign); ok {
			row.Origin = hint.Origin
			row.Destination = hint.Destination
		}
		out = append(out, row)
	}
	return map[string]any{
		"type":         "update",
		"updated_at":   updated.UTC().Format(time.RFC3339),
		"error":        errString(err),
		"stale":        s.store.Stale(),
		"circuit_open": s.store.CircuitOpen(),
		"aircraft":     out,
		"trails":       s.store.Trails(),
		"count":        len(out),
		"focus":        s.focus.ICAO,
	}
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.aircraftPayload())
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
	local.ADSBdbBaseURL = s.enricher.ADSBdbBaseURL
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
	s.refreshTotal.Add(1)
	s.store.RefreshOpenSky()
	_, _, err := s.store.Snapshot()
	if err != nil {
		s.refreshErrors.Add(1)
	} else {
		s.lastRefresh.Store(time.Now().Unix())
	}
	s.publishUpdate()
	s.handleAPI(w, r)
}

func (s *Server) authorized(r *http.Request) bool {
	want := s.fetchToken
	if want == "" {
		want = strings.TrimSpace(os.Getenv("FETCH_TOKEN"))
	}
	if want == "" {
		return true
	}
	return tokenMatches(r, want)
}

func (s *Server) authorizedLive(r *http.Request) bool {
	want := s.liveToken
	if want == "" {
		return true
	}
	return tokenMatches(r, want)
}

func tokenMatches(r *http.Request, want string) bool {
	got := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(got), "bearer ") {
		got = strings.TrimSpace(got[7:])
	}
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	if got == "" {
		got = tokenFromCookie(r)
	}
	return got == want
}

// handleTrailsExport returns current trails as downloadable JSON.
// GET /api/trails
func (s *Server) handleTrailsExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="trails.json"`)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"focus":       s.focus.ICAO,
		"trails":      s.store.Trails(),
	})
}

// handleHealthz is a liveness probe: always 200 if the process is up.
// Do not fail on OpenSky/upstream errors — that would make Render mark the
// service unhealthy and stop routing traffic after a failed Refresh.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	list, updated, err := s.store.Snapshot()
	live, until := s.liveStatus()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":       "ok",
		"upstream":     strings.TrimSpace(s.store.UpstreamURL) != "",
		"updated_at":   updated.UTC().Format(time.RFC3339),
		"error":        errString(err),
		"stale":        s.store.Stale(),
		"circuit_open": s.store.CircuitOpen(),
		"live":         live,
		"live_until":   untilUTC(until),
		"focus":        s.focus.ICAO,
		"aircraft":     len(list),
		"sse_clients":  s.hub.len(),
	})
}

// handleReadyz is a readiness probe: 503 while the OpenSky circuit breaker is open.
func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	open := s.store.CircuitOpen()
	stale := s.store.Stale()
	w.Header().Set("Content-Type", "application/json")
	body := map[string]any{
		"ready":        !open,
		"circuit_open": open,
		"stale":        stale,
		"focus":        s.focus.ICAO,
	}
	if open {
		w.WriteHeader(http.StatusServiceUnavailable)
		body["status"] = "not_ready"
	} else {
		w.WriteHeader(http.StatusOK)
		body["status"] = "ok"
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, _, _ := s.store.Snapshot()
	live, _ := s.liveStatus()
	liveN := 0
	if live {
		liveN = 1
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(w, "# HELP wroclaw_sky_refresh_total OpenSky/upstream refresh attempts\n")
	_, _ = fmt.Fprintf(w, "# TYPE wroclaw_sky_refresh_total counter\n")
	_, _ = fmt.Fprintf(w, "wroclaw_sky_refresh_total %d\n", s.refreshTotal.Load())
	_, _ = fmt.Fprintf(w, "# HELP wroclaw_sky_refresh_errors_total Failed refreshes\n")
	_, _ = fmt.Fprintf(w, "# TYPE wroclaw_sky_refresh_errors_total counter\n")
	_, _ = fmt.Fprintf(w, "wroclaw_sky_refresh_errors_total %d\n", s.refreshErrors.Load())
	_, _ = fmt.Fprintf(w, "# HELP wroclaw_sky_last_refresh_unixtime Last successful refresh unix time\n")
	_, _ = fmt.Fprintf(w, "# TYPE wroclaw_sky_last_refresh_unixtime gauge\n")
	_, _ = fmt.Fprintf(w, "wroclaw_sky_last_refresh_unixtime %d\n", s.lastRefresh.Load())
	_, _ = fmt.Fprintf(w, "# HELP wroclaw_sky_aircraft Aircraft in latest snapshot\n")
	_, _ = fmt.Fprintf(w, "# TYPE wroclaw_sky_aircraft gauge\n")
	_, _ = fmt.Fprintf(w, "wroclaw_sky_aircraft %d\n", len(list))
	_, _ = fmt.Fprintf(w, "# HELP wroclaw_sky_live Live poller active (0/1)\n")
	_, _ = fmt.Fprintf(w, "# TYPE wroclaw_sky_live gauge\n")
	_, _ = fmt.Fprintf(w, "wroclaw_sky_live %d\n", liveN)
	_, _ = fmt.Fprintf(w, "# HELP wroclaw_sky_sse_clients Connected SSE clients\n")
	_, _ = fmt.Fprintf(w, "# TYPE wroclaw_sky_sse_clients gauge\n")
	_, _ = fmt.Fprintf(w, "wroclaw_sky_sse_clients %d\n", s.hub.len())
	circuitN := 0
	if s.store.CircuitOpen() {
		circuitN = 1
	}
	_, _ = fmt.Fprintf(w, "# HELP wroclaw_sky_circuit_open OpenSky circuit breaker open (0/1)\n")
	_, _ = fmt.Fprintf(w, "# TYPE wroclaw_sky_circuit_open gauge\n")
	_, _ = fmt.Fprintf(w, "wroclaw_sky_circuit_open %d\n", circuitN)
	_, _ = fmt.Fprintf(w, "# HELP wroclaw_sky_webhook_total Alert webhook attempts\n")
	_, _ = fmt.Fprintf(w, "# TYPE wroclaw_sky_webhook_total counter\n")
	_, _ = fmt.Fprintf(w, "wroclaw_sky_webhook_total %d\n", s.webhookTotal.Load())
	_, _ = fmt.Fprintf(w, "# HELP wroclaw_sky_webhook_errors_total Alert webhook failures\n")
	_, _ = fmt.Fprintf(w, "# TYPE wroclaw_sky_webhook_errors_total counter\n")
	_, _ = fmt.Fprintf(w, "wroclaw_sky_webhook_errors_total %d\n", s.webhookErrors.Load())
	_, _ = fmt.Fprintf(w, "# HELP wroclaw_sky_alerts_total Edge-triggered alerts emitted\n")
	_, _ = fmt.Fprintf(w, "# TYPE wroclaw_sky_alerts_total counter\n")
	_, _ = fmt.Fprintf(w, "wroclaw_sky_alerts_total %d\n", s.alertTotal.Load())
	_, _ = fmt.Fprintf(w, "# HELP wroclaw_sky_sse_disconnects_total SSE client disconnects\n")
	_, _ = fmt.Fprintf(w, "# TYPE wroclaw_sky_sse_disconnects_total counter\n")
	_, _ = fmt.Fprintf(w, "wroclaw_sky_sse_disconnects_total %d\n", s.sseDisconnects.Load())
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
