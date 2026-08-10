package geo

import (
	"fmt"
	"strconv"
	"strings"
)

// Focus is the airport used for arrivals, approach, ETA, and map circle.
type Focus struct {
	ICAO string
	Lat  float64
	Lon  float64
	City string
}

// DefaultFocus is Copernicus Wrocław Airport.
func DefaultFocus() Focus {
	return Focus{ICAO: "EPWR", Lat: EPWRLat, Lon: EPWRLon, City: "Wrocław"}
}

// Label returns "ICAO · City" (or just ICAO).
func (f Focus) Label() string {
	if strings.TrimSpace(f.City) != "" {
		return f.ICAO + " · " + f.City
	}
	return f.ICAO
}

// known ARP positions for FOCUS_ICAO (decimal degrees).
var knownFocus = map[string]Focus{
	"EPWR": {ICAO: "EPWR", Lat: 51.1027, Lon: 16.8858, City: "Wrocław"},
	"EPWA": {ICAO: "EPWA", Lat: 52.1657, Lon: 20.9671, City: "Warsaw"},
	"EPKK": {ICAO: "EPKK", Lat: 50.0777, Lon: 19.7848, City: "Kraków"},
	"EPGD": {ICAO: "EPGD", Lat: 54.3776, Lon: 18.4662, City: "Gdańsk"},
	"EPKT": {ICAO: "EPKT", Lat: 50.4743, Lon: 19.0800, City: "Katowice"},
	"EPPO": {ICAO: "EPPO", Lat: 52.4210, Lon: 16.8263, City: "Poznań"},
	"EPRZ": {ICAO: "EPRZ", Lat: 50.1100, Lon: 22.0190, City: "Rzeszów"},
	"EPSC": {ICAO: "EPSC", Lat: 53.5847, Lon: 14.9023, City: "Szczecin"},
	"EDDF": {ICAO: "EDDF", Lat: 50.0379, Lon: 8.5622, City: "Frankfurt"},
	"EDDM": {ICAO: "EDDM", Lat: 48.3538, Lon: 11.7861, City: "Munich"},
	"LKPR": {ICAO: "LKPR", Lat: 50.1008, Lon: 14.2600, City: "Prague"},
	"LOWW": {ICAO: "LOWW", Lat: 48.1103, Lon: 16.5697, City: "Vienna"},
}

// LookupFocus returns a known focus airport by ICAO (empty → default EPWR).
func LookupFocus(icao string) (Focus, bool) {
	icao = strings.ToUpper(strings.TrimSpace(icao))
	if icao == "" {
		return DefaultFocus(), true
	}
	f, ok := knownFocus[icao]
	return f, ok
}

// ParseFocus resolves FOCUS_ICAO. Empty string → EPWR. Unknown ICAO → error.
func ParseFocus(icao string) (Focus, error) {
	icao = strings.ToUpper(strings.TrimSpace(icao))
	if icao == "" {
		return DefaultFocus(), nil
	}
	f, ok := knownFocus[icao]
	if !ok {
		return Focus{}, fmt.Errorf("unknown FOCUS_ICAO %q", icao)
	}
	return f, nil
}

// ResolveFocus builds a Focus from ICAO plus optional lat/lon/city overrides.
// Unknown ICAO is allowed when both lat and lon are provided (custom ARP).
func ResolveFocus(icao, latStr, lonStr, city string) (Focus, error) {
	icao = strings.ToUpper(strings.TrimSpace(icao))
	latStr = strings.TrimSpace(latStr)
	lonStr = strings.TrimSpace(lonStr)
	city = strings.TrimSpace(city)

	var f Focus
	switch icao {
	case "":
		f = DefaultFocus()
	default:
		if known, ok := knownFocus[icao]; ok {
			f = known
		} else if latStr != "" && lonStr != "" {
			f = Focus{ICAO: icao}
		} else {
			return Focus{}, fmt.Errorf("unknown FOCUS_ICAO %q (set FOCUS_LAT/FOCUS_LON for custom)", icao)
		}
	}
	if latStr != "" {
		lat, err := strconv.ParseFloat(latStr, 64)
		if err != nil || lat < -90 || lat > 90 {
			return Focus{}, fmt.Errorf("FOCUS_LAT: invalid %q", latStr)
		}
		f.Lat = lat
	}
	if lonStr != "" {
		lon, err := strconv.ParseFloat(lonStr, 64)
		if err != nil || lon < -180 || lon > 180 {
			return Focus{}, fmt.Errorf("FOCUS_LON: invalid %q", lonStr)
		}
		f.Lon = lon
	}
	if city != "" {
		f.City = city
	}
	if f.Lat == 0 && f.Lon == 0 {
		return Focus{}, fmt.Errorf("focus coordinates required")
	}
	return f, nil
}

// KnownFocusICAOs returns sorted ICAO codes available for FOCUS_ICAO.
func KnownFocusICAOs() []string {
	out := make([]string, 0, len(knownFocus))
	for k := range knownFocus {
		out = append(out, k)
	}
	// manual insert sort to avoid importing sort in hot path — use sort.Strings
	return sortedCopy(out)
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j-1] > out[j] {
			out[j-1], out[j] = out[j], out[j-1]
			j--
		}
	}
	return out
}

// OnApproachTo reports whether dest matches focus and position is within ApproachRadiusM.
func OnApproachTo(focus Focus, dest string, lat, lon float64, onGround bool) bool {
	if onGround || !strings.EqualFold(strings.TrimSpace(dest), focus.ICAO) {
		return false
	}
	if lat == 0 && lon == 0 {
		return false
	}
	return HaversineM(lat, lon, focus.Lat, focus.Lon) <= ApproachRadiusM
}

// OnApproach is OnApproachTo for the default EPWR focus (compat).
func OnApproach(dest string, lat, lon float64, onGround bool) bool {
	return OnApproachTo(DefaultFocus(), dest, lat, lon, onGround)
}

// FormatFocusHint shows distance · ETA for flights inbound to focus.
func FormatFocusHint(focus Focus, dest string, lat, lon, velocity float64, onGround bool) string {
	if onGround || !strings.EqualFold(strings.TrimSpace(dest), focus.ICAO) {
		return ""
	}
	if lat == 0 && lon == 0 {
		return ""
	}
	dist := HaversineM(lat, lon, focus.Lat, focus.Lon)
	out := FormatDistKm(dist)
	if eta := FormatETA(ETASeconds(dist, velocity)); eta != "" {
		out += " · " + eta
	}
	return out
}
