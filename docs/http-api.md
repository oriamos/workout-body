# HTTP API reference

A JSON API over the same engine the MCP tools use. It is **implemented and
tested**, and the web UI in `web/` uses it.

* Machine-readable contract: [`internal/apisrv/openapi.yaml`](../internal/apisrv/openapi.yaml)
  (OpenAPI 3.1). The running server also serves it at `GET /api/v1/openapi.yaml`.
  A contract test fails if a route is registered but undocumented, or documented
  but missing, and every response in the test-suite is validated against the
  spec's schemas.
* Base path: `/api/v1`. JSON only (UTF-8), `snake_case` fields.
* Rules (validation, uniqueness, idempotency, concurrency) are the engine's:
  [engine.md](engine.md). This page is the HTTP view of them.

## Authentication

Every `/api/v1` route requires `Authorization: Bearer <MCP_BEARER_TOKEN>` — the
same single static token as `/mcp`. A missing, wrong or non-Bearer header gets
`401` with `WWW-Authenticate: Bearer`. Only `GET /health` (outside `/api/v1`) is
open. There are no users, scopes or CORS headers; a browser UI on another origin
must go through a same-origin proxy that holds the token. See
[SECURITY.md](../SECURITY.md).

## Dates and timezone

A workout is identified by its **local calendar date** `YYYY-MM-DD` in the
server's configured timezone (`WORKOUT_TIMEZONE`, default `Asia/Jerusalem`),
never the server's UTC date. `GET /config` returns `timezone` and `today`. The
path segment `today` is accepted wherever a date is (including `from`/`to`).
There is exactly one workout per date.

## Units and value conventions

* Weight: kilograms (0–2000). Reps: whole numbers 1–1000. RPE: 1–10. RIR: 0–10.
* **`null` means "not recorded", never zero.** An explicit `0` kg is a real value.
* **Absent vs `null`:** in a `POST` an omitted metric is simply not recorded. In
  a `PATCH` an omitted field is *unchanged*, a value sets it, and `null` clears
  `weight_kg`, `reps`, `rpe`, `rir` (and `target_weight_kg`/`target_rir` on
  prescriptions). `null` on any other field is a `400`.
* `set_type`: `warmup`, `working` (default), `backoff`, `drop`, `failure`.
  Warm-ups are excluded from completed-work statistics.
* Set identity is the integer `id`; `position` and `set_number` are derived
  ordering and change when sets are deleted.

## Endpoints

| Method and path | Purpose |
|---|---|
| `GET /config` | Timezone, today, media settings, API version |
| `GET /workouts?from&to&limit` | Workout summaries, newest first |
| `GET /workouts/{date}` | Workout with sets and planned-vs-completed `progress`; `404` if none (never creates) |
| `PUT /workouts/{date}` | Get or create the (empty) workout of a date; idempotent (`201` created, `200` existing) |
| `POST /workouts/{date}/sets` | Append sets and/or merge prescriptions; creates the workout on first write |
| `POST /workouts/{date}/plan` | Make the workout follow a plan |
| `PATCH` / `DELETE /workouts/{date}/prescriptions/{exercise}` | Adjust / remove one prescription |
| `GET` / `PATCH` / `DELETE /sets/{id}` | Read, correct, delete one set |
| `GET` / `POST /plans`, `POST /plans/import` | List, create, import plans |
| `GET` / `PUT` / `DELETE /plans/{id}`, `GET /plans/{id}/export` | Read, replace, delete, export |
| `PUT /plans/{id}/schedule` | Set or clear (`null`) a plan's fixed schedule; `If-Match` required |
| `GET /schedule?from&to` | Schedule view: what is due on which day, what was done, weekly tallies |
| `GET /exercises`, `POST /exercises`, `GET` / `PATCH /exercises/{ref}` | Exercises (with aliases); create (or get existing, near misses reported); rename |
| `POST /exercises/{ref}/aliases`, `DELETE …/aliases/{alias}` | Manage aliases |
| `GET /exercises/{ref}/history`, `…/stats` | Progression ([analytics.md](analytics.md)) |
| `GET /stats/summary?from&to` | Training overview |
| Videos: `POST`/`GET /sets/{id}/videos`, `POST`/`GET /exercises/{ref}/videos`, `GET`/`DELETE /media/{id}`, `GET /exercises/{ref}/guide`, `PUT /exercises/{ref}/instructions` | See [media.md](media.md) |
| `GET /openapi.yaml` | This contract |

`{ref}` is an exercise id, name or alias (URL-encode names).

### Logging example

```sh
curl -X POST "$URL/api/v1/workouts/today/sets" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -H "Idempotency-Key: 2026-03-10-bench-1" \
  -d '{
        "sets": [ { "exercise": "bench press", "weight_kg": 60, "reps": 8 } ],
        "prescriptions": [ { "exercise": "squat", "target_sets": 3,
                             "rep_min": 4, "rep_max": 4, "target_weight_kg": 130 } ]
      }'
```

`201` returns the persisted sets (with ids and `revision`), the merged
prescriptions, `created` (whether this call created the workout),
`new_exercises` and the updated workout. Appending is a pure addition, so
`If-Match` is optional on this route.

