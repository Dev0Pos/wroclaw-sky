# wroclaw-sky

Aircraft around Wrocław (OpenSky Network) — Go + HTMX + Leaflet.

Requires **Go 1.27** (`go.mod` toolchain). Open http://localhost:8081 after `go run`.

## Layout

```
cmd/wroclaw-sky/       # main() + Docker healthcheck subcommand
internal/
  config/              # env → App config (bbox, tokens, port)
  opensky/             # OpenSky client + Aircraft model + bbox + circuit breaker
  cache/               # in-memory snapshot + session trails (file / SQLite / Redis)
  geo/                 # haversine / ETA / approach / focus airports
  meta/                # adsbdb/hexdb enrichment + airline hints
  viewstate/           # shareable URL filters / Live / alerts / tiles
  logging/             # slog + HTTP access log
  server/              # HTTP handlers + embedded templates + static/
deploy/fetcher/        # systemd unit for a residential OpenSky proxy
grafana/               # Prometheus dashboard JSON
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

# custom ARP with coordinates (unknown ICAO requires lat/lon)
FOCUS_ICAO=XXXX FOCUS_LAT=51.1 FOCUS_LON=17.0 FOCUS_CITY=Lab FOCUS_RADIUS_KM=60 go run ./cmd/wroclaw-sky
```

Default focus is **EPWR**. With no `OPENSKY_BBOX`, EPWR keeps the hardcoded Wrocław bbox; any other `FOCUS_ICAO` gets `FOCUS_RADIUS_KM` (default 80 km). Runtime focus switches always use that radius.

## Environment

| Variable | Default | Notes |
|----------|---------|--------|
| `PORT` | `8081` | Listen address `:{PORT}` |
| `OPENSKY_BBOX` | Wrocław metro | `lamin,lomin,lamax,lomax` (WGS84). When unset, bbox is derived from focus (see above) |
| `OPENSKY_USER` / `OPENSKY_PASS` | empty | OpenSky basic auth |
| `FOCUS_ICAO` | `EPWR` | Known ICAOs in `internal/geo/focus.go`. Unknown ICAO needs `FOCUS_LAT`/`FOCUS_LON` |
| `FOCUS_LAT` / `FOCUS_LON` / `FOCUS_CITY` | from known ARP | Custom airport |
| `FOCUS_RADIUS_KM` | `80` | Bbox radius for non-EPWR boot and all runtime `POST /api/focus` |
| `MAP_LABEL` | `{ICAO} · {City}` | Map badge |
| `TRAILS_FILE` | empty (Docker: `/data/trails.json`) | JSON trail persist |
| `TRAILS_DB` | empty | SQLite trail persist (`modernc.org/sqlite`, no CGO) |
| `TRAILS_REDIS_URL` | empty | Redis trail blob (`wroclaw-sky:trails`, TTL = 3 min). Needed for multi-replica |
| `UPSTREAM_URL` | empty | Fetcher base URL (UI host that cannot reach OpenSky) |
| `UPSTREAM_TOKEN` | `FETCH_TOKEN` | Bearer sent to the fetcher |
| `FETCH_TOKEN` | empty | Protects `/api/fetch` and `/api/meta` on **this** process. Also becomes `LIVE_TOKEN` when that is unset |
| `LIVE_TOKEN` | `FETCH_TOKEN` | Protects Live / SSE / `POST /api/focus`. Empty = open. Never embedded in HTML |
| `LIVE_COOKIE_HOURS` | `8` | HttpOnly cookie `wroclaw_sky_live` TTL. Invalid value **exits on boot** |
| `LIVE_AUTH_RPM` | `10` | `POST /api/auth/live` per client IP (uses `X-Forwarded-For` first hop). Invalid → boot fail |
| `LIVE_COOKIE_SECURE` | off | Set `1`/`true`/`yes`/`on` behind HTTPS |
| `LIVE_COOKIE_SAMESITE` | `lax` | `lax` / `strict` / `none`. Invalid → boot fail. `none` needs `LIVE_COOKIE_SECURE` in browsers |
| `ALERT_WEBHOOK_URL` | empty | POST JSON on new approach / low-pass edges |
| `ALERT_WEBHOOK_DIGEST` | **on** | One POST per evaluate cycle (`{"type":"digest",…}`). Set `0`/`false`/`off` for one POST per event |
| `APPROACH_RADIUS_KM` | `40` | Approach + low-pass radius around focus |
| `LOW_PASS_ALT_M` | `0` (disabled) | Airborne + baro alt ≤ this, inside approach radius |
| `LOG_FORMAT` | `json` | `json` or `text` |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |

