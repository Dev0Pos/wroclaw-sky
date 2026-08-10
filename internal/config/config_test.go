package config_test

import (
	"testing"

	"wroclaw-sky/internal/config"
	"wroclaw-sky/internal/opensky"
)

func TestFromEnvDefaults(t *testing.T) {
	cfg, err := config.FromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8081" || cfg.BBox != opensky.Wroclaw {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.Focus.ICAO != "EPWR" {
		t.Fatalf("focus = %+v", cfg.Focus)
	}
	if cfg.MapLabel != "EPWR · Wrocław" {
		t.Fatalf("auto map label = %q", cfg.MapLabel)
	}
	if cfg.OpenSkyAuth() {
		t.Fatal("expected no auth")
	}
}

func TestFromEnvCustomBBoxAndTokens(t *testing.T) {
	env := map[string]string{
		"PORT":         "3000",
		"OPENSKY_BBOX": "52.00,20.70,52.50,21.30",
		"MAP_LABEL":    "EPWA · Warsaw",
		"FOCUS_ICAO":   "EPWA",
		"UPSTREAM_URL": "https://fetcher.example",
		"FETCH_TOKEN":  "secret",
		"OPENSKY_USER": "u",
		"OPENSKY_PASS": "p",
		"TRAILS_FILE":  "/tmp/trails.json",
	}
	cfg, err := config.FromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "3000" || cfg.MapLabel != "EPWA · Warsaw" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.Focus.ICAO != "EPWA" || cfg.TrailsFile != "/tmp/trails.json" {
		t.Fatalf("focus/trails = %+v", cfg)
	}
	if cfg.UpstreamToken != "secret" {
		t.Fatalf("token fallback = %q", cfg.UpstreamToken)
	}
	if !cfg.OpenSkyAuth() {
		t.Fatal("expected auth")
	}
	want, _ := opensky.ParseBBox(env["OPENSKY_BBOX"])
	if cfg.BBox != want {
		t.Fatalf("bbox = %+v", cfg.BBox)
	}
}

func TestFromEnvAutoBBoxAroundFocus(t *testing.T) {
	cfg, err := config.FromEnv(func(k string) string {
		if k == "FOCUS_ICAO" {
			return "EPWA"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.BBox.Contains(cfg.Focus.Lat, cfg.Focus.Lon) {
		t.Fatalf("auto bbox %+v missing focus", cfg.BBox)
	}
	if cfg.BBox == opensky.Wroclaw {
		t.Fatal("expected non-Wroclaw bbox for EPWA")
	}
}

func TestFromEnvFocusRadius(t *testing.T) {
	cfg, err := config.FromEnv(func(k string) string {
		switch k {
		case "FOCUS_ICAO":
			return "EPWR"
		case "FOCUS_RADIUS_KM":
			return "50"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.BBox.Contains(cfg.Focus.Lat, cfg.Focus.Lon) {
		t.Fatal("radius bbox")
	}
	_, err = config.FromEnv(func(k string) string {
		if k == "FOCUS_RADIUS_KM" {
			return "bad"
		}
		return ""
	})
	if err == nil {
		t.Fatal("bad radius")
	}
}

func TestFromEnvCustomFocusCoords(t *testing.T) {
	cfg, err := config.FromEnv(func(k string) string {
		switch k {
		case "FOCUS_ICAO":
			return "TEST"
		case "FOCUS_LAT":
			return "51.1"
		case "FOCUS_LON":
			return "17.0"
		case "FOCUS_CITY":
			return "Lab"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Focus.ICAO != "TEST" || cfg.MapLabel != "TEST · Lab" {
		t.Fatalf("%+v", cfg)
	}
}

func TestFromEnvBadFocus(t *testing.T) {
	_, err := config.FromEnv(func(k string) string {
		if k == "FOCUS_ICAO" {
			return "ZZZZ"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFromEnvBadBBox(t *testing.T) {
	_, err := config.FromEnv(func(k string) string {
		if k == "OPENSKY_BBOX" {
			return "bad"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFromEnvNilGetenv(t *testing.T) {
	if _, err := config.FromEnv(nil); err == nil {
		t.Fatal("expected error")
	}
}
