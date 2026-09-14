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
  logging/             # slog + HTTP access log (Flush/Unwrap so SSE works)
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
| `LIVE_TOKEN` | `FETCH_TOKEN` | Protects Live / SSE / `/refresh` / `POST /api/focus`. Empty = open. Never embedded in HTML. Does **not** protect `GET /?focus=` |
| `LIVE_COOKIE_HOURS` | `8` | HttpOnly cookie `wroclaw_sky_live` TTL. Invalid value **exits on boot** |
| `LIVE_AUTH_RPM` | `10` | `POST /api/auth/live` per client IP (uses `X-Forwarded-For` first hop). Invalid → boot fail |
| `LIVE_COOKIE_SECURE` | off | Set `1`/`true`/`yes`/`on` behind HTTPS |
| `LIVE_COOKIE_SAMESITE` | `lax` | `lax` / `strict` / `none`. Invalid → boot fail. `none` needs `LIVE_COOKIE_SECURE` in browsers |
| `ALERT_WEBHOOK_URL` | empty | POST JSON on new approach / low-pass edges |
| `ALERT_WEBHOOK_DIGEST` | **on** | One POST per evaluate cycle (`{"type":"digest",…}`). Set `0`/`false`/`off` for one POST per event |
| `ALERT_MUTE` | empty | Comma-separated ICAO24s dropped from **webhook** payloads (SSE / `/api/alerts` stay complete) |
| `ALERT_AIRLINE` | empty | Callsign prefix (uppercased); only matching events reach the **webhook** |
| `SHARE_FOCUS` | **on** | When on, `GET /?focus=` switches process-wide focus (known ICAO, no auth). Set `0`/`false`/`off` so share URLs keep `focus=` for display only; use `POST /api/focus` to switch |
| `APPROACH_RADIUS_KM` | `40` | Approach + low-pass radius around focus (map draws a dashed amber approach ring) |
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

1. Set `LIVE_TOKEN` (and `FETCH_TOKEN` if using upstream fetcher). If only `FETCH_TOKEN` is set, Live/SSE use the same secret. `LIVE_TOKEN` also gates `/refresh`, so anonymous visitors cannot burn OpenSky credits.
2. Mount `/data` for `TRAILS_FILE` / `TRAILS_DB` (compose volume `sky-trails`).
3. **Health Check Path on Render must be `GET /healthz`** (always 200). Do **not** use `/readyz` as the Render health check — that caused restart loops when OpenSky/circuit failed. Scrape `GET /metrics`; optional k8s-style readiness: `GET /readyz?strict=1`.
4. Optionally set `ALERT_WEBHOOK_URL` / `ALERT_WEBHOOK_DIGEST` (default on — one POST per evaluate cycle), `ALERT_MUTE` / `ALERT_AIRLINE` (server-side webhook filter) and `LOW_PASS_ALT_M`.
5. For multiple replicas, use `docker-compose.prod.yml` + `TRAILS_REDIS_URL` (or sticky sessions + local SQLite). SSE and the Live poller are **per process** — use sticky sessions so EventSource stays on one replica. Disable proxy buffering for `/api/events` (response already sends `X-Accel-Buffering: no` for nginx).
6. Keep OpenSky credentials off the public UI host when using a fetcher (`UPSTREAM_*`). UI shows a fetcher banner when upstream is configured and refresh fails. Alerts and route warming run on the **UI** host (`/refresh` / Live), not on `/api/fetch`.
7. Optional auth tuning: `LIVE_COOKIE_HOURS` (default 8), `LIVE_AUTH_RPM` (default 10), `LIVE_COOKIE_SECURE`, `LIVE_COOKIE_SAMESITE` (`lax`/`strict`/`none`). `docker-compose.yml` defaults `LIVE_TOKEN` to `change-me` if unset (`go run` stays open when both tokens are empty).
8. Grafana: import `grafana/wroclaw-sky.json` (Prometheus datasource pointing at `/metrics`; dashboard UID `wroclaw-sky`). Metric names below.
9. Share URL: `arrivals=0` / `departures=0` hide boards (and survive an HTMX swap); `predict=sel` / `predict=0` gate predicted tracks; `tiles=light` for light Esri basemap; export `GET /api/arrivals?download=1` / `GET /api/departures?download=1` (accepts `q=` / `airline=`). `?focus=` is **process-wide** and unauthenticated when `SHARE_FOCUS` is on (default) — set `SHARE_FOCUS=0` on shared deploys and prefer `POST /api/focus` behind `LIVE_TOKEN`.