## Docker

```bash
docker build -t wroclaw-sky .
LIVE_TOKEN=secret docker compose up --build
# optional Redis trails backend:
# LIVE_TOKEN=secret TRAILS_REDIS_URL=redis://redis:6379/0 docker compose --profile redis up --build
#
# Multi-replica production (2× app + Redis, secure cookies):
# LIVE_TOKEN=secret docker compose -f docker-compose.yml -f docker-compose.prod.yml --profile redis up --build
```

The image is **scratch** (no shell/wget). Probes must use the binary:

```bash
wroclaw-sky healthcheck   # GET http://127.0.0.1:$PORT/healthz
```

Runs as uid `65534`; mount `/data` for trails. Final image has no Alpine packages (avoids openssl CVEs).

### Production checklist

1. Set `LIVE_TOKEN` (and `FETCH_TOKEN` if using upstream fetcher). If only `FETCH_TOKEN` is set, Live/SSE use the same secret.
2. Mount `/data` for `TRAILS_FILE` / `TRAILS_DB` (compose volume `sky-trails`).
3. **Health Check Path on Render must be `GET /healthz`** (always 200). Do **not** use `/readyz` as the Render health check — that caused restart loops when OpenSky/circuit failed. Scrape `GET /metrics`; optional k8s-style readiness: `GET /readyz?strict=1`.
4. Optionally set `ALERT_WEBHOOK_URL` / `ALERT_WEBHOOK_DIGEST` (default on — one POST per evaluate cycle) and `LOW_PASS_ALT_M`.
5. For multiple replicas, use `docker-compose.prod.yml` + `TRAILS_REDIS_URL` (or sticky sessions + local SQLite). SSE and the Live poller are **per process** — use sticky sessions so EventSource stays on one replica.
6. Keep OpenSky credentials off the public UI host when using a fetcher (`UPSTREAM_*`). UI shows a fetcher banner when upstream is configured and refresh fails.
7. Optional auth tuning: `LIVE_COOKIE_HOURS` (default 8), `LIVE_AUTH_RPM` (default 10), `LIVE_COOKIE_SECURE`, `LIVE_COOKIE_SAMESITE` (`lax`/`strict`/`none`).
8. Grafana: import `grafana/wroclaw-sky.json` (Prometheus datasource pointing at `/metrics`; dashboard UID `wroclaw-sky`).
9. Share URL: `arrivals=0` hides board; `tiles=light` for light Esri basemap (dark/light toggle); export `GET /api/arrivals?download=1`.

### Ops runbook

| Symptom | Check | Action |
|--------|--------|--------|
| Render restart loop | Health Check Path | Set to **`/healthz`** (not `/readyz`). Redeploy. |
| Process exits at boot | Logs `config` | Fix `LIVE_COOKIE_SAMESITE` (`lax`/`strict`/`none`), `LIVE_COOKIE_HOURS` (>0), `LIVE_AUTH_RPM` (>0), `FOCUS_*`, `OPENSKY_BBOX` |
| UI up, no aircraft | `/healthz` → `stale` / `circuit_open` | Direct OpenSky: circuit opens after **3** failures for **60s**. Fetcher (`UPSTREAM_URL`) failures set `stale` only (no circuit). Fix credentials / `UPSTREAM_*`; UI serves last snapshot |
| Live / SSE 401 | Cookie missing or expired | Re-auth via Live prompt or `POST /api/auth/live`; cookie TTL = `LIVE_COOKIE_HOURS`. `GET /api/auth/live` refreshes TTL when already ok |
| Auth 429 | Too many `POST /api/auth/live` | Back off; limit = `LIVE_AUTH_RPM` per client IP (`X-Forwarded-For`) |
| Cross-site cookie dropped | `SameSite=none` without Secure | Set `LIVE_COOKIE_SECURE=true` (prod overlay does this) |
| Alerts noisy | Share URL `mute=` / `alert_airline=` | Mute is **client-side** (localStorage + URL), not server. Filter by type; export via **Export JSON** or `GET /api/alerts?download=1` |
| Alerts missing after airport switch | `POST /api/focus` | Focus change **resets** alert bootstrap so the new airport does not replay old edges |
| Wrong airport | Presets / `?focus=EPWA` | PL/EU chips or `POST /api/focus` (Live-auth required). `GET /api/focus` lists `known` / `presets` / `presets_eu` |
| Trails empty on replica | No Redis | Set `TRAILS_REDIS_URL`; file/SQLite are local to the container |
| Docker healthcheck fails | scratch image | Use `CMD ["/usr/local/bin/wroclaw-sky", "healthcheck"]` — not wget/curl |

