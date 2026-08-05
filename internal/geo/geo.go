package geo

import (
	"math"
)

// EPWR — Copernicus Wrocław Airport (approx. ARP).
const (
	EPWRLat = 51.1027
	EPWRLon = 16.8858
	// ApproachRadiusM — inbound EPWR flights closer than this are "on approach".
	ApproachRadiusM = 40000.0
)

const earthRadiusM = 6371000.0

// HaversineM returns great-circle distance in metres.
func HaversineM(lat1, lon1, lat2, lon2 float64) float64 {
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	Δφ := (lat2 - lat1) * math.Pi / 180
	Δλ := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(Δφ/2)*math.Sin(Δφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*math.Sin(Δλ/2)*math.Sin(Δλ/2)
	return 2 * earthRadiusM * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// DestinationPoint returns the point reached by travelling distM metres
// on initial bearingDeg (0 = north, clockwise).
func DestinationPoint(lat, lon, bearingDeg, distM float64) (float64, float64) {
	δ := distM / earthRadiusM
	θ := bearingDeg * math.Pi / 180
	φ1 := lat * math.Pi / 180
	λ1 := lon * math.Pi / 180
	sinφ1, cosφ1 := math.Sin(φ1), math.Cos(φ1)
	sinδ, cosδ := math.Sin(δ), math.Cos(δ)
	sinθ, cosθ := math.Sin(θ), math.Cos(θ)

	sinφ2 := sinφ1*cosδ + cosφ1*sinδ*cosθ
	φ2 := math.Asin(sinφ2)
	y := sinθ * sinδ * cosφ1
	x := cosδ - sinφ1*sinφ2
	λ2 := λ1 + math.Atan2(y, x)

	return φ2 * 180 / math.Pi, math.Mod(λ2*180/math.Pi+540, 360) - 180
}

// ETASeconds estimates time to cover distM at ground speed velocityMs.
// Returns 0 when speed is too low or distance invalid.
func ETASeconds(distM, velocityMs float64) int {
	if distM <= 0 || velocityMs < 5 {
		return 0
	}
	return int(math.Round(distM / velocityMs))
}

// FormatDistKm formats metres as "12 km" / "0.8 km".
func FormatDistKm(distM float64) string {
	if distM <= 0 {
		return "—"
	}
	km := distM / 1000
	if km < 10 {
		return trim1(km) + " km"
	}
	return itoa(int(math.Round(km))) + " km"
}

// FormatETA formats seconds as "~3m" / "~1h 12m".
func FormatETA(sec int) string {
	if sec <= 0 {
		return ""
	}
	if sec < 60 {
		return "~" + itoa(sec) + "s"
	}
	m := sec / 60
	if m < 60 {
		return "~" + itoa(m) + "m"
	}
	h := m / 60
	rm := m % 60
	if rm == 0 {
		return "~" + itoa(h) + "h"
	}
	return "~" + itoa(h) + "h " + itoa(rm) + "m"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func trim1(km float64) string {
	// one decimal without fmt to keep package light
	t := int(math.Round(km * 10))
	whole := t / 10
	frac := t % 10
	if frac < 0 {
		frac = -frac
	}
	return itoa(whole) + "." + itoa(frac)
}
