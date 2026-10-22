# Deployment

The service is one static binary (or the Docker image) plus a data directory.
It listens on plain HTTP, so put TLS in front of it for anything not on
localhost.

## Configuration

| Variable / flag | Default | Purpose |
|---|---|---|
| `MCP_BEARER_TOKEN` | required | Static bearer token for `/mcp` and `/api/v1`. Use a long random value. |
| `MCP_AUTH_DISABLED=1` | off | Disables auth. Development only. |
| `WORKOUT_TIMEZONE` | `Asia/Jerusalem` | IANA zone that decides the workout date. The binary embeds the zone database. |
| `WORKOUT_WEEK_START` | `sunday` | First day of the week for schedules, frequency targets, training summaries and weight trends: `sunday` or `monday`. |
| `TRAINING_DB_PATH` | `~/.local/share/training-mcp/training.db` | SQLite file. |
| `MEDIA_DIR` | `media/` beside the database (`/data/media` in Docker) | Uploaded videos. If it cannot be created the server keeps running with video storage disabled. |
| `MAX_VIDEO_MB` | `200` (1–4096) | Video upload size limit. |
| `WEB_BASE_PATH` | unset | Mounts the optional upstream PWA under this secret prefix. |
| `--addr` | `:8080` | Listen address. |
| `--stdio` | off | Serve MCP over stdin/stdout instead of HTTP (no network, no auth). |
| `backup <file>` / `media-gc` | — | Commands, see [backup-restore.md](backup-restore.md). |

`GET /health` is unauthenticated and returns `{"status":"ok"}`.

## Web UI

The React UI in `web/` is embedded in the binary when it is built first:
`scripts/build-ui.sh` (needs Node 20+), then `go build ./cmd/training-mcp`. The
Docker image builds and embeds it automatically. It is served at `/`:

* `/login` asks for the bearer token once and sets an HttpOnly session cookie;
  the API accepts that cookie, so the browser never holds the token.
* Settings → "התנתק" (`POST /logout`) signs the browser out. Changing
  `MCP_BEARER_TOKEN` signs out every browser.
* A binary built without the UI still serves the API and MCP; `/` then explains
  how to build the UI.
* Behind a tunnel or reverse proxy, expose `/` as well as `/mcp`, `/api/v1` and
  `/health`, and make sure the proxy sets `X-Forwarded-Proto: https` (Cloudflare
  Tunnel does) so the cookie is marked Secure.

## Docker

```sh
docker build -t workout-body .
docker run -d --name workout-body -p 127.0.0.1:8080:8080 \
  -e MCP_BEARER_TOKEN="$(openssl rand -hex 32)" \
  -e WORKOUT_TIMEZONE=Asia/Jerusalem \
  -v workout-data:/data workout-body
```

The image is distroless, runs as UID 65532, and stores data in the `/data`
volume. Back that volume up ([backup-restore.md](backup-restore.md)).

## HTTPS and clients

* Put a reverse proxy (Caddy, nginx, Traefik) or a tunnel in front, forward
  `/` (the web UI), `/mcp`, `/api/v1` and `/health`, and keep the app bound to
  localhost.
* Remote clients that cannot send a static `Authorization` header (some hosted
  connectors) cannot use this server directly — see
  [plugin-setup.md](plugin-setup.md).
* Do not enable `MCP_AUTH_DISABLED` on a network-reachable host.

## Operations

* One process, one SQLite file; do not run two instances on the same file.
* Upgrades: back up, deploy the new binary/image, start. Migrations run on
  startup ([migrations.md](migrations.md)).
* Logs go to stderr. JSON and MCP request bodies over 1 MiB are rejected; video uploads are limited to `MAX_VIDEO_MB`.