Published images (on tag `v*`):

```bash
docker pull ghcr.io/dev0pos/wroclaw-sky:latest
```

## HTTP API

Auth tokens are accepted as `Authorization: Bearer …`, `?token=`, or cookie `wroclaw_sky_live`.

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET | `/` | — | HTML UI. Query = [share URL](#share-url) |
| GET/POST | `/refresh` | — | Fetch OpenSky (or upstream), warm routes (~2.5s), evaluate alerts, return flights partial |
| GET | `/flights` | — | HTMX flights partial (no fetch) |
| GET | `/api/aircraft` | — | JSON snapshot (`aircraft`, `trails`, `stale`, `circuit_open`, `upstream`, `focus`) |
| GET | `/api/aircraft/{icao24}` | — | Live vector + adsbdb/hexdb enrichment |
| GET/POST | `/api/fetch` | `FETCH_TOKEN` | Fetcher: refresh OpenSky, return same JSON as `/api/aircraft` |
| GET/POST | `/api/meta` | `FETCH_TOKEN` | Fetcher: enrich `?icao24=&callsign=` **locally** (never recurses `UPSTREAM_URL`) |
| GET/POST | `/api/live` | `LIVE_TOKEN` | Heartbeat: start/extend shared poller (**45s** interval, **90s** lease). DELETE does **not** stop the poller (other tabs) |
| POST | `/api/auth/live` | rate-limited | Body/query/`Bearer` token → Set-Cookie. Wrong token → 401; over RPM → 429 |
| GET | `/api/auth/live` | — | `{required, ok, ttl_sec}`; rotates cookie TTL when already authorized |
| DELETE | `/api/auth/live` | — | Clear cookie |
| GET | `/api/events` | `LIVE_TOKEN` | SSE. `event: hello` then `event: update` with JSON `type=update` (snapshot) or `type=alert` |
| GET | `/api/focus` | — | Current focus, bbox, `known`, `presets` (PL), `presets_eu` |
| POST | `/api/focus` | `LIVE_TOKEN` | Switch ICAO (`icao`, optional `lat`/`lon`/`city`/`radius_km`). Resets alert bootstrap |
| GET | `/api/trails` | — | Download `trails.json` (≤48 points / ICAO, 3 min grace after leaving bbox) |
| GET | `/api/alerts` | — | Recent alerts (cap **40**). `?download=1` or `?export=1` → attachment |
| GET | `/api/arrivals` | — | Focus-bound airborne, sorted by ETA. `?download=1` / `?export=1` → attachment |
| GET | `/healthz` | — | **Always 200** JSON (liveness). Includes `stale`, `circuit_open`, `aircraft`, `sse_clients` |
| GET | `/readyz` | — | Always 200 unless `?strict=1` **and** circuit open → 503 |
| GET | `/metrics` | — | Prometheus text (`wroclaw_sky_*` refresh, aircraft, live, SSE, circuit, webhooks, alerts) |
| GET | `/manifest.webmanifest` `/sw.js` `/static/` | — | PWA shell + vendored HTMX/Leaflet |

Webhook POST (`User-Agent: wroclaw-sky-alerts`, 10s timeout):

```json
{"type":"digest","focus":"EPWR","count":2,"at":"2026-09-02T12:00:00Z","alerts":[{"type":"approach","icao24":"abc123","focus":"EPWR","at":"…"}]}
```

With digest off, each event is posted as a single `AlertEvent` (`type` is `approach` or `low_pass`). Approach requires destination ICAO = focus and distance ≤ `APPROACH_RADIUS_KM`. First snapshot after boot (or focus switch) **bootstraps** without firing.

## Share URL

`viewstate.Parse` reads query params; `Encode` omits defaults so links stay short.

| Param | Values | Default |
|-------|--------|---------|
| `q` | callsign / ICAO search | empty |
| `airborne` | `1` | off |
| `alt` | `any` / `low` / `mid` / `high` | `any` |
| `epwr` | `any` / `to` / `from` / `either` (to/from **focus**) | `any` |
| `sort` | `callsign` / `alt` / `speed` / `epwr` | `callsign` |
| `airline` | display name (e.g. `LOT`) | any |
| `live` | `1` | off |
| `follow` | `0` to disable | **on** |
| `alert` | `1` approach browser notify | off |
| `alert_low` | `1` low-pass notify | off |
| `alert_airline` | callsign prefix (uppercased) | empty |
| `mute` | comma-separated ICAO24s | empty |
| `arrivals` | `0` hides board | **shown** |
| `tiles` | `dark` / `light` (Esri Canvas, no API key) | `dark` |
| `icao` | selected aircraft | empty |
| `focus` | ICAO override (known airports only on GET `/`) | process focus |
| `pb_at` / `pb_from` / `pb_to` | unix seconds | unset |
| `pb_speed` | `0.5` / `1` / `2` | `1` |

Example: `?epwr=to&sort=epwr&live=1&airline=LOT&alert=1&focus=EPWA&tiles=light&arrivals=0&mute=abc123`

Filters/sort apply **client-side**. `?focus=` on `/` switches the server focus when the ICAO is known.

## CI / release

GitHub Actions:

- **CI** on `main` / PRs: `go test`, build, golangci-lint **v2.13.1 from source** (`GOTOOLCHAIN=local`, required for Go 1.27), Docker build + **Trivy** (fail on any severity including `UNKNOWN`)
- **Release** on tag `v*`: Trivy gate → push multi-arch image to **GHCR** + binary assets (linux/darwin amd64/arm64)

```bash
git tag v0.1.0
git push origin v0.1.0
```

## How it works

1. OpenSky is queried when you click **Refresh** (~1 API credit for the bbox), or via the shared **Live** poller (one server-side fetch every 45s for all Live viewers; clients heartbeat `POST /api/live` and receive full snapshot pushes on `GET /api/events` SSE). OpenSky HTTP timeout is 60s with 2 retries; the store HTTP client (upstream fetch) uses 90s.
2. HTMX swaps the flight list; the map applies SSE/`/api/aircraft` snapshots (markers + trails). HTMX/Leaflet are served from `/static/` (vendored). Map tiles are Esri Canvas dark/light (CARTO watermarked without a key).
3. Filters (callsign/ICAO, airborne, altitude, focus to/from, airline) and sort apply client-side. **Follow** keeps the map on the selected flight during Live updates. **Approach alert** notifies on inbound approach; server also POSTs webhooks and SSE `type=alert` payloads. Trail playback supports speed ×0.5/1/2, ICAO marks, and JSON export (`/api/trails`).
4. Click a flight for details (adsbdb + hexdb fallback). Refresh warms routes (~2.5s). Inbounds show distance/ETA; the **arrivals** board lists them by ETA. Trails persist via `TRAILS_FILE` / `TRAILS_DB` / Redis (max 48 points, 3 min grace). Failed refreshes keep the last snapshot (`stale` banner). Direct OpenSky also opens a circuit after 3 failures (60s cooldown); upstream fetcher errors do not.
5. Logs are structured JSON by default (`LOG_FORMAT` / `LOG_LEVEL`); `/healthz` and `/readyz` are omitted from access logs. `/metrics` exposes Prometheus counters.

### Render / cloud hosts

OpenSky may block hyperscaler IPs. Run a **fetcher** on a normal host and set on Render:

```text
UPSTREAM_URL=https://your-fetcher.example
UPSTREAM_TOKEN=shared-secret
```

**Render Health Check Path:** `/healthz` only. `/readyz` is informational by default (always 200); use `/readyz?strict=1` only if you intentionally want 503 when the OpenSky circuit is open.

See [`deploy/fetcher/README.md`](deploy/fetcher/README.md).
