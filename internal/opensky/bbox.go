package opensky

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseBBox parses "lamin,lomin,lamax,lomax" (decimal degrees).
func ParseBBox(s string) (BBox, error) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 4 {
		return BBox{}, fmt.Errorf("bbox want lamin,lomin,lamax,lomax got %q", s)
	}
	vals := make([]float64, 4)
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return BBox{}, fmt.Errorf("bbox field %d: %w", i, err)
		}
		vals[i] = v
	}
	b := BBox{LaMin: vals[0], LoMin: vals[1], LaMax: vals[2], LoMax: vals[3]}
	if b.LaMin >= b.LaMax || b.LoMin >= b.LoMax {
		return BBox{}, fmt.Errorf("bbox min must be < max: %+v", b)
	}
	if b.LaMin < -90 || b.LaMax > 90 || b.LoMin < -180 || b.LoMax > 180 {
		return BBox{}, fmt.Errorf("bbox out of range: %+v", b)
	}
	return b, nil
}

// Center returns the geographic centre of the bbox.
func (b BBox) Center() (lat, lon float64) {
	return (b.LaMin + b.LaMax) / 2, (b.LoMin + b.LoMax) / 2
}

// Contains reports whether lat/lon is inside the bbox (inclusive).
func (b BBox) Contains(lat, lon float64) bool {
	return lat >= b.LaMin && lat <= b.LaMax && lon >= b.LoMin && lon <= b.LoMax
}
