# Client integrations

This directory holds client-side packaging for the workout-body MCP server.

- [`workout-body/`](workout-body/) - Claude Code plugin: MCP server entry,
  `workout-logging` and `nutrition-logging` skills, and the same instructions as plain text
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
  with dry run, values left unrecorded, prescription revisions/clears, and
  tool annotations;
- exercise guides (via alias), instructions, listing and deleting set and
  exercise videos, video cleanup when a set is deleted, and the error text
  when media storage is disabled. Videos in tests are attached directly
  through the service with synthetic bytes; the HTTP upload endpoints belong
  to the API server and are not exercised by these tests;
- plan schedules: `create_plan`/`import_plan` (incl. dry run)/`export_plan`
  carrying a schedule, `set_plan_schedule` set/clear/stale-revision/invalid
  days, and `get_schedule` statuses, weekly tallies and default week;
- nutrition: adding meals and day totals, zero vs missing values, partial
  update and clear, copy and its independence, retry replay vs intentional
  duplicate, stale-revision conflicts for entries and targets, target changes
  that leave earlier dates intact, invalid values with field paths, future
  dates rejected, and no workout created by nutrition.

### Checked against the documentation, but not run

A review of the plugin files against the current Claude Code documentation found
them valid (not an installation):

- layout: `.claude-plugin/plugin.json` plus `.mcp.json` and `skills/` at the
  plugin root; only `name` is required in the manifest;
- `.mcp.json` uses the `{"mcpServers": {...}}` wrapper, `"type": "http"` and
  `headers`; `${VAR}` expansion is documented for `url` and `headers`;
- the marketplace schema (relative `./workout-body` resolves from the directory
  that contains `.claude-plugin/`);
- Claude Code supports a static `Authorization` header without OAuth.

Caveat from that review: if `WORKOUT_MCP_URL` or `WORKOUT_MCP_TOKEN` is **not
exported**, the config still loads and a literal `Bearer ${WORKOUT_MCP_TOKEN}`
is sent, which the server rejects with 401. `claude mcp list` shows a warning.
Run `claude plugin validate ./plugin` and `claude plugin validate
./plugin/workout-body` to check the files.

### Not verified (never run)

- **Installing the plugin in Claude Code** and whether the `workout-logging`
  and `nutrition-logging` skills trigger at the right moments.
- **Claude.ai and Claude Desktop custom connectors.** Anthropic's connector
  documentation describes custom *request headers* for fixed credentials such as
  a bearer token, so this server's static token is a documented fit. But the
  connector is added in claude.ai settings (not through this plugin's
  `.mcp.json`), it is brokered from Anthropic's cloud so it needs a **publicly
  reachable HTTPS URL**, and the header option may not be available on every
  account. None of this has been tried against this server.
- **ChatGPT connectors/apps** and **other MCP clients.** Untested. Any client
  that can send a custom `Authorization` header over Streamable HTTP, or launch
  a stdio command, should work in principle.
- Exposing the server publicly. Never run it with authentication disabled on a
  public URL: that would expose the whole training log to anyone who finds it.

`assistant-instructions.md` is client-neutral text; pasting it into a client
does not by itself make that client able to reach the server.
