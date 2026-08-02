package main

import (
	"log/slog"
	"net/http"
	"os"

	"wroclaw-sky/internal/cache"
	"wroclaw-sky/internal/logging"
	"wroclaw-sky/internal/opensky"
	"wroclaw-sky/internal/server"
)

func main() {
	slog.SetDefault(logging.NewFromEnv())

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	auth := os.Getenv("OPENSKY_USER") != ""
	slog.Info("opensky auth", "enabled", auth)

	client := &opensky.Client{
		Username: os.Getenv("OPENSKY_USER"),
		Password: os.Getenv("OPENSKY_PASS"),
	}
	store := cache.New(client, opensky.Wroclaw)

	srv, err := server.New(store)
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
