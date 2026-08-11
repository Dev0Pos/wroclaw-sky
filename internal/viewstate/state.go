// Package viewstate holds shareable map/list UI state (URL query params).
package viewstate

import (
	"net/url"
	"sort"
	"strings"
)

// Allowed query values.
const (
	AltAny  = "any"
	AltLow  = "low"
	AltMid  = "mid"
	AltHigh = "high"

	EPWRAny    = "any"
	EPWRTo     = "to"
	EPWRFrom   = "from"
	EPWREither = "either"

	SortCallsign = "callsign"
	SortAlt      = "alt"
	SortSpeed    = "speed"
	SortEPWR     = "epwr"
)

// State is the shareable view (filters, sort, Live, Follow, approach alerts).
type State struct {
	Q        string
	Airborne bool
	Alt      string
	EPWR     string
	Sort     string
	Airline  string
	Live     bool
	Follow   bool
	Alert    bool
	ICAO     string
	Focus    string // FOCUS_ICAO override via share URL
}

// Default returns a fresh state with Follow on.
func Default() State {
	return State{
		Alt:    AltAny,
		EPWR:   EPWRAny,
		Sort:   SortCallsign,
		Follow: true,
	}
}

// Parse reads state from URL query values. Unknown values fall back to defaults.
// Follow defaults to true when the param is absent; set follow=0 to disable.
func Parse(q url.Values) State {
	s := Default()
	if q == nil {
		return s
	}
	s.Q = strings.TrimSpace(q.Get("q"))
	s.Airborne = truthy(q.Get("airborne"))
	s.Alt = oneOf(q.Get("alt"), AltAny, AltLow, AltMid, AltHigh)
	s.EPWR = oneOf(q.Get("epwr"), EPWRAny, EPWRTo, EPWRFrom, EPWREither)
	s.Sort = oneOf(q.Get("sort"), SortCallsign, SortAlt, SortSpeed, SortEPWR)
	s.Airline = strings.TrimSpace(q.Get("airline"))
	s.Live = truthy(q.Get("live"))
	if _, ok := q["follow"]; ok {
		s.Follow = truthy(q.Get("follow"))
	}
	s.Alert = truthy(q.Get("alert"))
	s.ICAO = strings.ToLower(strings.TrimSpace(q.Get("icao")))
	s.Focus = strings.ToUpper(strings.TrimSpace(q.Get("focus")))
	return s
}

// Encode returns a query string without leading '?', omitting default values
// so shared links stay short. ICAO and non-default filters are always kept.
func (s State) Encode() string {
	v := url.Values{}
	if s.Q != "" {
		v.Set("q", s.Q)
	}
	if s.Airborne {
		v.Set("airborne", "1")
	}
	if s.Alt != "" && s.Alt != AltAny {
		v.Set("alt", s.Alt)
	}
	if s.EPWR != "" && s.EPWR != EPWRAny {
		v.Set("epwr", s.EPWR)
	}
	if s.Sort != "" && s.Sort != SortCallsign {
		v.Set("sort", s.Sort)
	}
	if s.Airline != "" && s.Airline != "any" {
		v.Set("airline", s.Airline)
	}
	if s.Live {
		v.Set("live", "1")
	}
	if !s.Follow {
		v.Set("follow", "0")
	}
	if s.Alert {
		v.Set("alert", "1")
	}
	if s.ICAO != "" {
		v.Set("icao", s.ICAO)
	}
	if s.Focus != "" {
		v.Set("focus", s.Focus)
	}
	return v.Encode()
}

// NewlyOnApproach returns ICAO24s that flipped from not-approach to approach.
func NewlyOnApproach(prev, curr map[string]bool) []string {
	var out []string
	for id, on := range curr {
		if !on {
			continue
		}
		if prev[id] {
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func oneOf(v string, allowed ...string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return allowed[0]
	}
	for _, a := range allowed {
		if v == a {
			return a
		}
	}
	return allowed[0]
}
