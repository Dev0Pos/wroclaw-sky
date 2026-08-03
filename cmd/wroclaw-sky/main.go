package main

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/logging"
	"wroclaw-sky/internal/meta"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

// Set via -ldflags "-X main.version=v1.2.3" on release builds.
var version = "dev"

func main() {
	slog.SetDefault(logging.NewFromEnv())

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	upstream := strings.TrimSpace(os.Getenv("UPSTREAM_URL"))
	auth := os.Getenv("OPENSKY_USER") != ""
	slog.Info("config",
		"version", version,
		"opensky_auth", auth,
		"upstream", upstream != "",
		"fetch_token", os.Getenv("FETCH_TOKEN") != "",
	)

	client := &opensky.Client{
		Username: os.Getenv("OPENSKY_USER"),
		Password: os.Getenv("OPENSKY_PASS"),
	}
	store := cache.New(client, opensky.Wroclaw)
	store.UpstreamURL = upstream
	store.UpstreamToken = os.Getenv("UPSTREAM_TOKEN")
	if store.UpstreamToken == "" {
		store.UpstreamToken = os.Getenv("FETCH_TOKEN")
	}

	enricher := meta.NewEnricher()
	// Cloud UI: pull route/type via fetcher (hexdb often blocked from Render).
	enricher.UpstreamURL = upstream
	enricher.UpstreamToken = store.UpstreamToken

	srv, err := server.New(store, enricher)
	if err != nil {
		slog.Error("server init", "err", err)
		os.Exit(1)
	}

	addr := ":" + port
	slog.Info("listening", "addr", addr, "url", "http://localhost"+addr)
	if err := http.ListenAndServe(addr, logging.AccessLog(srv.Handler())); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
