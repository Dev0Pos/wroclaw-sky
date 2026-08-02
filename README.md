# wroclaw-sky

Aircraft around Wrocław (OpenSky Network) — Go + HTMX + Leaflet.

## Layout

```
cmd/wroclaw-sky/       # main()
internal/
  opensky/             # OpenSky client + Aircraft model
  cache/               # in-memory snapshot (refresh on demand)
  logging/             # slog + HTTP access log
  server/              # HTTP handlers + embedded templates
```

## Run

```bash
go test ./...
go run ./cmd/wroclaw-sky
```

Open http://localhost:8081 — click **Refresh from OpenSky** (no background polling).

```bash
LOG_FORMAT=json LOG_LEVEL=info go run ./cmd/wroclaw-sky
PORT=3000 go run ./cmd/wroclaw-sky
```

Optional OpenSky credentials (higher credit quota):

```bash
OPENSKY_USER=you OPENSKY_PASS=secret go run ./cmd/wroclaw-sky
```

## Docker

```bash
docker build -t wroclaw-sky .
docker run --rm -p 8081:8081 wroclaw-sky
```

Published images (on tag `v*`):

```bash
docker pull ghcr.io/dev0pos/wroclaw-sky:latest
```

## CI / release

GitHub Actions (same pattern as k8s-sigs-scout):

- **CI** on `main` / PRs: `go test`, build, golangci-lint, Docker build + **Trivy** (fail on any vuln)
- **Release** on tag `v*`: Trivy gate → push multi-arch image to **GHCR** + binary assets (linux/darwin amd64/arm64)

```bash
git tag v0.1.0
git push origin v0.1.0
```

## How it works

1. OpenSky is queried only when you click **Refresh from OpenSky** (~1 API credit for the Wrocław bbox).
2. HTMX swaps the flight list; the map reloads markers from `/api/aircraft` after the swap.
3. Logs are structured JSON by default (`LOG_FORMAT` / `LOG_LEVEL`); `/healthz` is omitted from access logs.

### Render / cloud hosts

OpenSky may block hyperscaler IPs. Run a **fetcher** on a normal host and set on Render:

```text
UPSTREAM_URL=https://your-fetcher.example
UPSTREAM_TOKEN=shared-secret
```

See [`deploy/fetcher/README.md`](deploy/fetcher/README.md).
