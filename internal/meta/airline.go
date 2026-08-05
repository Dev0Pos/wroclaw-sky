package meta

import (
	"sort"
	"strings"
	"unicode"
)

// Common ICAO airline designators → short display name.
var airlineNames = map[string]string{
	"LOT": "LOT",
	"RYR": "Ryanair",
	"WZZ": "Wizz Air",
	"ENT": "Enter Air",
	"AUA": "Austrian",
	"DLH": "Lufthansa",
	"EZY": "easyJet",
	"EJU": "easyJet",
	"BAW": "British Airways",
	"AFR": "Air France",
	"KLM": "KLM",
	"SAS": "SAS",
	"NAX": "Norwegian",
	"SWR": "Swiss",
	"THY": "Turkish",
	"UAE": "Emirates",
	"QTR": "Qatar",
	"ACA": "Air Canada",
	"AAL": "American",
	"UAL": "United",
	"DAL": "Delta",
	"VLG": "Vueling",
	"BER": "Air Berlin",
	"GWI": "Germanwings",
	"EWG": "Eurowings",
	"CFG": "Condor",
	"TRA": "Transavia",
	"TVF": "Transavia",
	"BEL": "Brussels",
	"FIN": "Finnair",
	"ICE": "Icelandair",
	"CSA": "Czech Airlines",
	"OKA": "Czech Airlines",
	"SPP": "SprintAir",
	"CLW": "Buzz",
	"BZY": "Buzz",
}

// AirlineFromCallsign extracts a 3-letter ICAO airline code and display name.
// Returns empty strings when the callsign does not look like a scheduled flight.
func AirlineFromCallsign(callsign string) (icao, name string) {
	cs := strings.ToUpper(strings.TrimSpace(callsign))
	cs = strings.ReplaceAll(cs, " ", "")
	if len(cs) < 3 {
		return "", ""
	}
	// Skip pure hex / ICAO24-looking tokens.
	if looksLikeHex(cs) {
		return "", ""
	}
	prefix := cs[:3]
	for _, r := range prefix {
		if !unicode.IsLetter(r) {
			return "", ""
		}
	}
	// Remaining should start with a digit for a typical flight number.
	rest := cs[3:]
	if rest == "" {
		return "", ""
	}
	hasDigit := false
	for _, r := range rest {
		if unicode.IsDigit(r) {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return "", ""
	}
	name = airlineNames[prefix]
	if name == "" {
		name = prefix
	}
	return prefix, name
}

// AirlineHint is a compact label for the flight list (e.g. "LOT" / "Ryanair").
func AirlineHint(callsign string) string {
	_, name := AirlineFromCallsign(callsign)
	return name
}

// AirlineOptions returns sorted unique display names for the airline filter.
func AirlineOptions() []string {
	seen := make(map[string]struct{}, len(airlineNames))
	out := make([]string, 0, len(airlineNames))
	for _, name := range airlineNames {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func looksLikeHex(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}
