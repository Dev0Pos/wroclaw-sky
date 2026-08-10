package cache

// PositionAt returns a linearly interpolated position along pts at unix time at.
// ok is false when pts is empty or at is before the first sample.
func PositionAt(pts []Point, at int64) (lat, lon float64, ok bool) {
	if len(pts) == 0 {
		return 0, 0, false
	}
	if len(pts) == 1 || at <= pts[0].At {
		p := pts[0]
		if at < p.At && len(pts) > 1 {
			return 0, 0, false
		}
		return p.Lat, p.Lon, true
	}
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if at > b.At {
			continue
		}
		t := float64(at-a.At) / float64(b.At-a.At)
		return a.Lat + (b.Lat-a.Lat)*t, a.Lon + (b.Lon-a.Lon)*t, true
	}
	last := pts[len(pts)-1]
	return last.Lat, last.Lon, true
}

// TrailTimeRange returns min/max At across all trails (0,0 if none).
func TrailTimeRange(trails map[string][]Point) (minAt, maxAt int64) {
	first := true
	for _, pts := range trails {
		for _, p := range pts {
			if p.At == 0 {
				continue
			}
			if first {
				minAt, maxAt = p.At, p.At
				first = false
				continue
			}
			if p.At < minAt {
				minAt = p.At
			}
			if p.At > maxAt {
				maxAt = p.At
			}
		}
	}
	return minAt, maxAt
}