### Ops runbook

| Symptom | Check | Action |
|--------|--------|--------|
| Render restart loop | Health Check Path | Set to **`/healthz`** (not `/readyz`). Redeploy. |
| Process exits at boot | Logs `config` | Fix `LIVE_COOKIE_SAMESITE` (`lax`/`strict`/`none`), `LIVE_COOKIE_HOURS` (>0), `LIVE_AUTH_RPM` (>0), `FOCUS_*`, `OPENSKY_BBOX` |
| UI up, no aircraft | `/healthz` → `stale` / `circuit_open` | Direct OpenSky: circuit opens after **3** failures for **60s**, then **one** half-open retry. Fetcher (`UPSTREAM_URL`) failures set `stale` only (no circuit). Fix credentials / `UPSTREAM_*`; UI serves last snapshot |
| Live map frozen, SSE 500 `streaming unsupported` | `GET /api/events` body | ResponseWriter must implement `http.Flusher`. Production `logging.AccessLog` forwards `Flush` / `Unwrap` (regression if a middleware wraps the mux without that). |
| Live map frozen, SSE 200 but no pushes | Proxy / `sse_clients` | Turn off buffering (`proxy_buffering off;` or rely on `X-Accel-Buffering: no`). Sticky sessions. Hub drops updates when a client's buffer of **4** is full. |
| Live markers jump then sit still | Dead reckoning | DR needs Live on, vel ≥ 15 m/s, and a snapshot younger than **50s**. Playback disables DR. Slow/taxiing traffic stays on the last OpenSky fix. |
| Arrivals/departures empty | Route cache | Boards need warmed origin/dest. Click **Refresh** or enable Live (not `/api/fetch`). Check enrichment/upstream `/api/meta`. |
| Live / SSE 401 | Cookie missing or expired | Re-auth via Live prompt or `POST /api/auth/live`; cookie TTL = `LIVE_COOKIE_HOURS`. `GET /api/auth/live` refreshes TTL when already ok. Compose image default token is `change-me`. |
| Refresh button 401 | `LIVE_TOKEN` set, no cookie | `/refresh` needs the token. The UI prompts via `htmx:confirm` → `ensureLiveAuth`; for scripts send `Authorization: Bearer $LIVE_TOKEN` or `?token=` |
| Auth 429 | Too many `POST /api/auth/live` | Back off; limit = `LIVE_AUTH_RPM` per client IP (`X-Forwarded-For` first hop) |
| Cross-site cookie dropped | `SameSite=none` without Secure | Set `LIVE_COOKIE_SECURE=true` (prod overlay does this) |
| Alerts noisy in the browser | Share URL `mute=` / `alert_airline=` | Client-side only (`localStorage` key `wroclaw-sky-mute` + URL). Filter by type; export via **Export JSON** or `GET /api/alerts?download=1` |
| Webhook noisy | `ALERT_MUTE` / `ALERT_AIRLINE` | Server-side webhook filter: mute ICAO24s and/or keep one callsign prefix. SSE, `/api/alerts`, and `wroclaw_sky_alerts_total` stay unfiltered; a fully filtered cycle sends **no** POST |
| Empty boards right after deploy | Boot bootstrap | `run()` fires `BootstrapRefresh` in the background; it is skipped when a snapshot already exists. Check logs for refresh errors / `wroclaw_sky_stale` |
| Alerts replay / missing after airport switch | How focus changed | Both `POST /api/focus` and `GET /?focus=` (known ICAO) **reset** alert bootstrap. If webhooks still burst, check Live is on and whether the switch failed (unknown ICAO is ignored). |
| Everyone’s map jumped airport | Share link `?focus=` | `GET /?focus=` is **process-wide** when `SHARE_FOCUS` is on (default) and needs **no** Live token (known ICAOs only). Set `SHARE_FOCUS=0` on public UIs; use `POST /api/focus` (auth required). `GET /api/focus` lists `known` / `presets` / `presets_eu`. |
| Trails empty on replica | No Redis | Set `TRAILS_REDIS_URL` (key `wroclaw-sky:trails`, TTL = 3 min grace). File/SQLite are local to the container |
| Docker healthcheck fails | scratch image | Use `CMD ["/usr/local/bin/wroclaw-sky", "healthcheck"]` — not wget/curl |

