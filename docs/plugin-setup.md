# Connecting an assistant (MCP and plugin)

The server exposes the engine as MCP tools ([mcp-tools.md](mcp-tools.md)). The
assistant interprets your messages; the server only receives explicit values.
This page covers transports, authentication, and what has and has not been
verified for each client.

## What the server speaks

| Transport | How | Auth |
|---|---|---|
| **Streamable HTTP** | `POST /mcp` on the server (`--addr`, default `:8080`), stateless | `Authorization: Bearer <MCP_BEARER_TOKEN>` — one static token. No OAuth, no discovery endpoints. |
| **stdio** | `training-mcp --stdio`; the client launches the process | None needed (a pipe to a local process). Logs go to stderr; stdout carries the protocol. |

## Claude Code

The plugin lives in [`plugin/workout-body`](../plugin/workout-body/): an MCP
entry (`.mcp.json`), the `workout-logging`, `nutrition-logging` and
`weight-tracking` skills, and the same instructions as plain text.

```sh
export WORKOUT_MCP_URL="https://your-host.example/mcp"   # or http://localhost:8080/mcp
export WORKOUT_MCP_TOKEN="<the server's MCP_BEARER_TOKEN>"
claude plugin validate ./plugin/workout-body
claude plugin marketplace add ./plugin
claude plugin install workout-body@workout-body-local
```

Export both variables **before** starting Claude Code. If the token is unset the
config still loads and sends a literal `Bearer ${WORKOUT_MCP_TOKEN}`, which the
server answers with `401`.

Local, no network: replace the HTTP entry with a `stdio` entry running
`training-mcp --stdio` (example in the
[plugin README](../plugin/workout-body/README.md)).

You can also skip the plugin and add only the server (command form per the Claude
Code docs; not run here):
`claude mcp add --transport http workout-body "$WORKOUT_MCP_URL" --header "Authorization: Bearer $WORKOUT_MCP_TOKEN"`,
then paste [`assistant-instructions.md`](../plugin/workout-body/assistant-instructions.md)
into your project instructions.

## Other clients

* **claude.ai / Claude Desktop custom connectors:** Anthropic documents
  *request headers* for fixed credentials such as a bearer token, so this
  server's token is a documented fit. The connector is configured in claude.ai
  settings, not via the plugin, and is reached from Anthropic's cloud — so the
  server needs a **public HTTPS URL**. Not tried against this server.
* **ChatGPT and other clients:** untested. A client works if it can send a custom
  `Authorization` header over Streamable HTTP, or launch a stdio command. Many
  hosted connectors require OAuth or no authentication, which this server does
  not offer.
* Never expose the server publicly with `MCP_AUTH_DISABLED=1`.

Clients without skills support: paste
[`assistant-instructions.md`](../plugin/workout-body/assistant-instructions.md)
as custom instructions. That teaches behavior; it does not by itself connect the
client.

## Verification status

| Item | Status |
|---|---|
| Every tool via the official Go MCP SDK client, in memory and over Streamable HTTP with bearer auth (`internal/mcpsrv`); the acceptance flow and a subset of tools against the real binary (`internal/e2e`) | **Verified** |
| `--stdio` against the real binary, with no token | **Verified** (`internal/e2e`) |
| Missing/wrong/non-Bearer credentials rejected with 401 | **Verified** |
| Plugin files match the current Claude Code documentation (layout, `.mcp.json`, `${VAR}` expansion, marketplace schema) | **Checked against the docs**, not installed |
| Installing the plugin in Claude Code; the skill triggering at the right moments | **Not verified** |
| Claude Desktop, claude.ai connectors, ChatGPT | **Not verified** |

## What the instructions make the assistant do

Read the workout when context is insufficient; resolve "same as before" into
explicit values; keep planned prescriptions and completed sets apart; correct by
persisted set id; ask one brief question when ambiguity would materially change
stored data (for example a typo'd exercise that resembles an existing one); never
invent missing weight, reps or RPE; reuse the same `idempotency_key` when
retrying the same write and a new key for a new one; report success only after a
successful tool response; use the configured timezone for "today/yesterday";
confirm before destructive actions. Deterministic integrity (validation,
uniqueness, idempotency, conflicts) stays in the engine, not in the prompt.
