package main

import (
	"io"
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

// Overridable in tests.
var (
	getenv                   = os.Getenv
	listenAndServe           = http.ListenAndServe
	stderr         io.Writer = os.Stderr
	exitFunc                 = os.Exit
	newServer                = server.New
)

func main() {
	exitFunc(run())
}

func run() int {
	slog.SetDefault(logging.NewFromEnv())

	cfg, err := config.FromEnv(getenv)
	if err != nil {
		slog.Error("config", "err", err)
		return 1
	}

	clat, clon := cfg.BBox.Center()
	slog.Info("config",
		"version", version,
		"opensky_auth", cfg.OpenSkyAuth(),
		"upstream", cfg.UpstreamURL != "",
		"fetch_token", cfg.FetchToken != "",
		"bbox", cfg.BBox,
		"focus", cfg.Focus.ICAO,
		"center", []float64{clat, clon},
	)

	client := &opensky.Client{
		Username: cfg.OpenSkyUser,
		Password: cfg.OpenSkyPass,
	}
	store := cache.New(client, cfg.BBox)
	store.UpstreamURL = cfg.UpstreamURL
	store.UpstreamToken = cfg.UpstreamToken
	if cfg.TrailsFile != "" {
		if err := store.SetTrailsFile(cfg.TrailsFile); err != nil {
			slog.Warn("trails file", "path", cfg.TrailsFile, "err", err)
		}
	}

	enricher := meta.NewEnricher()
	enricher.UpstreamURL = cfg.UpstreamURL
	enricher.UpstreamToken = cfg.UpstreamToken

	srv, err := newServer(store, enricher)
	if err != nil {
		slog.Error("server init", "err", err)
		return 1
	}
	srv.SetFocus(cfg.Focus)
	if cfg.MapLabel != "" {
		srv.SetMapLabel(cfg.MapLabel)
	}

	addr := ":" + cfg.Port
	slog.Info("listening", "addr", addr, "url", "http://localhost"+addr)
	if err := listenAndServe(addr, logging.AccessLog(srv.Handler())); err != nil {
		slog.Error("server stopped", "err", err)
		_, _ = io.WriteString(stderr, err.Error()+"\n")
		return 1
	}
	return 0
}
