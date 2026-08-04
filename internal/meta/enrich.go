package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Overridable in tests for defensive error paths after url.Parse.
var newHTTPRequest = http.NewRequest

// Detail is live ADS-B state plus optional enrichment (type, route).
type Detail struct {
	ICAO24       string  `json:"icao24"`
	Callsign     string  `json:"callsign"`
	Country      string  `json:"country"`
	Lon          float64 `json:"lon"`
	Lat          float64 `json:"lat"`
	AltitudeM    float64 `json:"altitude_m"`
	Velocity     float64 `json:"velocity"`
	Track        float64 `json:"track"`
	Vertical     float64 `json:"vertical"`
	OnGround     bool    `json:"on_ground"`
	Registration string  `json:"registration,omitempty"`
	TypeCode     string  `json:"type_code,omitempty"`
	TypeName     string  `json:"type_name,omitempty"`
	Manufacturer string  `json:"manufacturer,omitempty"`
	Operator     string  `json:"operator,omitempty"`
	PhotoURL     string  `json:"photo_url,omitempty"`
	Origin       string  `json:"origin,omitempty"`      // ICAO
	Destination  string  `json:"destination,omitempty"` // ICAO
	OriginName   string  `json:"origin_name,omitempty"`
	DestName     string  `json:"dest_name,omitempty"`
	OriginCity   string  `json:"origin_city,omitempty"`
	DestCity     string  `json:"dest_city,omitempty"`
	Route        string  `json:"route,omitempty"` // e.g. EPWA-EDDF
	RouteSource  string  `json:"route_source,omitempty"`
	Error        string  `json:"error,omitempty"`
}

// Enricher loads aircraft/route metadata with a small TTL cache.
//
// Order: cache → UpstreamURL /api/meta (fetcher) → adsbdb → hexdb (gap fill).
type Enricher struct {
	HTTP          *http.Client
	BaseURL       string // hexdb base (tests override)
	ADSBdbBaseURL string // tests override; default api.adsbdb.com/v0
	UpstreamURL   string
	UpstreamToken string

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	at   time.Time
	data Detail
}

const (
	cacheTTL     = 30 * time.Minute
	localTimeout = 4 * time.Second
	upstreamMeta = "/api/meta"
)

func NewEnricher() *Enricher {
	return &Enricher{
		HTTP:    &http.Client{Timeout: localTimeout},
		BaseURL: "https://hexdb.io",
		cache:   make(map[string]cacheEntry),
	}
}

func (e *Enricher) hexBase() string {
	if e.BaseURL != "" {
		return strings.TrimRight(e.BaseURL, "/")
	}
	return "https://hexdb.io"
}

func (e *Enricher) adsbdbBase() string {
	if e.ADSBdbBaseURL != "" {
		return strings.TrimRight(e.ADSBdbBaseURL, "/")
	}
	return adsbdbBase
}

func (e *Enricher) client() *http.Client {
	if e.HTTP != nil {
		return e.HTTP
	}
	return &http.Client{Timeout: localTimeout}
}

// Enrich fills registration/type/route fields. Soft-fails on provider errors.
func (e *Enricher) Enrich(d Detail) Detail {
	key := strings.ToLower(d.ICAO24) + "|" + strings.ToUpper(strings.TrimSpace(d.Callsign))
	e.mu.Lock()
	if ent, ok := e.cache[key]; ok && time.Since(ent.at) < cacheTTL {
		cached := ent.data
		e.mu.Unlock()
		return mergeLive(cached, d)
	}
	e.mu.Unlock()

	out := d
	if strings.TrimSpace(e.UpstreamURL) != "" {
		if enriched, err := e.fetchUpstreamMeta(d.ICAO24, d.Callsign); err == nil {
			out = mergeEnrichment(out, enriched)
		}
	}
	// Local providers: full enrich when upstream missing/empty, else gap-fill aircraft only.
	if out.Registration == "" && out.TypeCode == "" && out.Route == "" {
		out = e.enrichLocal(out)
	} else if incomplete(out) {
		out = mergeEnrichment(out, e.enrichLocal(Detail{ICAO24: d.ICAO24, Callsign: d.Callsign}))
	}
	out = fillAirportNames(out)

	// Do not cache empty misses — a tunnel blip must not stick for cacheTTL.
	if out.Registration != "" || out.TypeCode != "" || out.Route != "" {
		e.mu.Lock()
		e.cache[key] = cacheEntry{at: time.Now(), data: out}
		e.mu.Unlock()
	}
	return out
}

