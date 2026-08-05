package server

import (
	"sort"
	"strings"

	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/meta"
)

// arrivalRow is an inbound EPWR flight for the arrivals board.
type arrivalRow struct {
	ICAO24   string
	Callsign string
	Origin   string
	Airline  string
	DistM    float64
	ETASec   int
	Hint     string
	Approach bool
}

// buildArrivals returns airborne EPWR-bound flights sorted by ETA then distance.
func buildArrivals(rows []flightRow) []arrivalRow {
	out := make([]arrivalRow, 0)
	for _, r := range rows {
		if r.OnGround || !strings.EqualFold(strings.TrimSpace(r.Destination), "EPWR") {
			continue
		}
		if r.Lat == 0 && r.Lon == 0 {
			continue
		}
		dist := geo.HaversineM(r.Lat, r.Lon, geo.EPWRLat, geo.EPWRLon)
		eta := geo.ETASeconds(dist, r.Velocity)
		hint := geo.FormatDistKm(dist)
		if e := geo.FormatETA(eta); e != "" {
			hint += " · " + e
		}
		out = append(out, arrivalRow{
			ICAO24:   r.ICAO24,
			Callsign: r.Callsign,
			Origin:   r.Origin,
			Airline:  meta.AirlineHint(r.Callsign),
			DistM:    dist,
			ETASec:   eta,
			Hint:     hint,
			Approach: geo.OnApproach(r.Destination, r.Lat, r.Lon, r.OnGround),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aj := out[i], out[j]
		// ETA 0 (unknown) sorts last.
		if (ai.ETASec == 0) != (aj.ETASec == 0) {
			return ai.ETASec != 0
		}
		if ai.ETASec != aj.ETASec {
			return ai.ETASec < aj.ETASec
		}
		if ai.DistM != aj.DistM {
			return ai.DistM < aj.DistM
		}
		return ai.Callsign < aj.Callsign
	})
	return out
}
