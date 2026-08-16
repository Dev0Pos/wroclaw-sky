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
	Port             string
	BBox             opensky.BBox
	MapLabel         string
	Focus            geo.Focus
	FocusRadiusKM    float64
	TrailsFile       string
	TrailsDB         string
	TrailsRedisURL   string
	UpstreamURL      string
	UpstreamToken    string
	FetchToken       string
	LiveToken        string
	LiveCookieHours  float64
	LiveAuthRPM      int
	AlertWebhookURL  string
	ApproachRadiusKM float64
	LowPassAltM      float64
	OpenSkyUser      string
	OpenSkyPass      string
}

// FromEnv loads configuration. getenv is typically os.Getenv (injectable in tests).
func FromEnv(getenv func(string) string) (App, error) {
	if getenv == nil {
		return App{}, fmt.Errorf("getenv required")
	}
	cfg := App{
		Port:             strings.TrimSpace(getenv("PORT")),
		MapLabel:         strings.TrimSpace(getenv("MAP_LABEL")),
		TrailsFile:       strings.TrimSpace(getenv("TRAILS_FILE")),
		TrailsDB:         strings.TrimSpace(getenv("TRAILS_DB")),
		TrailsRedisURL:   strings.TrimSpace(getenv("TRAILS_REDIS_URL")),
		UpstreamURL:      strings.TrimSpace(getenv("UPSTREAM_URL")),
		UpstreamToken:    strings.TrimSpace(getenv("UPSTREAM_TOKEN")),
		FetchToken:       strings.TrimSpace(getenv("FETCH_TOKEN")),
		LiveToken:        strings.TrimSpace(getenv("LIVE_TOKEN")),
		AlertWebhookURL:  strings.TrimSpace(getenv("ALERT_WEBHOOK_URL")),
		OpenSkyUser:      getenv("OPENSKY_USER"),
		OpenSkyPass:      getenv("OPENSKY_PASS"),
		BBox:             opensky.Wroclaw,
		Focus:            geo.DefaultFocus(),
		ApproachRadiusKM: 40,
	}
	if cfg.Port == "" {
		cfg.Port = "8081"
	}
	if cfg.UpstreamToken == "" {
		cfg.UpstreamToken = cfg.FetchToken
	}
	// LIVE_TOKEN falls back to FETCH_TOKEN when unset (same private deploy secret).
	if cfg.LiveToken == "" {
		cfg.LiveToken = cfg.FetchToken
	}
	cfg.LiveCookieHours = 8
	if raw := strings.TrimSpace(getenv("LIVE_COOKIE_HOURS")); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v <= 0 {
			return App{}, fmt.Errorf("LIVE_COOKIE_HOURS: invalid %q", raw)
		}
		cfg.LiveCookieHours = v
	}
	cfg.LiveAuthRPM = 10
	if raw := strings.TrimSpace(getenv("LIVE_AUTH_RPM")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			return App{}, fmt.Errorf("LIVE_AUTH_RPM: invalid %q", raw)
		}
		cfg.LiveAuthRPM = v
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

	if raw := strings.TrimSpace(getenv("APPROACH_RADIUS_KM")); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v <= 0 {
			return App{}, fmt.Errorf("APPROACH_RADIUS_KM: invalid %q", raw)
		}
		cfg.ApproachRadiusKM = v
	}
	if raw := strings.TrimSpace(getenv("LOW_PASS_ALT_M")); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 {
			return App{}, fmt.Errorf("LOW_PASS_ALT_M: invalid %q", raw)
		}
		cfg.LowPassAltM = v
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
		cfg.FocusRadiusKM = radiusKm
		if radiusKm > 0 {
			cfg.BBox = opensky.BBoxAround(focus.Lat, focus.Lon, radiusKm)
		}
	}
	if cfg.FocusRadiusKM <= 0 {
		cfg.FocusRadiusKM = 80
	}
	return cfg, nil
}

// OpenSkyAuth reports whether OpenSky basic auth credentials are set.
func (c App) OpenSkyAuth() bool {
	return strings.TrimSpace(c.OpenSkyUser) != ""
}

// ApproachRadiusM returns approach radius in metres.
func (c App) ApproachRadiusM() float64 {
	if c.ApproachRadiusKM <= 0 {
		return geo.ApproachRadiusM
	}
	return c.ApproachRadiusKM * 1000
}
