# workout-body Claude Code plugin

Connects Claude Code to a personal workout-body MCP server (the engine v2
`training-mcp` binary in this repository) and adds a `workout-logging` skill
that tells the assistant how to log sets safely: read before guessing, never
invent values, keep planned and completed sets apart, correct by set id, and
retry writes idempotently. It also covers exercise guides (answer "how do I
do X" from the stored instructions, or say none exist) and videos (uploaded
through the app/HTTP API, never through MCP; the assistant sees metadata only).
Prescriptions use `target_sets` for the planned set count; only plan documents
(`import_plan`/`replace_plan`/`export_plan`) use `sets`. Plan schedules
("Mondays and Thursdays", "three times a week") are read with `get_schedule`
and set with `set_plan_schedule`; a schedule never creates a workout.
The `nutrition-logging` skill covers food entries (calories in kcal, macros in
grams for the whole portion): explicit values, clearly labelled estimates,
repeating a meal by finding it and calling `copy_nutrition_entry`, corrections
by entry id with `expected_revision`, and targets set only when the user asks.

Verification status is in [`../README.md`](../README.md). In short: the MCP
server's tools are tested with the Go MCP SDK client; installing this plugin
in Claude Code has **not** been verified.

## Contents

| Path | Purpose |
|---|---|
| `.claude-plugin/plugin.json` | Plugin manifest. |
| `.mcp.json` | MCP server entry: Streamable HTTP with a static bearer token. |
| `skills/workout-logging/SKILL.md` | Assistant instructions, loaded as a skill. |
| `skills/nutrition-logging/SKILL.md` | Food/macro logging instructions, loaded as a skill. |
| `assistant-instructions.md` | Both skills as plain text, for clients without skills. |

## HTTP setup (default)

The server is a single-user service protected by one static bearer token
(`MCP_BEARER_TOKEN` on the server). It listens on `/mcp`.

Set two environment variables before starting Claude Code:

```sh
export WORKOUT_MCP_URL="https://your-host.example/mcp"   # full URL including /mcp
export WORKOUT_MCP_TOKEN="the server's MCP_BEARER_TOKEN"
```

`.mcp.json` references them as `${WORKOUT_MCP_URL}` and `${WORKOUT_MCP_TOKEN}`
and sends `Authorization: Bearer ${WORKOUT_MCP_TOKEN}`. The token never needs
to be written into a file in this repository. Use HTTPS for anything that
leaves your machine: the bearer token is the only protection.

Install from this directory (a local marketplace is defined in
`plugin/.claude-plugin/marketplace.json`):

```text
/plugin marketplace add ./plugin
/plugin install workout-body@workout-body-local
```

These commands follow the Claude Code plugin documentation; they have not been
run against this plugin (see verification status).

## Stdio alternative (local, no network)

If the binary and the database are on the same machine as the client, the
server can speak MCP over stdin/stdout instead of HTTP (`training-mcp --stdio`). Replace the entry in `.mcp.json` with:

```json
{
  "mcpServers": {
    "workout-body": {
      "type": "stdio",
      "command": "/absolute/path/to/training-mcp",
      "args": ["--stdio"],
      "env": {
        "TRAINING_DB_PATH": "/absolute/path/to/training.db",
        "WORKOUT_TIMEZONE": "Asia/Jerusalem"
      }
    }
  }
}
```

- `TRAINING_DB_PATH` is the SQLite file (default
  `~/.local/share/training-mcp/training.db`).
- `WORKOUT_TIMEZONE` is the IANA timezone that decides which calendar date
  "today" is (default `Asia/Jerusalem`). `get_workout` reports it.
- No `MCP_BEARER_TOKEN` is needed in stdio mode: the client launches the
  process over a pipe, so there is no network endpoint to protect.
- Prefer one process per database file. SQLite's locking (WAL, 10 s busy
  timeout) is meant to cope with a second process on a local disk, but running
  a stdio instance beside the HTTP server on the same file is not tested.

## Clients without skills

Paste `assistant-instructions.md` into the client's custom instructions or
system prompt field and connect the client to the MCP server by whatever means
it supports. Whether a given client can connect at all is a separate question:
see the verification status.