Published images (on tag `v*`):

```bash
docker pull ghcr.io/dev0pos/wroclaw-sky:latest
```

## HTTP API

Auth tokens are accepted as `Authorization: Bearer …`, `?token=`, or cookie `wroclaw_sky_live`.

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET | `/` | — | HTML UI. Query = [share URL](#share-url). `?focus=` switches **process** focus when `SHARE_FOCUS` is on (known ICAO, no auth) and resets alert bootstrap |
| GET/POST | `/refresh` | `LIVE_TOKEN` | `Store.Refresh` (OpenSky or upstream), warm routes (~2.5s), **evaluate alerts**, SSE snapshot, flights partial. Requires the token when set — the UI runs `ensureLiveAuth` on `htmx:confirm` before the POST |
| GET | `/flights` | — | HTMX flights partial (no fetch) |
| GET | `/api/aircraft` | — | JSON snapshot (`type=update`, `updated_at`, `aircraft`, `trails`, `count`, `stale`, `circuit_open`, `upstream`, `focus`, `error`) |
| GET | `/api/aircraft/{icao24}` | — | Live vector + adsbdb/hexdb enrichment |
| GET/POST | `/api/fetch` | `FETCH_TOKEN` | Fetcher: `RefreshOpenSky` only — **no** route warm, **no** alert evaluate. Same JSON as `/api/aircraft` |
| GET/POST | `/api/meta` | `FETCH_TOKEN` | Fetcher: enrich `?icao24=&callsign=` **locally** (never recurses `UPSTREAM_URL`) |
| GET/POST | `/api/live` | `LIVE_TOKEN` | Heartbeat: start/extend shared poller (**45s** interval, **90s** lease). DELETE does **not** stop the poller (other tabs). Map markers dead-reckon client-side between SSE ticks (no extra API) |
| POST | `/api/auth/live` | rate-limited | Body/query/`Bearer` token → Set-Cookie. Wrong token → 401; over RPM → 429 |
| GET | `/api/auth/live` | — | `{required, ok, ttl_sec}`; rotates cookie TTL when already authorized |
| DELETE | `/api/auth/live` | — | Clear cookie |
| GET | `/api/events` | `LIVE_TOKEN` | SSE (`text/event-stream`). `event: hello` then `event: update` with JSON `type=update` or `type=alert`. Needs `http.Flusher`; sets `X-Accel-Buffering: no` |
| GET | `/api/focus` | — | Current focus, bbox, `known`, `presets` (PL), `presets_eu` |
| POST | `/api/focus` | `LIVE_TOKEN` | Switch ICAO (`icao`, optional `lat`/`lon`/`city`/`radius_km`). Resets alert bootstrap |
| GET | `/api/trails` | — | Download `trails.json` (≤48 points / ICAO, 3 min grace after leaving bbox) |
| GET | `/api/alerts` | — | Recent alerts (cap **40**). `?download=1` or `?export=1` → attachment |
| GET | `/api/arrivals` | — | Airborne + **enriched dest = focus** + lat/lon. Sort: nonzero ETA first, then ETA, distance, callsign. `approach` = dist ≤ `APPROACH_RADIUS_KM`. Optional `?q=` (callsign/ICAO24 substring) and `?airline=` (airline hint substring, `any` = all). `?download=1` / `?export=1` → `arrivals.json` (same filters) |
| GET | `/api/departures` | — | Same as arrivals but **enriched origin = focus**. UI labels in-radius rows `near`. Same `?q=` / `?airline=` filters. `?download=1` / `?export=1` → `departures.json` |
| GET | `/healthz` | — | **Always 200** JSON (liveness). Includes `stale`, `circuit_open`, `aircraft`, `sse_clients` |
| GET | `/readyz` | — | Always 200 unless `?strict=1` **and** circuit open → 503 |
| GET | `/metrics` | — | Prometheus text — see [metrics](#prometheus) |
| GET | `/manifest.webmanifest` `/sw.js` `/static/` | — | PWA shell + vendored HTMX/Leaflet. SW cache `wroclaw-sky-v1`; `/api/aircraft` is network-first |

Webhook POST (`User-Agent: wroclaw-sky-alerts`, 10s timeout):

```json
{"type":"digest","focus":"EPWR","count":2,"at":"2026-09-02T12:00:00Z","alerts":[{"type":"approach","icao24":"abc123","focus":"EPWR","at":"…"}]}
```

With digest off, each event is posted as a single `AlertEvent` (`type` is `approach` or `low_pass`). Approach requires destination ICAO = focus and distance ≤ `APPROACH_RADIUS_KM` (also drawn as a dashed amber ring on the map). First snapshot after boot, **`POST /api/focus`**, or a successful **`GET /?focus=`** (when `SHARE_FOCUS` is on) bootstraps without firing.

## Share URL

`viewstate.Parse` reads query params; `Encode` omits defaults so links stay short.

| Param | Values | Default |
|-------|--------|---------|
| `q` | callsign / ICAO search | empty |
| `airborne` | `1` | off |
| `alt` | `any` / `low` / `mid` / `high` (baro ft; see [Client UI](#client-ui)) | `any` |
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
| `departures` | `0` hides board | **shown** |
| `predict` | `all` (omit) / `sel` (selected+inbound) / `0` (off) | **all** |
| `tiles` | `dark` / `light` (Esri Canvas, no API key) | `dark` |
| `icao` | selected aircraft | empty |
| `focus` | ICAO override (known airports only). **Process-wide** when `SHARE_FOCUS` is on; display-only when off. Resets alert bootstrap on switch | process focus |
| `pb_at` / `pb_from` / `pb_to` | unix seconds | unset |
| `pb_speed` | `0.5` / `1` / `2` | `1` |

Example: `?epwr=to&sort=epwr&live=1&airline=LOT&alert=1&focus=EPWA&tiles=light&arrivals=0&departures=0&predict=sel&mute=abc123`

Filters/sort/mute/tiles/predict/playback apply **client-side**. `?focus=` on `/` mutates the **process** focus + bbox when `SHARE_FOCUS` is enabled (default) and the ICAO is in `internal/geo/focus.go` (unknown codes are ignored here; use `FOCUS_LAT`/`FOCUS_LON` or `POST /api/focus`). With `SHARE_FOCUS=0`, the URL still carries `focus=` for display/sync but does not switch the server (the UI toasts “Share focus disabled”). Like `POST /api/focus`, a successful share-URL switch resets alert bootstrap so inbound traffic at the new airport is not replayed as fresh alerts.

Airport chips and the focus `<select>` always call **`POST /api/focus`** (Live auth). They never use the unauthenticated share-URL switch.

## Client UI

Browser logic lives in `internal/server/templates/index.html` (list markup in `flights.html`). No extra API.

| Behavior | Rule |
|----------|------|
| Altitude filter `alt=` | Baro `altitude_m` × 3.28084. `low`: 0 < ft < 10000; `mid`: 10000–25000 inclusive; `high`: > 25000. Ground and 0 m are excluded from every band |
| Marker / list color | Ground slate; < 10000 ft green; < 25000 ft sky; else purple (list uses 3048 m / 7620 m) |
| Climb / descent | OpenSky `vertical` m/s. ↑ if > **0.5**, ↓ if < **−0.5** (list + map glyph) |
| Dead reckoning | **Live only**, 1s tick, vel ≥ **15 m/s**, cap **50s** past last `updated_at`. Skipped on ground, NaN track, or trail playback |
| Predicted track | 120s constant-velocity dashed line; same 15 m/s floor. `predict=sel` = selected **or** inbound (dest = focus) |
| Map rings | Cyan **1.2 km** ARP; dashed amber **`APPROACH_RADIUS_KM`** (default 40 km) |
| Snapshot age | `#stat-updated` → “just now” / “Xs ago” / “Xm ago” from `updated_at` / `data-updated-ms` |
| Copy link | Writes the current share URL to the clipboard |
| Board toggles after Refresh | Boards live in the HTMX partial, so `htmx:afterSwap` on `#flights` re-runs `applyBoardVisibility()` + `applyListFilter()` — `arrivals=0` / `departures=0` survive a swap |
| Board Export links | `syncExportLinks()` copies the current `q` / airline into `/api/arrivals?download=1&q=…&airline=…` so the JSON matches the screen |
| Refresh with `LIVE_TOKEN` | `htmx:confirm` on `#refresh-btn` awaits `ensureLiveAuth()` (cookie via `POST /api/auth/live`) before htmx issues the request |
| `localStorage` | `wroclaw-sky-mute`, `wroclaw-sky-sort`, `wroclaw-sky-live` (URL query wins when present) |
| Client Live heartbeat | `POST /api/live` every **30s** (server poller 45s / lease 90s). No `EventSource` → fallback poll 45s |

ETA on boards / hints is 0 when ground speed < **5 m/s** (`geo.ETASeconds`).

**Approach radius:** alerts, the map ring, JS markers, the boards, and the server-rendered list “approach” chip all use `APPROACH_RADIUS_KM`. The chip comes from `flightRow.Approach` (computed in `snapshotData` via `Server.onApproach`), so a custom radius is consistent everywhere.

**Empty arrivals/departures:** origin/destination are **not** on the OpenSky state vector. Boards and `epwr=` filters read `enricher.CachedRoute` (adsbdb → hexdb, 30 min TTL, ~2.5s warm on `/refresh` / Live). `/api/fetch` does not warm. Boot runs one background `BootstrapRefresh` (skipped if Live already refreshed), so the boards normally fill within a few seconds of start without a Refresh click.

## CI / release

GitHub Actions:

- **CI** on `main` / PRs: `go test`, build, golangci-lint **v2.13.1 from source** (`GOTOOLCHAIN=local`, required for Go 1.27), Docker build + **Trivy** (fail on any severity including `UNKNOWN`)
- **Release** on tag `v*`: Trivy gate → push multi-arch image to **GHCR** + binary assets (linux/darwin amd64/arm64)

```bash
git tag v0.1.0
git push origin v0.1.0
```

## How it works

1. OpenSky is queried when you click **Refresh** (~1 API credit for the bbox), or via the shared **Live** poller (one server-side fetch every 45s for all Live viewers; clients heartbeat `POST /api/live` every 30s and receive pushes on `GET /api/events`). OpenSky HTTP timeout is 60s with 2 retries; upstream fetch uses 90s and `User-Agent: wroclaw-sky-ui` (1 MiB body cap).
2. `/refresh` and the Live loop call `refreshAndWarm`: `Store.Refresh` → route warm (~2.5s) → `evaluateAlerts` → SSE snapshot. `/refresh` requires `LIVE_TOKEN` when set. At boot `run()` also calls `BootstrapRefresh` once in a goroutine — same path, but a no-op when a snapshot already exists (so an immediately-live client does not cause a double fetch). Fetcher `/api/fetch` only calls `RefreshOpenSky` (no warm, no alerts) so the **UI** host still evaluates alerts after it pulls upstream.
3. HTMX swaps the flight list; the map applies SSE/`/api/aircraft` snapshots (`updated_at` drives age + DR). Between Live ticks the browser **dead-reckons** airborne markers from velocity/track (1s, ≥15 m/s, ≤50s; no extra API). Predicted tracks are a 120s dashed extrapolation. EventSource uses `withCredentials`. After `event: hello`, payloads ride `event: update` (`type=update` snapshot or `type=alert`). Slow SSE clients are dropped (hub buffer **4**). HTMX/Leaflet are vendored under `/static/`. Map tiles are Esri Canvas dark/light (CARTO watermarked without a key).
4. Filters/sort are client-side ([Client UI](#client-ui)). **Follow** pans with the selected flight. Browser notify + webhook + SSE alerts are edge-triggered (destination = focus and distance ≤ `APPROACH_RADIUS_KM`; low-pass needs `LOW_PASS_ALT_M` > 0). Mute lives in `localStorage` (`wroclaw-sky-mute`) and the share URL. Trail playback interpolates breadcrumbs (`cache.PositionAt`) at ×0.5/1/2; export `GET /api/trails`.
5. Click a flight for details: in-process cache **30 min** (empty misses are not cached) → optional `{UPSTREAM_URL}/api/meta` → **adsbdb** → **hexdb** (4s HTTP timeout). Arrivals / departures use that cached route (airborne inbound dest / outbound origin = focus), sorted by ETA then distance. Trails: max **48** points, **3 min** grace, persist file / SQLite / Redis. Failed refreshes keep the last snapshot (`stale`). Direct OpenSky opens a circuit after 3 failures (60s cooldown, then one half-open try); upstream errors do not.
6. Logs are JSON by default; `/healthz` and `/readyz` skip access logs. PWA service worker (`/sw.js`, `Cache-Control: no-cache`) caches the shell (`/`, vendored JS/CSS, manifest) as `wroclaw-sky-v1` and uses network-first for `/api/aircraft`.

### Focus airports

Known ARPs: `internal/geo/focus.go`. UI chips — **PL** `EPWR EPWA EPKK EPGD EPKT EPPO`; **EU** `EDDF EDDM LOWW LKPR EHAM LFPG EBBR LSZH`. Also known (no chip): `EPRZ EPSC EPLL EPBY EPMO EPLB EDDL EDDH`. Unknown ICAO needs `FOCUS_LAT`/`FOCUS_LON` (or JSON body on `POST /api/focus`).

### Prometheus

`GET /metrics` (no auth). Grafana dashboard `grafana/wroclaw-sky.json` (UID `wroclaw-sky`) graphs a subset.

| Metric | Type | Meaning |
|--------|------|---------|
| `wroclaw_sky_refresh_total` | counter | OpenSky/upstream refresh attempts (`/refresh`, Live, `/api/fetch`) |
| `wroclaw_sky_refresh_errors_total` | counter | Failed refreshes |
| `wroclaw_sky_last_refresh_unixtime` | gauge | Last successful refresh (unix seconds) |
| `wroclaw_sky_aircraft` | gauge | Aircraft in latest snapshot |
| `wroclaw_sky_live` | gauge | Shared poller active (0/1) |
| `wroclaw_sky_sse_clients` | gauge | Connected EventSource clients |
| `wroclaw_sky_sse_disconnects_total` | counter | SSE disconnects |
| `wroclaw_sky_circuit_open` | gauge | OpenSky breaker open (0/1) |
| `wroclaw_sky_stale` | gauge | Last refresh failed, serving the previous snapshot (0/1) |
| `wroclaw_sky_webhook_total` | counter | Alert webhook POST attempts (digest = 1 per cycle) |
| `wroclaw_sky_webhook_errors_total` | counter | Webhook failures / HTTP ≥ 300 |
| `wroclaw_sky_alerts_total` | counter | Edge-triggered alerts emitted |

### Render / cloud hosts

OpenSky may block hyperscaler IPs. Run a **fetcher** on a normal host and set on Render:

```text
UPSTREAM_URL=https://your-fetcher.example
UPSTREAM_TOKEN=shared-secret
```

**Render Health Check Path:** `/healthz` only. `/readyz` is informational by default (always 200); use `/readyz?strict=1` only if you intentionally want 503 when the OpenSky circuit is open.

See [`deploy/fetcher/README.md`](deploy/fetcher/README.md).
