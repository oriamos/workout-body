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
| `TRAINING_DB_PATH` | `~/.local/share/training-mcp/training.db` | SQLite file. |
| `MEDIA_DIR` | `media/` beside the database (`/data/media` in Docker) | Uploaded videos. If it cannot be created the server keeps running with video storage disabled. |
| `MAX_VIDEO_MB` | `200` (1–4096) | Video upload size limit. |
| `WEB_BASE_PATH` | unset | Mounts the optional upstream PWA under this secret prefix. |
| `--addr` | `:8080` | Listen address. |
| `--stdio` | off | Serve MCP over stdin/stdout instead of HTTP (no network, no auth). |
| `backup <file>` / `media-gc` | — | Commands, see [backup-restore.md](backup-restore.md). |

`GET /health` is unauthenticated and returns `{"status":"ok"}`.

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
  `/mcp`, `/api/v1` and `/health`, and keep the app bound to localhost.
* Remote clients that cannot send a static `Authorization` header (some hosted
  connectors) cannot use this server directly — see
  [plugin-setup.md](plugin-setup.md).
* Do not enable `MCP_AUTH_DISABLED` on a network-reachable host.

## Operations

* One process, one SQLite file; do not run two instances on the same file.
* Upgrades: back up, deploy the new binary/image, start. Migrations run on
  startup ([migrations.md](migrations.md)).
* Logs go to stderr. JSON and MCP request bodies over 1 MiB are rejected; video uploads are limited to `MAX_VIDEO_MB`.
