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

`FOCUS_ICAO` drives arrivals, approach, ETA, and the map circle (switchable at runtime via `POST /api/focus` or `?focus=EPWA`). Persist trails with `TRAILS_FILE`, `TRAILS_DB` (SQLite), and/or `TRAILS_REDIS_URL`. Optional `LIVE_TOKEN` protects Live/SSE via HttpOnly cookie (`POST /api/auth/live`) — the secret is never embedded in HTML. `ALERT_WEBHOOK_URL` + `LOW_PASS_ALT_M` / `APPROACH_RADIUS_KM` for server alerts. Prometheus: `GET /metrics`. PWA: `/manifest.webmanifest` + `/sw.js`.

## Docker

```bash
docker build -t wroclaw-sky .
LIVE_TOKEN=secret docker compose up --build
# optional Redis trails backend:
# LIVE_TOKEN=secret TRAILS_REDIS_URL=redis://redis:6379/0 docker compose --profile redis up --build
```

### Production checklist

1. Set `LIVE_TOKEN` (and `FETCH_TOKEN` if using upstream fetcher).
2. Mount `/data` for `TRAILS_FILE` / `TRAILS_DB` (compose volume `sky-trails`).
3. Point liveness at `GET /healthz`, readiness at `GET /readyz` (503 when OpenSky circuit is open); scrape `GET /metrics`.
4. Optionally set `ALERT_WEBHOOK_URL` and `LOW_PASS_ALT_M`.
5. For multiple replicas, prefer `TRAILS_REDIS_URL` (or sticky sessions + local SQLite).
6. Keep OpenSky credentials off the public UI host when using a fetcher (`UPSTREAM_*`).
7. Optional auth tuning: `LIVE_COOKIE_HOURS` (default 8), `LIVE_AUTH_RPM` (default 10).

### Ops runbook

| Symptom | Check | Action |
|--------|--------|--------|
| UI up, no aircraft | `/healthz` → `circuit_open` / `stale` | Wait for breaker (≈60s) or fix OpenSky/fetcher; `/readyz` is 503 while open |
| Live / SSE 401 | Cookie missing or expired | Re-auth via Live (prompt) or `POST /api/auth/live`; cookie TTL = `LIVE_COOKIE_HOURS` |
| Auth 429 | Too many `POST /api/auth/live` | Back off; limit = `LIVE_AUTH_RPM` per client IP |
| Alerts noisy | Share URL `mute=` / `alert_airline=` | Mute ICAOs in alert history; filter by callsign prefix |
| Wrong airport | Presets / `?focus=EPWA` | Use PL preset chips or `POST /api/focus` |

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
3. Filters (callsign/ICAO, airborne, altitude, focus to/from, airline) and sort apply client-side. Share the view with query params (`?epwr=to&sort=epwr&live=1&airline=LOT&alert=1&focus=EPWA&icao=…`). **Follow** keeps the map on the selected flight during Live updates. **Approach alert** notifies on inbound approach; server can also POST webhooks / SSE `alert` events.
4. Click a flight for details (adsbdb + hexdb fallback). Refresh warms routes (~2.5s). Inbounds show distance/ETA; the **arrivals** board lists them by ETA. **Trail playback** supports speed ×0.5/1/2, ICAO marks, and JSON export (`/api/trails`). Trails persist via `TRAILS_FILE`. Failed OpenSky refreshes keep the last snapshot (`stale` banner + circuit breaker).
5. Logs are structured JSON by default (`LOG_FORMAT` / `LOG_LEVEL`); `/healthz` and `/metrics` expose liveness and Prometheus counters. `/healthz` is omitted from access logs.

### Render / cloud hosts

OpenSky may block hyperscaler IPs. Run a **fetcher** on a normal host and set on Render:

```text
UPSTREAM_URL=https://your-fetcher.example
UPSTREAM_TOKEN=shared-secret
```

See [`deploy/fetcher/README.md`](deploy/fetcher/README.md).
