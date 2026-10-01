# OpenSky fetcher (devops) + Render UI

OpenSky often blocks cloud IPs (Render). Run a **fetcher** on a residential/normal
host (e.g. devops) and point the Render UI at it via `UPSTREAM_URL`.

```
[Browser] → [Render UI] --UPSTREAM_URL--> [/api/fetch and /api/meta on devops] → [OpenSky / adsbdb]
```

The same binary is used on both sides. On the fetcher, set `FETCH_TOKEN` and **do not** set `UPSTREAM_URL` (that would recurse). `LIVE_TOKEN` defaults to `FETCH_TOKEN` when unset — so Live/SSE on the fetcher host also require that secret. That is usually fine (fetcher is not a public UI).

`/api/fetch` only refreshes OpenSky and returns JSON. It does **not** warm routes, evaluate alerts, or change the fetcher’s focus ICAO. The Render UI `/refresh`, Live poller, and `POST /api/focus` POST `{UPSTREAM_URL}/api/fetch?lamin=&lomin=&lamax=&lomax=` (UI store box, 4 decimals), then warm + alert on the UI process. The fetcher `SetBBox` from those params (in-memory, last POST wins) so an airport chip does not keep pulling the fetcher’s boot box. Point `ALERT_WEBHOOK_URL` at the **UI** host, not the fetcher.

## 1. Fetcher on devops

```bash
# build & install
cd ~/src/wroclaw-sky
go build -o /usr/local/bin/wroclaw-sky ./cmd/wroclaw-sky

sudo mkdir -p /etc/wroclaw-sky
# shared secret (Render + fetcher)
echo 'FETCH_TOKEN=change-me' | sudo tee /etc/wroclaw-sky/fetcher.env

sudo cp deploy/fetcher/wroclaw-sky-fetcher.service /etc/systemd/system/
# Unit ships WorkingDirectory=/root/src/wroclaw-sky and PORT=8082 — edit the
# checkout path (and EnvironmentFile=/etc/wroclaw-sky/fetcher.env) before enable.
sudo systemctl daemon-reload
sudo systemctl enable --now wroclaw-sky-fetcher
```

Expose publicly with Tailscale Funnel (public HTTPS):

```bash
sudo tailscale funnel --bg 8082
tailscale funnel status
```

Note the HTTPS URL, e.g. `https://devops.tailXXXX.ts.net`.

Test:

```bash
# boot / last-set box (omit lamin…lomax)
curl -sS -X POST -H "Authorization: Bearer change-me" \
  https://devops.tailXXXX.ts.net/api/fetch | head

# same override the UI sends on Refresh / Live / POST /api/focus (EPWR metro)
curl -sS -X POST -H "Authorization: Bearer change-me" \
  "https://devops.tailXXXX.ts.net/api/fetch?lamin=50.9000&lomin=16.7000&lamax=51.3000&lomax=17.4000" | head

# enrichment proxy (UI uses this when hexdb is blocked on Render)
curl -sS -H "Authorization: Bearer change-me" \
  "https://devops.tailXXXX.ts.net/api/meta?icao24=3c6444&callsign=DLH123"
```

`/api/fetch` and `/api/meta` accept `Authorization: Bearer`, `?token=`, or cookie `wroclaw_sky_live`. Empty `FETCH_TOKEN` leaves both open — do not expose the funnel without a token.

All four bbox params must parse (`lamin,lomin,lamax,lomax`, min < max, lat -90..90, lon -180..180). Partial or invalid → **400** `bbox invalid`. Missing all four leaves the last `SetBBox` (boot `OPENSKY_BBOX` / `FOCUS_ICAO` until the first UI refresh). `GET /api/focus` on this host still shows the boot ICAO — only the query box moves. Restart resets the box. Two UI hosts sharing one fetcher: last POST wins.

## 2. Render UI env

| Key | Value |
|-----|--------|
| `UPSTREAM_URL` | `https://devops.tailXXXX.ts.net` (no trailing path) |
| `UPSTREAM_TOKEN` or `FETCH_TOKEN` | same as fetcher `FETCH_TOKEN` |
| `LIVE_TOKEN` | set explicitly if you do not want it to inherit `FETCH_TOKEN` |
| `SHARE_FOCUS` | `0` on a public UI so `GET /?focus=` does not switch the process (chips still use `POST /api/focus`) |
| `LOG_FORMAT` | `json` |
| Health Check Path | **`/healthz`** (never `/readyz`) |

Do **not** set `UPSTREAM_URL` on the fetcher host. The UI calls `{UPSTREAM_URL}/api/fetch?lamin&lomin&lamax&lomax` and `{UPSTREAM_URL}/api/meta`. Redeploy **both** sides after this contract — an old fetcher ignores the query and keeps serving its boot airport after a chip switch.

Redeploy Render, click **Refresh from OpenSky**. The UI shows an upstream banner when `UPSTREAM_URL` is set.

If the service restart-loops, check Health Check Path is `/healthz` and that `LIVE_COOKIE_SAMESITE` is one of `lax` / `strict` / `none` (invalid value exits on boot). Scratch-based images have no wget — Compose already uses `wroclaw-sky healthcheck`.

The Docker healthcheck binary probes `http://127.0.0.1:$PORT/healthz` (3s timeout). Fetcher systemd does **not** set `TRAILS_*`; trails stay in memory unless you add them to `fetcher.env`. Do not set `UPSTREAM_URL` on this host.
