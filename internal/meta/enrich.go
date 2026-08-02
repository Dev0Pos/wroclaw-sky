package meta

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

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

// Enricher loads aircraft/route metadata (hexdb) with a small TTL cache.
type Enricher struct {
	HTTP    *http.Client
	BaseURL string

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	at   time.Time
	data Detail
}

const cacheTTL = 30 * time.Minute

func NewEnricher() *Enricher {
	return &Enricher{
		HTTP:    &http.Client{Timeout: 8 * time.Second},
		BaseURL: "https://hexdb.io",
		cache:   make(map[string]cacheEntry),
	}
}

func (e *Enricher) base() string {
	if e.BaseURL != "" {
		return strings.TrimRight(e.BaseURL, "/")
	}
	return "https://hexdb.io"
}

// Enrich fills registration/type/route fields. Safe if hexdb is down.
func (e *Enricher) Enrich(d Detail) Detail {
	key := strings.ToLower(d.ICAO24) + "|" + strings.ToUpper(strings.TrimSpace(d.Callsign))
	e.mu.Lock()
	if ent, ok := e.cache[key]; ok && time.Since(ent.at) < cacheTTL {
		cached := ent.data
		e.mu.Unlock()
		// Prefer live kinematics from the caller.
		cached.Lon, cached.Lat = d.Lon, d.Lat
		cached.AltitudeM, cached.Velocity = d.AltitudeM, d.Velocity
		cached.Track, cached.Vertical = d.Track, d.Vertical
		cached.OnGround, cached.Country = d.OnGround, d.Country
		cached.Callsign = d.Callsign
		return cached
	}
	e.mu.Unlock()

	out := d
	if ac, err := e.fetchAircraft(d.ICAO24); err == nil {
		out.Registration = ac.Registration
		out.TypeCode = ac.ICAOTypeCode
		out.TypeName = ac.Type
		out.Manufacturer = ac.Manufacturer
		out.Operator = ac.RegisteredOwners
	} else if err != nil {
		out.Error = err.Error()
	}

	callsign := strings.TrimSpace(d.Callsign)
	if callsign != "" && !strings.EqualFold(callsign, d.ICAO24) {
		if route, err := e.fetchRoute(callsign); err == nil && route.Route != "" {
			out.Route = route.Route
			out.RouteSource = "hexdb"
			origin, dest := splitRoute(route.Route)
			out.Origin, out.Destination = origin, dest
			if a, ok := LookupAirport(origin); ok {
				out.OriginName = a.Name
				out.OriginCity = a.City
			}
			if a, ok := LookupAirport(dest); ok {
				out.DestName = a.Name
				out.DestCity = a.City
			}
		}
	}

	e.mu.Lock()
	e.cache[key] = cacheEntry{at: time.Now(), data: out}
	e.mu.Unlock()
	return out
}

type hexAircraft struct {
	Registration    string `json:"Registration"`
	Manufacturer    string `json:"Manufacturer"`
	ICAOTypeCode    string `json:"ICAOTypeCode"`
	Type            string `json:"Type"`
	RegisteredOwners string `json:"RegisteredOwners"`
}

type hexRoute struct {
	Flight string `json:"flight"`
	Route  string `json:"route"`
	Error  string `json:"error"`
	Status string `json:"status"`
}

func (e *Enricher) fetchAircraft(icao string) (hexAircraft, error) {
	icao = strings.ToLower(strings.TrimSpace(icao))
	var zero hexAircraft
	if icao == "" {
		return zero, fmt.Errorf("empty icao")
	}
	body, code, err := e.get(e.base() + "/api/v1/aircraft/" + icao)
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

func (e *Enricher) fetchRoute(callsign string) (hexRoute, error) {
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	var zero hexRoute
	body, code, err := e.get(e.base() + "/api/v1/route/icao/" + urlPath(callsign))
	if err != nil {
		return zero, err
	}
	if code == http.StatusNotFound {
		return zero, fmt.Errorf("route not found")
	}
	if code != http.StatusOK {
		return zero, fmt.Errorf("hexdb route %d", code)
	}
	var route hexRoute
	if err := json.Unmarshal(body, &route); err != nil {
		return zero, err
	}
	if route.Error != "" || route.Status == "404" {
		return zero, fmt.Errorf("route not found")
	}
	return route, nil
}

func (e *Enricher) get(url string) ([]byte, int, error) {
	client := e.HTTP
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "wroclaw-sky/1.0 (+https://github.com/Dev0Pos/wroclaw-sky)")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return body, resp.StatusCode, err
}

func splitRoute(route string) (string, string) {
	route = strings.ToUpper(strings.TrimSpace(route))
	parts := strings.Split(route, "-")
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func urlPath(s string) string {
	return strings.ReplaceAll(s, " ", "")
}
