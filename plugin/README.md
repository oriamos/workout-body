# Client integrations

This directory holds client-side packaging for the workout-body MCP server.

- [`workout-body/`](workout-body/) - Claude Code plugin: MCP server entry,
  `workout-logging` skill, and the same instructions as plain text
  (`workout-body/assistant-instructions.md`) for clients without skills.
- [`.claude-plugin/marketplace.json`](.claude-plugin/marketplace.json) - a
  local marketplace listing that plugin, so it can be added from a checkout.

Setup instructions: [`workout-body/README.md`](workout-body/README.md).

## What is verified and what is not

The server supports exactly two transports: Streamable HTTP at `/mcp`
protected by **one static bearer token**, and stdio. It has no OAuth and no
per-user accounts.

### Verified (automated tests in `internal/mcpsrv`)

Run with `go test -race ./internal/mcpsrv`. Each scenario runs through the
official Go MCP SDK client (`github.com/modelcontextprotocol/go-sdk` v1.2.0)
against the real service on a temporary SQLite database:

- over in-memory transports and over Streamable HTTP behind the production
  bearer middleware (`internal/httpsrv`);
- over stdio, with the server running `RunStdio` in a child process;
- requests with a missing, wrong or non-Bearer `Authorization` header get
  HTTP 401, and the SDK client cannot connect without the right token;
- the four-message bench/squat logging scenario from the skill, idempotent
  retries, validation and conflict error text, plan import/export round trip
  with dry run, values left unrecorded, and tool annotations.

### Not verified

- **Claude Code plugin installation.** `plugin.json`, `.mcp.json` (including
  `${VAR}` expansion in the URL and header) and the marketplace file follow the
  documented formats but have not been installed or run in Claude Code.
- **Skill triggering.** Whether Claude Code loads `workout-logging` at the
  right moments has not been evaluated.
- **Claude Desktop.** Not tested. Local stdio configuration is the likely
  route, but it has not been tried.
- **Claude.ai custom connectors and ChatGPT connectors/apps.** Not tested, and
  likely incompatible as-is: these typically require a public HTTPS endpoint
  with OAuth, or no authentication at all. This server only accepts a static
  bearer token, which such connectors may have no field for. Running it with
  authentication disabled on a public URL would expose the whole log to anyone
  who finds it; do not do that.
- **Other MCP clients.** Any client that can send a custom `Authorization`
  header over Streamable HTTP, or launch a stdio command, should work in
  principle; none has been tested.

`assistant-instructions.md` is client-neutral text; pasting it into a client
does not by itself make that client able to reach the server.
