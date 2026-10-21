# Engine and data model

The engine is the `internal/training` package (rules) plus `internal/sqlitestore`
(persistence). Both the HTTP API and the MCP server are thin adapters over
`training.Service`; neither contains business rules. The engine has no LLM
dependency: natural language is interpreted by the assistant, which sends the
engine explicit structured values.

## Concepts

| Concept | Meaning |
|---|---|
| **Workout** | The record for one local calendar date. Stored in the `sessions` table; "workout" and "session" are the same thing (the MCP/API surface says *workout*, some legacy tool names say *session*). |
| **Set** | One *performed* set. Has a stable integer `id`. |
| **Prescription** | What is *planned* for an exercise in a workout (sets, reps or rep range, optional weight/RPE/RIR). Stored in `session_items`. Never counts as performed work. |
| **Plan** | A reusable routine (`plans` + `plan_items`). Starting a workout from a plan **copies** its items into the workout. |
| **Exercise** | A stable identity (`exercises.id`) with a canonical name and optional aliases. |

Planned and performed data are separate tables, so a plan edit can never rewrite
history, and a prescription can never be mistaken for a completed set.

## One workout per local date

* The date is a calendar date `YYYY-MM-DD` in the **configured timezone**
  (`WORKOUT_TIMEZONE`, default `Asia/Jerusalem`). It is never the server's UTC
  date: at 22:30 UTC on 10 March it is already 00:30 on 11 March in Jerusalem.
* `sessions.date` has a `UNIQUE` index. Get-or-create is a single
  `INSERT … ON CONFLICT(date) DO NOTHING` followed by a read, inside one
  transaction, so simultaneous first writes (MCP and HTTP, or two
  conversations) all resolve to the same workout.
* Omitting the date means "today in the configured timezone". An explicit past
  date is allowed for logging and corrections. A **future** date is rejected for
  performed sets on every path (`log_sets`, and `add_set` into a workout that
  was opened for a future day) but accepted for prescriptions.
* There is no start/finish step: the first write creates the workout.

## Sets

| Field | Type | Rules |
|---|---|---|
| `id` | integer | Stable, never reused (`AUTOINCREMENT`). Use it to address a set. |
| `exercise_id`, `exercise` | integer, string | Resolved from a name/alias or an exact `exercise_id`. |
| `position` | integer | Order within the workout, dense `1..n`. Compacted after a delete, so it is **not** an identity. |
| `set_number` | integer | Derived on read: 1-based order among sets of the same exercise in that workout. |
| `weight_kg` | number or null | `0 … 2000`. **null = not recorded**; `0` = an explicit zero load (e.g. bodyweight). Never defaulted. |
| `reps` | integer or null | `1 … 1000`. null = not recorded. |
| `rpe` | number or null | `1 … 10`. null = not recorded. |
| `rir` | number or null | `0 … 10`. null = not recorded. RPE and RIR are stored independently; neither is derived from the other. |
| `set_type` | enum | `warmup`, `working` (default), `backoff`, `drop`, `failure`. |
| `notes` | string | ≤ 500 characters. |
| `technique` | string | Legacy free-text label (drop set, rest-pause, …), ≤ 40 characters. |
| `superset` | string | Copied from the workout's prescription when not given. |
| `si` | number | Legacy RPE-derived score `max(0, round1(0.2·RPE − 0.6))`. **0 when RPE is missing** (nothing is invented). |
| `revision` | integer | Starts at 1, increments on every change; used for optimistic concurrency. |

A set may be partial: only the exercise is required. All numeric fields are
independently optional so that "bench, 8 reps" can be stored without inventing a
weight. Units: **kilograms**, RPE on the 1–10 scale, RIR in reps.

Ranges and enumerations are enforced in the service (clear, field-addressed
messages) and, as a backstop, by `CHECK` constraints in SQLite. The database
bounds are deliberately looser than the service limits for weight and reps
(only `weight_kg >= 0` and `reps >= 1`): upstream never capped them, so a
legacy row above 2000 kg or 1000 reps must still migrate and load. The service
limits apply to everything written from now on.

## Prescriptions and progress

A prescription has `target_sets` (1–20), `rep_min`/`rep_max` (a fixed target has
both equal; 0 means unset), and optional `target_weight_kg`, `target_rpe`,
`target_rir`, `superset` and `notes`.

`progress` (returned with a workout) lists every prescribed exercise plus any
exercise performed without a prescription:

* `done_sets` counts completed **non-warm-up** sets.
* `warmup_sets` counts warm-ups separately.
* Planned sets never increment `done_sets`; only logged sets do.

