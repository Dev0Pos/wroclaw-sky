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
	if cfg.OpenSkyAuth() {
		t.Fatal("expected no auth")
	}
}

func TestFromEnvCustomBBoxAndTokens(t *testing.T) {
	env := map[string]string{
		"PORT":           "3000",
		"OPENSKY_BBOX":   "52.00,20.70,52.50,21.30",
		"MAP_LABEL":      "EPWA · Warsaw",
		"UPSTREAM_URL":   "https://fetcher.example",
		"FETCH_TOKEN":    "secret",
		"OPENSKY_USER":   "u",
		"OPENSKY_PASS":   "p",
	}
	cfg, err := config.FromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "3000" || cfg.MapLabel != "EPWA · Warsaw" {
		t.Fatalf("cfg = %+v", cfg)
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
