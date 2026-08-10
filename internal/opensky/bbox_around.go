package opensky

import "math"

// BBoxAround returns a square-ish bbox of roughly radiusKm around lat/lon.
func BBoxAround(lat, lon, radiusKm float64) BBox {
	if radiusKm <= 0 {
		radiusKm = 80
	}
	dLat := radiusKm / 111.0
	cosLat := math.Cos(lat * math.Pi / 180)
	if cosLat < 0.2 {
		cosLat = 0.2
	}
	dLon := radiusKm / (111.0 * cosLat)
	b := BBox{
		LaMin: lat - dLat,
		LoMin: lon - dLon,
		LaMax: lat + dLat,
		LoMax: lon + dLon,
	}
	if b.LaMin < -90 {
		b.LaMin = -90
	}
	if b.LaMax > 90 {
		b.LaMax = 90
	}
	if b.LoMin < -180 {
		b.LoMin = -180
	}
	if b.LoMax > 180 {
		b.LoMax = 180
	}
	return b
}
