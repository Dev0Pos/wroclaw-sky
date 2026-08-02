# OpenSky fetcher (devops) + Render UI

OpenSky often blocks cloud IPs (Render). Run a **fetcher** on a residential/normal
host (e.g. devops) and point the Render UI at it via `UPSTREAM_URL`.

```
[Browser] → [Render UI] --UPSTREAM_URL--> [/api/fetch on devops] → [OpenSky]
```

## 1. Fetcher on devops

```bash
# build & install
cd ~/src/wroclaw-sky
go build -o /usr/local/bin/wroclaw-sky ./cmd/wroclaw-sky

sudo mkdir -p /etc/wroclaw-sky
# shared secret (Render + fetcher)
echo 'FETCH_TOKEN=change-me' | sudo tee /etc/wroclaw-sky/fetcher.env

sudo cp deploy/fetcher/wroclaw-sky-fetcher.service /etc/systemd/system/
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
curl -sS -X POST -H "Authorization: Bearer change-me" \
  https://devops.tailXXXX.ts.net/api/fetch | head
```

## 2. Render UI env

| Key | Value |
|-----|--------|
| `UPSTREAM_URL` | `https://devops.tailXXXX.ts.net` |
| `UPSTREAM_TOKEN` or `FETCH_TOKEN` | same as fetcher `FETCH_TOKEN` |
| `LOG_FORMAT` | `json` |

Do **not** set `UPSTREAM_URL` on the fetcher host.

Redeploy Render, click **Refresh from OpenSky**.