func (e *Enricher) enrichLocal(out Detail) Detail {
	out = e.enrichADSBdb(out)
	// hexdb is often unreachable; only use it to fill aircraft gaps, never block on routes.
	if out.Registration == "" || out.TypeCode == "" {
		out = e.enrichHexDB(out)
	}
	out.Error = ""
	return out
}

// fillAirportNames uses the embedded OpenFlights DB when providers omit city/name.
func fillAirportNames(d Detail) Detail {
	if d.Origin != "" && (d.OriginCity == "" || d.OriginName == "") {
		if a, ok := LookupAirport(d.Origin); ok {
			if d.OriginCity == "" {
				d.OriginCity = a.City
			}
			if d.OriginName == "" {
				d.OriginName = a.Name
			}
		}
	}
	if d.Destination != "" && (d.DestCity == "" || d.DestName == "") {
		if a, ok := LookupAirport(d.Destination); ok {
			if d.DestCity == "" {
				d.DestCity = a.City
			}
			if d.DestName == "" {
				d.DestName = a.Name
			}
		}
	}
	return d
}

// RouteHint is origin/destination for list/map filters (e.g. EPWR).
type RouteHint struct {
	Origin      string `json:"origin,omitempty"`
	Destination string `json:"destination,omitempty"`
}

// CachedRoute returns a previously enriched route for icao+callsign, if any.
func (e *Enricher) CachedRoute(icao, callsign string) (RouteHint, bool) {
	key := strings.ToLower(icao) + "|" + strings.ToUpper(strings.TrimSpace(callsign))
	e.mu.Lock()
	defer e.mu.Unlock()
	ent, ok := e.cache[key]
	if !ok || time.Since(ent.at) >= cacheTTL {
		return RouteHint{}, false
	}
	if ent.data.Origin == "" && ent.data.Destination == "" {
		return RouteHint{}, false
	}
	return RouteHint{Origin: ent.data.Origin, Destination: ent.data.Destination}, true
}

// cachedIdentity reports whether we already attempted enrichment for this key.
func (e *Enricher) cachedIdentity(icao, callsign string) bool {
	key := strings.ToLower(icao) + "|" + strings.ToUpper(strings.TrimSpace(callsign))
	e.mu.Lock()
	defer e.mu.Unlock()
	ent, ok := e.cache[key]
	return ok && time.Since(ent.at) < cacheTTL
}

// WarmItem is a minimal aircraft identity for route warming.
type WarmItem struct {
	ICAO24   string
	Callsign string
}