## Idempotency

Send `Idempotency-Key` on `POST /workouts/{date}/sets`. The first success is
`201`. A retry with the same key and body returns `200` with `replayed: true`,
the original set ids, and writes nothing. The same key with a different body is
`409`. Keys are shared with MCP (one namespace for the single user). Without a
key every call writes, so intentionally repeated identical sets stay possible.
Plan import needs no key: re-importing under `on_conflict=error` is a `409`, and
under `replace` it yields the same plan.

## Concurrency and conflicts

Workouts, sets and plans return a `revision` and an `ETag: "<revision>"`.

| Operation | Precondition |
|---|---|
| `PATCH`/`DELETE /sets/{id}`, `PUT`/`DELETE /plans/{id}`, `PATCH`/`DELETE …/prescriptions/…` | **`If-Match` required**: `428` if absent, `412` if stale |
| `POST /workouts/{date}/sets` | Optional `If-Match` (workout revision) |

A rejected write changes nothing. On `412` re-read the resource, merge, retry.
This is what stops a phone UI edit and a conversational edit from silently
overwriting each other.

## Errors

```json
{ "error": {
    "code": "validation_error",
    "message": "validation error: sets[1].reps: must be a whole number between 1 and 1000; omit it if unknown",
    "issues": [ { "field": "sets[1].reps", "message": "must be a whole number between 1 and 1000; omit it if unknown" } ] } }
```

| Status | `code` | When |
|---|---|---|
| 400 | `validation_error`, `bad_request` | Invalid values (every problem listed in `issues`), malformed or unknown JSON fields |
| 401 | — | Missing/wrong token. Answered by the authentication layer in front of the API: a plain-text `unauthorized` body with `WWW-Authenticate: Bearer`, **not** the JSON error shape |
| 404 | `not_found` | Unknown id, date, name |
| 405 | `method_not_allowed` | Known path, wrong method (`Allow` header set) |
| 409 | `conflict` | Idempotency-key reuse with another payload, plan-name collision, alias/rename collision, swap onto an already-prescribed exercise, per-owner video limit — regardless of any `If-Match` header |
| 412 | `precondition_failed` | Stale revision (the record changed since you read it) |
| 413 | `payload_too_large` | JSON body over 1 MiB, or a video over `MAX_VIDEO_MB` |
| 415 | `unsupported_media_type` | Wrong `Content-Type` |
| 428 | `precondition_required` | `If-Match` missing where required |
| 501 | `media_disabled` | Video storage not available |
| 500 | `internal` | Unexpected; details are logged, not returned |

Request bodies are decoded strictly: an unknown field is a `400` naming it, so a
typo never silently does nothing.

## Plan import and export

`POST /plans/import?on_conflict=error|replace&dry_run=true|false` takes a plan
document ([plan-format.md](plan-format.md)) and returns what happened
(`created` / `replaced`, the plan, exercises it created). `dry_run` writes
nothing. A failed import reports every issue with its JSON path and leaves no
partial plan. `GET /plans/{id}/export` returns the same format, so
`export → import(replace)` is idempotent. `PUT /plans/{id}` replaces an existing
plan in place (guarded by `If-Match`).

## Schedule

A plan may carry a fixed schedule (format and rules:
[plan-format.md](plan-format.md#schedule-optional)):

```json
{ "type": "weekdays", "days": ["mon", "thu"] }
{ "type": "times_per_week", "times_per_week": 3 }
```

It is part of the plan (returned as `schedule`, `null` when unscheduled), travels
in plan documents, and is changed with `PUT /plans/{id}/schedule`
(`{"schedule": {...}}` or `{"schedule": null}`, `If-Match` required).

`GET /schedule?from&to` (default: the current Monday–Sunday week in the server
timezone, at most 62 days) returns `days[]` with each date's `weekday`, whether
it is `today`, the `workout` stored on it (if any), and the weekday-scheduled
plans with a status — `done`, `started`, `missed`, `due` or `upcoming` (defined
in [engine.md](engine.md#plan-schedules)) — plus `weekly_targets[]` for
frequency plans (`target`, `completed`, `remaining`, `dates`) and
`unscheduled_plans[]`. It never creates a workout.

## Nutrition

`/nutrition/entries`, `/nutrition/days` and `/nutrition/targets` (entries with
idempotent create and copy, daily summaries against effective-dated targets) are
documented in [nutrition.md](nutrition.md#http-api) and in the OpenAPI spec.
They follow the same auth, date, `If-Match` and error rules as this page.

## Body weight

`/weight/entries` and `/weight/entries/{date}` (one weigh-in per local date;
`PUT` is an upsert keyed by the date, so `If-Match` is optional there and
required for `DELETE`) and `/weight/trend` (Monday-start weekly averages and
week-over-week change) are documented in [weight.md](weight.md#http-api) and in
the OpenAPI spec. Without store support they answer `501 weight_disabled`.

## Not provided

No pagination cursors (history is bounded by `limit`), no webhooks, no bulk
delete, no workout-level `DELETE` (use set deletes), no authentication beyond
the static token.
