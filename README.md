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

1. OpenSky is queried when you click **Refresh** (~1 API credit for the Wrocław bbox), or via the shared **Live** poller (one server-side fetch every 45s for all Live viewers; clients heartbeat `POST /api/live`).
2. HTMX swaps the flight list; the map reloads markers + trails from `/api/aircraft` (also on first page load).
3. Filters (callsign/ICAO, airborne-only, altitude band, EPWR to/from) and sort (callsign / altitude / speed / dist EPWR) apply client-side to list and map. Icons are coloured by altitude. **Follow** keeps the map on the selected flight during Live updates.
4. Click a flight (list or map) to open details: live ADS-B + route/type from [adsbdb](https://www.adsbdb.com) (hexdb fallback) via `/api/aircraft/{icao24}`. Refresh also warms routes (~2.5s budget) so EPWR filters work on the list. Inbounds show distance/ETA to EPWR; the map draws a short predicted track. Share with `?icao=…`.
5. Logs are structured JSON by default (`LOG_FORMAT` / `LOG_LEVEL`); `/healthz` is omitted from access logs.

### Render / cloud hosts

OpenSky may block hyperscaler IPs. Run a **fetcher** on a normal host and set on Render:

```text
UPSTREAM_URL=https://your-fetcher.example
UPSTREAM_TOKEN=shared-secret
```

See [`deploy/fetcher/README.md`](deploy/fetcher/README.md).