Writing a prescription through the batch operation **merges**: fields that are
omitted keep their stored value, so "planning 4 sets" does not erase a previously
stored rep range or target weight. (The `add_session_exercise` tool, which sets a whole prescription, replaces.)

## Atomic batch logging

`Service.LogSets` applies, in **one transaction**: find-or-create the workout,
merge prescriptions, append sets, bump the workout revision and record the
idempotency key. If any part is invalid nothing is written — not even the
workout.

### Idempotency

* A request may carry an `idempotency_key` (≤ 128 characters).
* **Scope:** keys are global to the (single) user and shared by MCP and HTTP.
  A key is bound to its payload by a SHA-256 of the normalized request (the date
  is resolved first, so retry across midnight with an explicit date).
* **Same key, same payload:** nothing is written; the original sets and
  prescriptions are returned with `replayed: true`. The `workout` in the
  response is re-read, so it shows the workout as it is now.
* **Same key, different payload:** rejected with a conflict. Use a new key for a
  new write.
* **No key:** every call writes. Two identical sets sent without a key (or with
  different keys) are both stored — intentional identical sets stay possible.
* Keys are kept for 90 days.

### Optimistic concurrency

Workouts, sets and plans carry a `revision`. Callers may pass the revision they
last read (`expected_revision`, or `If-Match` over HTTP). If it no longer
matches, the write is rejected with a **stale-revision** error and nothing
changes, so a UI edit and a conversational edit never silently overwrite each
other. The HTTP API *requires* the precondition for updating or deleting a set,
a plan or a prescription; the MCP tools accept it optionally.

* Stale revisions are `ErrStale` (a kind of `ErrConflict`, HTTP 412). Other
  conflicts — idempotency-key reuse with another payload, a name collision —
  are plain `ErrConflict` (HTTP 409), whether or not a precondition was sent.
* Renaming an exercise changes what the affected sets, workouts and plans
  display, so it bumps their revisions too.
* Two deliberate exceptions, both explicit overwrites chosen by the caller:
  `import` with `on_conflict=replace` (overwrite-by-name; use `dry_run` first)
  and the granular plan-item tools (`set_plan_exercise` etc.), which bump the
  plan revision but take no precondition. Use `replace_plan` / `PUT /plans/{id}`
  when you want the guard.

## Exercise identity

* Names are normalized to lower case with collapsed whitespace — nothing more.
* Resolution order: exact `exercise_id` → canonical name → alias → create a new
  exercise.
* A near-miss name (`"bench pres"`) **creates a new exercise** and the response
  lists `new_exercises[].similar`, so the typo is visible and can be corrected
  (update the set with the right `exercise_id`, or add an alias). Exercises are
  never merged implicitly; an alias that equals an existing exercise name is
  refused, and so is a rename onto an existing name.
* Renaming an exercise updates sets, plans, prescriptions, muscle-group mapping
  and notes in one transaction.

## Plans

See [plan-format.md](plan-format.md) for the JSON format, import semantics and
the JSON Schema. Plan names are unique, compared case-insensitively
(creating, renaming or replacing onto an existing name is a conflict). Plans
have a `revision`; replacing or deleting one can be
guarded with it. Workouts started from a plan keep their own copy of the
prescription and a snapshot of the plan name, so renaming, replacing or deleting
the plan never changes a past workout.

## Persistence

* SQLite via `modernc.org/sqlite` (pure Go, no CGO).
* Connection: WAL journal, `busy_timeout` 10 s, foreign keys on, and
  `_txlock=immediate` so every transaction takes the write lock up front.
  Concurrent writers queue instead of failing on a read-to-write lock upgrade.
* Migrations: embedded SQL in `internal/sqlitestore/migrations`, applied once
  each in order inside a transaction and recorded in `schema_migrations`. See
  [migrations.md](migrations.md).

## Errors

The service returns sentinel errors that adapters map to protocol errors:

| Error | Meaning | HTTP | 
|---|---|---|
| `ErrValidation` (`*ValidationError` with `Issues[{field,message}]`) | Input rejected; every problem is listed with its path such as `sets[1].reps` | 400 |
| `ErrNotFound` | Unknown id/date/name | 404 |
| `ErrConflict` | Stale revision, idempotency key reuse with another payload, name collision | 409 / 412 |

## Legacy features inherited from upstream

Muscle-group catalogue, post-session feedback, volume landmarks, load
suggestions and the SI metric come from upstream Training MCP and are kept
(their messages are in Spanish). They operate on non-warm-up sets. See
`si-formula.md` and the upstream README (`UPSTREAM_README.md`).
