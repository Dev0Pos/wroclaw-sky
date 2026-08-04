package main

import (
	"log/slog"
	"net/http"
	"os"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/config"
	"wroclaw-sky/internal/logging"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

// Set via -ldflags "-X main.version=v1.2.3" on release builds.
var version = "dev"

func main() {
	slog.SetDefault(logging.NewFromEnv())

	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	clat, clon := cfg.BBox.Center()
	slog.Info("config",
		"version", version,
		"opensky_auth", cfg.OpenSkyAuth(),
		"upstream", cfg.UpstreamURL != "",
		"fetch_token", cfg.FetchToken != "",
		"bbox", cfg.BBox,
		"center", []float64{clat, clon},
	)

	client := &opensky.Client{
		Username: cfg.OpenSkyUser,
		Password: cfg.OpenSkyPass,
	}
	store := cache.New(client, cfg.BBox)
	store.UpstreamURL = cfg.UpstreamURL
	store.UpstreamToken = cfg.UpstreamToken

	enricher := meta.NewEnricher()
	// Cloud UI: pull route/type via fetcher (hexdb often blocked from Render).
	enricher.UpstreamURL = cfg.UpstreamURL
	enricher.UpstreamToken = cfg.UpstreamToken

	srv, err := server.New(store, enricher)
	if err != nil {
		slog.Error("server init", "err", err)
		os.Exit(1)
	}
	if cfg.MapLabel != "" {
		srv.SetMapLabel(cfg.MapLabel)
	}

	addr := ":" + cfg.Port
	slog.Info("listening", "addr", addr, "url", "http://localhost"+addr)
	if err := http.ListenAndServe(addr, logging.AccessLog(srv.Handler())); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
