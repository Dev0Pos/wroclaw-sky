# wroclaw-sky

Aircraft around Wrocław (OpenSky Network) — Go + HTMX + Leaflet.

## Layout

```
cmd/wroclaw-sky/       # main()
internal/
  config/              # env → App config (bbox, tokens, port)
  opensky/             # OpenSky client + Aircraft model + bbox helpers
  cache/               # in-memory snapshot + session trails
  geo/                 # haversine / ETA / approach
  meta/                # adsbdb/hexdb enrichment + airline hints
  viewstate/           # shareable URL filters / Live / alerts
  logging/             # slog + HTTP access log
  server/              # HTTP handlers + embedded templates + static/
```

## Run

```bash
go test ./...
go run ./cmd/wroclaw-sky
```

Open http://localhost:8081 — click **Refresh** (or enable **Live** for a shared server poll).

```bash
LOG_FORMAT=json LOG_LEVEL=info go run ./cmd/wroclaw-sky
PORT=3000 go run ./cmd/wroclaw-sky
```

Optional OpenSky credentials (higher credit quota):

```bash
OPENSKY_USER=you OPENSKY_PASS=secret go run ./cmd/wroclaw-sky
```

Custom region and focus airport:

```bash
# explicit bbox
OPENSKY_BBOX=52.00,20.70,52.50,21.30 FOCUS_ICAO=EPWA MAP_LABEL="EPWA · Warsaw" go run ./cmd/wroclaw-sky

# auto bbox (~80 km) around focus when OPENSKY_BBOX is unset
FOCUS_ICAO=EPWA FOCUS_RADIUS_KM=80 go run ./cmd/wroclaw-sky

# custom ARP with coordinates
FOCUS_ICAO=XXXX FOCUS_LAT=51.1 FOCUS_LON=17.0 FOCUS_CITY=Lab FOCUS_RADIUS_KM=60 go run ./cmd/wroclaw-sky
```

`FOCUS_ICAO` drives arrivals, approach, ETA, and the map circle. Persist session trails across restarts with `TRAILS_FILE=/data/trails.json`. Prometheus text metrics: `GET /metrics`.

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

1. OpenSky is queried when you click **Refresh** (~1 API credit for the bbox), or via the shared **Live** poller (one server-side fetch every 45s for all Live viewers; clients heartbeat `POST /api/live` and receive full snapshot pushes on `GET /api/events` SSE).
2. HTMX swaps the flight list; the map applies SSE/`/api/aircraft` snapshots (markers + trails). HTMX/Leaflet are served from `/static/` (vendored).
3. Filters (callsign/ICAO, airborne, altitude, focus to/from, airline) and sort apply client-side. Share the view with query params (`?epwr=to&sort=epwr&live=1&airline=LOT&alert=1&icao=…`). **Follow** keeps the map on the selected flight during Live updates. **Approach alert** notifies when a flight enters &lt;40 km inbound to `FOCUS_ICAO`.
4. Click a flight for details (adsbdb + hexdb fallback). Refresh warms routes (~2.5s). Inbounds show distance/ETA; the **arrivals** board lists them by ETA. **Trail playback** scrubs session history (unlocks after 2+ ticks; optional selected-only). Trails can persist via `TRAILS_FILE`.
5. Logs are structured JSON by default (`LOG_FORMAT` / `LOG_LEVEL`); `/healthz` and `/metrics` expose liveness and Prometheus counters. `/healthz` is omitted from access logs.

### Render / cloud hosts

OpenSky may block hyperscaler IPs. Run a **fetcher** on a normal host and set on Render:

```text
UPSTREAM_URL=https://your-fetcher.example
UPSTREAM_TOKEN=shared-secret
```

See [`deploy/fetcher/README.md`](deploy/fetcher/README.md).
