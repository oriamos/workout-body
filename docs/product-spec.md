# Product specification

## Purpose

An open-source, self-hosted tool for **one person** to manage their own
training: a deterministic engine that stores workouts, plans and history,
exposed to an AI assistant through MCP and to a future mobile UI through a
documented HTTP API. There are no teams, coaches, billing or organizations.

## Layers

1. **Engine** — business logic, validation, persistence, plans, workouts,
   exercises, sets, analytics. *Implemented.* No LLM dependency.
2. **MCP server and plugin** — the assistant interprets natural language and
   calls structured tools. *Implemented* (server, tools, instructions). Client
   packaging status: see [plugin-setup.md](plugin-setup.md).
3. **Custom UI** — mobile-first interface over the HTTP API. **Not built.** Out
   of scope for this phase; the API contract in [http-api.md](http-api.md) is
   what it will use. The upstream project's server-rendered PWA is still
   shipped (optional, off by default) but is not the future UI and is not
   extended.

Natural-language understanding lives only in the assistant. The engine receives
explicit values and rejects anything invalid.

## Confirmed requirements and where they are met

| Requirement | Status |
|---|---|
| Incremental logging: several updates through a conversation modify the same workout | Implemented — `log_sets`, one workout per date |
| One workout per local date, default today, created on first write, reused by later writes and other conversations | Implemented — unique index + atomic get-or-create |
| Explicit logging/corrections for other dates | Implemented (future dates rejected for sets) |
| Configurable timezone, default Asia/Jerusalem, date from that zone | Implemented — `WORKOUT_TIMEZONE` |
| No start/finish commands needed | Implemented |
| Same rules for MCP and HTTP | Implemented — both call `training.Service` |
| Concurrent first writes do not duplicate workouts | Implemented and tested |
| Core set data + optional RPE, RIR, set type, notes; partial records; missing ≠ 0 | Implemented — nullable columns, validated ranges |
| Plans: templates, planned sets/reps/rep ranges/target weight/RPE/RIR | Implemented |
| Plan JSON import/export with validation, atomic apply, schema and example | Implemented — [plan-format.md](plan-format.md) |
| Plan edits never rewrite history | Implemented — prescriptions are copied per workout |
| Create/update plans through MCP | Implemented |
| Individual set update/delete by stable id | Implemented |
| Retry safety without blocking identical sets | Implemented — idempotency keys |
| UI and conversation edits must not silently overwrite each other | Implemented — revisions / `If-Match` |
| Exercise history, progression, useful statistics | Implemented — [analytics.md](analytics.md) |
| Stable exercise identity and aliases; no silent merging | Implemented |
| Documented HTTP API with OpenAPI | Implemented |
| Rotating A/B/C vs weekday scheduling | **Undecided** — deliberately not modelled; a plan is a named routine and the user picks which to follow |

## Non-goals (this phase)

A custom frontend, in-app chatbot or LLM calls, multi-user accounts or RBAC,
OAuth, billing, cloud sync, coaching features, a schedule engine, a database
server or message broker.

## Units and conventions

Kilograms; RPE 1–10; RIR 0–10; dates are local calendar dates `YYYY-MM-DD`;
`null` means "not recorded"; times are never exposed for sets. Full rules:
[engine.md](engine.md).
