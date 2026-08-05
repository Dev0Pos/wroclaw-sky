package config

import (
	"fmt"
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
	if raw := strings.TrimSpace(getenv("OPENSKY_BBOX")); raw != "" {
		bbox, err := opensky.ParseBBox(raw)
		if err != nil {
			return App{}, fmt.Errorf("OPENSKY_BBOX: %w", err)
		}
		cfg.BBox = bbox
	}
	focus, err := geo.ParseFocus(getenv("FOCUS_ICAO"))
	if err != nil {
		return App{}, err
	}
	cfg.Focus = focus
	return cfg, nil
}

// OpenSkyAuth reports whether OpenSky basic auth credentials are set.
func (c App) OpenSkyAuth() bool {
	return strings.TrimSpace(c.OpenSkyUser) != ""
}
