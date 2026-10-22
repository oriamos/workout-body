# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for security problems. Use GitHub's
private vulnerability reporting ("Security" tab → "Report a vulnerability") on
this repository. Include what you found, how to reproduce it, and the version or
commit. You should get an acknowledgement within a few days. Fixes are released
as soon as practical and credited unless you prefer otherwise.

## Scope and threat model

This is a **single-user, self-hosted** service. It is designed to run on a
machine you control, behind HTTPS.

* Authentication is **one static bearer token** (`MCP_BEARER_TOKEN`), compared in
  constant time. It guards `/mcp` and `/api/v1`. There is no OAuth, no user
  management and no token rotation: rotate by changing the variable and
  restarting.
* `MCP_AUTH_DISABLED=1` disables authentication entirely. Use it only on a
  loopback-only development machine.
* The server speaks plain HTTP. Terminate TLS in front of it (reverse proxy,
  tunnel) for anything beyond localhost, otherwise the token travels in the
  clear.
* The built-in web UI (served at `/` when built into the binary) is gated by a
  login page. Logging in with the bearer token sets the `wb_session` cookie:
  HttpOnly, `SameSite=Strict`, `Secure` over HTTPS (also behind a proxy that sets
  `X-Forwarded-Proto: https`), valid 180 days. Its value is an HMAC of the token,
  so the browser never stores the token and changing the token signs every
  browser out. The cookie is accepted by `/api/v1` only, never by `/mcp`;
  cookie-authenticated state changes marked cross-site (`Sec-Fetch-Site`) are
  refused. Failed logins are delayed. The UI pages carry a strict
  Content-Security-Policy, `X-Frame-Options: DENY` and `nosniff`.
* The optional upstream PWA (`WEB_BASE_PATH`) is protected by the same bearer
  token *and* a secret URL prefix, has no CSRF protection, and is off by default.
  Do not expose it to the internet without understanding that.
* The API sets no CORS headers: it is same-origin only. A browser UI served from
  another origin needs a reverse proxy that adds CORS and holds the token
  server-side; do not embed the token in client-side code you distribute.
* JSON and MCP request bodies are limited to 1 MiB; video uploads to `MAX_VIDEO_MB` (default 200 MB) and only on the video endpoints.

Never commit tokens, `.env` files, database files or real training data. `.gitignore`
excludes them; keep it that way.