// WarmRoutes enriches callsigns in parallel so list filters can use origin/destination.
// Stops after budget even if some lookups are still in flight.
func (e *Enricher) WarmRoutes(items []WarmItem, budget time.Duration) {
	if budget <= 0 || len(items) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, it := range items {
		if strings.TrimSpace(it.ICAO24) == "" {
			continue
		}
		// Skip cache hits (with or without a route) so Live refreshes stay cheap.
		if e.cachedIdentity(it.ICAO24, it.Callsign) {
			continue
		}
		wg.Add(1)
		go func(it WarmItem) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			if ctx.Err() != nil {
				return
			}
			_ = e.Enrich(Detail{ICAO24: it.ICAO24, Callsign: it.Callsign})
		}(it)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func incomplete(d Detail) bool {
	return d.Registration == "" || d.TypeCode == ""
}

func mergeLive(cached, live Detail) Detail {
	cached.Lon, cached.Lat = live.Lon, live.Lat
	cached.AltitudeM, cached.Velocity = live.AltitudeM, live.Velocity
	cached.Track, cached.Vertical = live.Track, live.Vertical
	cached.OnGround, cached.Country = live.OnGround, live.Country
	cached.Callsign = live.Callsign
	cached.ICAO24 = live.ICAO24
	cached.Error = ""
	return cached
}

func mergeEnrichment(base, extra Detail) Detail {
	if extra.Registration != "" {
		base.Registration = extra.Registration
	}
	if extra.TypeCode != "" {
		base.TypeCode = extra.TypeCode
	}
	if extra.TypeName != "" {
		base.TypeName = extra.TypeName
	}
	if extra.Manufacturer != "" {
		base.Manufacturer = extra.Manufacturer
	}
	if extra.Operator != "" {
		base.Operator = extra.Operator
	}
	if extra.PhotoURL != "" {
		base.PhotoURL = extra.PhotoURL
	}
	if extra.Route != "" {
		base.Route = extra.Route
		base.RouteSource = extra.RouteSource
		base.Origin = extra.Origin
		base.Destination = extra.Destination
		base.OriginName = extra.OriginName
		base.DestName = extra.DestName
		base.OriginCity = extra.OriginCity
		base.DestCity = extra.DestCity
	}
	return base
}

func (e *Enricher) enrichHexDB(out Detail) Detail {
	if out.Registration != "" && out.TypeCode != "" {
		return out
	}
	ac, err := e.fetchHexAircraft(out.ICAO24)
	if err != nil {
		return out
	}
	if out.Registration == "" {
		out.Registration = ac.Registration
	}
	if out.TypeCode == "" {
		out.TypeCode = ac.ICAOTypeCode
	}
	if out.TypeName == "" {
		out.TypeName = ac.Type
	}
	if out.Manufacturer == "" {
		out.Manufacturer = ac.Manufacturer
	}
	if out.Operator == "" {
		out.Operator = ac.RegisteredOwners
	}
	return out
}

func (e *Enricher) fetchUpstreamMeta(icao, callsign string) (Detail, error) {
	u, err := url.Parse(strings.TrimRight(e.UpstreamURL, "/") + upstreamMeta)
	if err != nil {
		return Detail{}, err
	}
	q := u.Query()
	q.Set("icao24", icao)
	q.Set("callsign", callsign)
	u.RawQuery = q.Encode()

	req, err := newHTTPRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return Detail{}, err
	}
	if e.UpstreamToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.UpstreamToken)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "wroclaw-sky-ui")

	// Dedicated client — do not inherit the short local provider timeout.
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Detail{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Detail{}, fmt.Errorf("upstream meta %s", resp.Status)
	}
	var d Detail
	if err := json.Unmarshal(body, &d); err != nil {
		return Detail{}, err
	}
	return d, nil
}

type hexAircraft struct {
	Registration     string `json:"Registration"`
	Manufacturer     string `json:"Manufacturer"`
	ICAOTypeCode     string `json:"ICAOTypeCode"`
	Type             string `json:"Type"`
	RegisteredOwners string `json:"RegisteredOwners"`
}

func (e *Enricher) fetchHexAircraft(icao string) (hexAircraft, error) {
	icao = strings.ToLower(strings.TrimSpace(icao))
	var zero hexAircraft
	if icao == "" {
		return zero, fmt.Errorf("empty icao")
	}
	body, code, err := e.get(e.hexBase() + "/api/v1/aircraft/" + icao)
	if err != nil {
		return zero, err
	}
	if code == http.StatusNotFound {
		return zero, fmt.Errorf("aircraft not found")
	}
	if code != http.StatusOK {
		return zero, fmt.Errorf("hexdb aircraft %d", code)
	}
	var ac hexAircraft
	if err := json.Unmarshal(body, &ac); err != nil {
		return zero, err
	}
	return ac, nil
}

func (e *Enricher) get(rawURL string) ([]byte, int, error) {
	req, err := newHTTPRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "wroclaw-sky/1.0 (+https://github.com/Dev0Pos/wroclaw-sky)")
	req.Header.Set("Accept", "application/json")
	resp, err := e.client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return body, resp.StatusCode, err
}
