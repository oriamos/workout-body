# Prompt: deploy workout-body on this Linux machine

Copy everything below the line into the agent that manages the server.

---

You are setting up **workout-body**, my personal, single-user workout / nutrition / weigh-in tracker, on this Linux machine. It is one Go server (JSON API + MCP endpoint + web UI with a login page) with a SQLite database, run with Docker Compose. The repository already contains everything needed; do not improvise a different setup. Report back to me in Hebrew.

## Rules
- Do not open any port to the internet and do not change the firewall/router. The app listens on 127.0.0.1:8080 only; public access (later) goes through a Cloudflare Tunnel.
- Never disable authentication (do not set MCP_AUTH_DISABLED).
- The token is a password. Never send it in chat, never print it in logs or command output you report, never commit it. Store it only in `deploy/.env` with mode 600.
- Do not delete or overwrite any existing data. If `/srv/workout-body` already exists, stop and tell me what is there.
- If something needs my action (GitHub access, Cloudflare login/token, sudo password), stop and ask me precisely what you need.
- Before each step that installs packages or changes system configuration, say what you are about to do.

## Steps
1. **Check the machine:** report distro and version, CPU architecture, free disk space, RAM. Check whether Docker Engine and the Docker Compose plugin are installed (`docker version`, `docker compose version`). If not, install them from Docker's official repository for this distro (docs.docker.com/engine/install), enable and start the service.
2. **Get the code:** clone `https://github.com/oriamos/workout-body` into `/srv/workout-body` and check out branch `claude/workout-engine-mcp-z2c025`. It is a private repository: if the clone is refused, stop and ask me how to give this machine read access (I can add a read-only deploy key or a fine-grained read-only token). Do not ask me to paste my GitHub password.
3. **Configure:** in `/srv/workout-body/deploy`:
   - `cp .env.example .env && chmod 600 .env`
   - set `MCP_BEARER_TOKEN` to the output of `openssl rand -hex 32` (write it into the file directly, e.g. with sed, without echoing it).
   - keep `WORKOUT_TIMEZONE=Asia/Jerusalem` and `WORKOUT_WEEK_START=sunday`; leave `CLOUDFLARE_TUNNEL_TOKEN` empty for now.
   - `mkdir -p data && sudo chown -R 65532:65532 data` (the container runs as UID 65532).
4. **Build and start:** `cd /srv/workout-body/deploy && docker compose up -d --build`. The image build compiles the web UI (Node) and the Go server; it can take several minutes the first time.
5. **Verify** and report each result:
   - `docker compose ps` shows `workout-body` running; `docker compose logs --tail 30 workout-body` contains `web UI served at /` and `listening on :8080`.
   - `curl -s http://127.0.0.1:8080/health` returns `{"status":"ok"}`.
   - `curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/api/v1/config` returns `401` (no token).
   - With the token read from `.env` inside the command (not printed): `curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $(grep ^MCP_BEARER_TOKEN= .env | cut -d= -f2)" http://127.0.0.1:8080/api/v1/config` returns `200`.
   - `curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' http://127.0.0.1:8080/` returns `303` to `/login`.
6. **Survive reboots:** confirm the Docker service is enabled at boot (`systemctl is-enabled docker`); the container has `restart: unless-stopped`.
7. **Backups:** install a root cron entry `15 3 * * * /srv/workout-body/deploy/backup.sh >> /var/log/workout-body-backup.log 2>&1`, run `sudo /srv/workout-body/deploy/backup.sh` once now, and report that a `training-*.db` file appeared in `deploy/data/backups/`. Tell me that backups stay on this same disk and suggest (do not set up without asking) copying `deploy/data/backups` to another place.
8. **Report back** (in Hebrew): what you installed, the results of every check above, the backup result, and how I can see the token myself on this machine: `sudo grep ^MCP_BEARER_TOKEN= /srv/workout-body/deploy/.env`. Do not include the token itself.

## Then: public HTTPS address (only after I confirm)
Ask me whether I have a domain in Cloudflare. When I give you a Cloudflare Tunnel token (Cloudflare dashboard → Zero Trust → Networks → Tunnels → Create tunnel → Docker; public hostname e.g. `workout.<my-domain>` → service `http://workout-body:8080`):
- put it in `deploy/.env` as `CLOUDFLARE_TUNNEL_TOKEN` (not echoed), then `docker compose --profile tunnel up -d`.
- verify `docker compose logs --tail 30 cloudflared` shows a registered connection, and from this machine `curl -s https://<hostname>/health` returns `{"status":"ok"}` and `https://<hostname>/` redirects to `/login`.
- tell me the address of the web UI (`https://<hostname>/`, log in with the token once) and the MCP endpoint for Claude: URL `https://<hostname>/mcp` with header `Authorization: Bearer <token>` (Claude Code: `claude mcp add --transport http workout-body https://<hostname>/mcp --header "Authorization: Bearer <token>"`).

## Updating later
When I ask you to update: `cd /srv/workout-body && git pull`, run `sudo deploy/backup.sh` first, then `cd deploy && docker compose up -d --build` (add `--profile tunnel` if the tunnel is in use), and repeat the checks in step 5. Database migrations run automatically on start.
