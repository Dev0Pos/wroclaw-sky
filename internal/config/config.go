package config

import (
	"fmt"
	"strconv"
	"strings"

	"wroclaw-sky/internal/geo"
	"wroclaw-sky/internal/opensky"
)

// App holds process configuration loaded from the environment.
type App struct {
	Port          string
	BBox          opensky.BBox
	MapLabel      string
	Focus         geo.Focus
	TrailsFile    string
	UpstreamURL   string
	UpstreamToken string
	FetchToken    string
	OpenSkyUser   string
	OpenSkyPass   string
}

// FromEnv loads configuration. getenv is typically os.Getenv (injectable in tests).
func FromEnv(getenv func(string) string) (App, error) {
	if getenv == nil {
		return App{}, fmt.Errorf("getenv required")
	}
	cfg := App{
		Port:          strings.TrimSpace(getenv("PORT")),
		MapLabel:      strings.TrimSpace(getenv("MAP_LABEL")),
		TrailsFile:    strings.TrimSpace(getenv("TRAILS_FILE")),
		UpstreamURL:   strings.TrimSpace(getenv("UPSTREAM_URL")),
		UpstreamToken: strings.TrimSpace(getenv("UPSTREAM_TOKEN")),
		FetchToken:    strings.TrimSpace(getenv("FETCH_TOKEN")),
		OpenSkyUser:   getenv("OPENSKY_USER"),
		OpenSkyPass:   getenv("OPENSKY_PASS"),
		BBox:          opensky.Wroclaw,
		Focus:         geo.DefaultFocus(),
	}
	if cfg.Port == "" {
		cfg.Port = "8081"
	}
	if cfg.UpstreamToken == "" {
		cfg.UpstreamToken = cfg.FetchToken
	}

	focus, err := geo.ResolveFocus(
		getenv("FOCUS_ICAO"),
		getenv("FOCUS_LAT"),
		getenv("FOCUS_LON"),
		getenv("FOCUS_CITY"),
	)
	if err != nil {
		return App{}, err
	}
	cfg.Focus = focus
	if cfg.MapLabel == "" {
		cfg.MapLabel = focus.Label()
	}

	if raw := strings.TrimSpace(getenv("OPENSKY_BBOX")); raw != "" {
		bbox, err := opensky.ParseBBox(raw)
		if err != nil {
			return App{}, fmt.Errorf("OPENSKY_BBOX: %w", err)
		}
		cfg.BBox = bbox
	} else {
		radiusKm := 0.0
		if r := strings.TrimSpace(getenv("FOCUS_RADIUS_KM")); r != "" {
			v, err := strconv.ParseFloat(r, 64)
			if err != nil || v <= 0 {
				return App{}, fmt.Errorf("FOCUS_RADIUS_KM: invalid %q", r)
			}
			radiusKm = v
		} else if focus.ICAO != "EPWR" {
			radiusKm = 80
		}
		if radiusKm > 0 {
			cfg.BBox = opensky.BBoxAround(focus.Lat, focus.Lon, radiusKm)
		}
	}
	return cfg, nil
}

// OpenSkyAuth reports whether OpenSky basic auth credentials are set.
func (c App) OpenSkyAuth() bool {
	return strings.TrimSpace(c.OpenSkyUser) != ""
}
