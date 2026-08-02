package meta

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:embed airports.json
var airportsJSON []byte

// Airport is a compact OpenFlights row used for route labels.
type Airport struct {
	Name    string `json:"name"`
	City    string `json:"city"`
	Country string `json:"country"`
	IATA    string `json:"iata"`
}

var (
	airportsOnce sync.Once
	airportsMap  map[string]Airport
)

func loadAirports() {
	airportsOnce.Do(func() {
		airportsMap = make(map[string]Airport)
		_ = json.Unmarshal(airportsJSON, &airportsMap)
	})
}

// LookupAirport returns airport metadata by ICAO code.
func LookupAirport(icao string) (Airport, bool) {
	loadAirports()
	a, ok := airportsMap[strings.ToUpper(strings.TrimSpace(icao))]
	return a, ok
}

// FormatAirport prefers "City (ICAO)" then name.
func FormatAirport(icao string) string {
	icao = strings.ToUpper(strings.TrimSpace(icao))
	if icao == "" {
		return ""
	}
	a, ok := LookupAirport(icao)
	if !ok {
		return icao
	}
	if a.City != "" {
		if a.IATA != "" {
			return a.City + " (" + a.IATA + "/" + icao + ")"
		}
		return a.City + " (" + icao + ")"
	}
	if a.Name != "" {
		return a.Name + " (" + icao + ")"
	}
	return icao
}
