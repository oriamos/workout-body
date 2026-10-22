# Body weight (weigh-ins)

A daily body-weight log next to the workout and food logs. You usually report
your weight to the assistant ("82.4 this morning"); it calls `log_weigh_in` for
today in your timezone. When you ask how it is going, the assistant reads the
weekly trend: the **weekly average** is the signal, single days fluctuate.

Not included, by design: body-fat or other measurements, goals or target
weights, projections, BMI, calorie or medical advice.

## Data model and units

**Weigh-in** — the body weight recorded for one local date:

| Field | Type | Rules |
|---|---|---|
| `id` | integer | Stable for the date (kept when the value is replaced). |
| `date` | `YYYY-MM-DD` | The **local date** in the configured timezone (`WORKOUT_TIMEZONE`, default `Asia/Jerusalem`). Defaults to today; past dates allowed; future dates rejected. |
| `weight_kg` | number | Kilograms, 20–400, stored rounded to 0.01. There is no pounds field: clients convert (1 lb = 0.45359237 kg). |
| `note` | string | Optional, at most 200 characters, trimmed; omitted from JSON when empty. |
| `revision` | integer | Starts at 1, increments when the value or note changes. |
| `created_at`, `updated_at` | RFC 3339 UTC | Audit timestamps only. |

## Rules

* **One weigh-in per local date.** Logging a date that already has one
  **replaces** it (weight and note; an omitted note clears the old one). There
  is never a second row for a day.
* **Retries are harmless.** Logging the same weight and note again changes
  nothing: no new row and no revision bump. That is why weigh-ins need no
  idempotency key.
* **Optional revision check.** A write may carry the expected revision
  (`If-Match` over HTTP, `expected_revision` over MCP); `0` means "the date must
  not have a weigh-in yet". A mismatch is rejected and nothing changes.
  Deleting over HTTP requires `If-Match`.
* Invalid input (weight outside 20–400, not a finite number, a future or
  impossible date, a note over 200 characters) is rejected with field-addressed
  errors and nothing is stored.
* Body weight lives in its own table. Logging it never creates or touches a
  workout or a food entry.

## Weekly math

`WeightTrendFor(to, weeks)` (HTTP `GET /weight/trend`, MCP `get_weight_trend`)
returns the last `weeks` weeks (default 8, at most 104) ending with the week that
contains `to` (default today), oldest first.

* Weeks run **Sunday to Saturday** by default (or Monday to Sunday with `WORKOUT_WEEK_START=monday`), like the rest of the engine.
* For each week: `days_logged`, and over **only the days that have a weigh-in**
  `average_kg` (rounded to 0.01), `min_kg`, `max_kg`. A week without any
  weigh-in has `days_logged: 0` and `null` statistics. Missing days are never
  filled in or interpolated.
* `change_kg` = this week's average − the previous week's average (rounded to
  0.01); `change_pct` = `change_kg` / previous average × 100 (rounded to 0.01).
  Both are `null` when either week has no weigh-in. The first week in the range
  is compared with the week before the range, which is read for that purpose.
* `trend` is `up` (change ≥ 0.1 kg), `down` (≤ −0.1 kg) or `flat`; omitted when
  there is no change.
* `entries` are the weigh-ins in the range, `latest` is the most recent weigh-in
  on or before `to` (it may be older than the range, or `null`), and
  `overall_change_kg` is the last week with data minus the first week with data
  in the range (`null` with fewer than two such weeks).
* Everything is computed from the stored weigh-ins on every read, so edits and
  deletes are reflected immediately.

## HTTP API

All under `/api/v1`, bearer-authenticated like the rest. Full schemas in
[`internal/apisrv/openapi.yaml`](../internal/apisrv/openapi.yaml); conventions
(errors, `If-Match`) in [http-api.md](http-api.md). `{date}` accepts
`YYYY-MM-DD` or `today`.

| Method and path | Purpose |
|---|---|
| `GET /weight/entries?from&to` | `{entries: [...]}`, oldest first; both bounds optional |
| `GET /weight/entries/{date}` | The weigh-in, with `ETag: "<revision>"`; `404` if none |
| `PUT /weight/entries/{date}` | Body `{weight_kg, note?}` (strict). `201` + `Location` when created, `200` when replaced or unchanged; `ETag`. `If-Match` optional: a revision, `"0"` = must not exist, `*` = must exist; stale → `412` |
| `DELETE /weight/entries/{date}` | `If-Match` required (`428` without, `412` stale) → `204`; `404` if none |
| `GET /weight/trend?to&weeks` | The weekly trend described above |

A store without weigh-in support answers `501 weight_disabled` on every
endpoint (the bundled SQLite store supports it).

```sh
curl -X PUT "$URL/api/v1/weight/entries/today" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"weight_kg":82.4,"note":"morning"}'

curl "$URL/api/v1/weight/trend?weeks=4" -H "Authorization: Bearer $TOKEN"
```

## MCP tools

| Tool | Kind | Notes |
|---|---|---|
| `log_weigh_in {weight_kg, date?, note?, expected_revision?}` | destructive (replaces the day's value), idempotent | Returns `weigh_in`, `created`, `today`, `timezone` |
| `get_weigh_in {date?}` | read-only | `found: false` and no `weigh_in` when nothing was logged |
| `delete_weigh_in {date, expected_revision?}` | destructive, idempotent | The description tells the assistant to confirm first |
| `get_weight_trend {to?, weeks?}` | read-only | Same shape as the HTTP trend plus `timezone` |

Full descriptions and parameters: [mcp-tools.md](mcp-tools.md) (generated from
the code). The `weight-tracking` plugin skill
([SKILL.md](../plugin/workout-body/skills/weight-tracking/SKILL.md)) tells the
assistant to log reported weights for today in kg (converting pounds and saying
so), not to nag, to answer "how is it going" from the weekly average and
week-over-week change, and never to give medical advice.

## Example (synthetic data)

Weigh-ins on Tue 2026-01-06 (80.0), Thu 2026-01-08 (79.6) and Tue 2026-01-13
(79.2):

```text
GET /api/v1/weight/trend?to=2026-01-18&weeks=2
{
  "from": "2026-01-05", "to": "2026-01-18", "today": "…",
  "weeks": [
    {"week_start": "2026-01-05", "week_end": "2026-01-11", "days_logged": 2,
     "average_kg": 79.8, "min_kg": 79.6, "max_kg": 80,
     "change_kg": null, "change_pct": null},
    {"week_start": "2026-01-12", "week_end": "2026-01-18", "days_logged": 1,
     "average_kg": 79.2, "min_kg": 79.2, "max_kg": 79.2,
     "change_kg": -0.6, "change_pct": -0.75, "trend": "down"}
  ],
  "entries": [ … three weigh-ins … ],
  "latest": {"date": "2026-01-13", "weight_kg": 79.2, …},
  "overall_change_kg": -0.6
}
```

The first week has no change because the week before it has no data. After
replacing the 13th with 78.8 (`PUT` with `If-Match: "1"`) the second week reads
`average_kg: 78.8, change_kg: -1`; after deleting the 8th the first week's
average is 80 and the change is −1.2. This exact flow runs against the real
binary in `internal/e2e/weight_test.go`.

Through the assistant: "82.4 this morning" → `log_weigh_in{weight_kg: 82.4}`;
"I'm 181 lb today" → `log_weigh_in{weight_kg: 82.1}` and the reply says 181 lb
was converted to 82.1 kg; "how's my weight going?" → `get_weight_trend{}` and a
reply such as "this week's average is 82.3 kg over 4 weigh-ins, 0.4 kg lower than
last week".

## Storage, migration and backup

* Migration `014_weigh_ins.sql` adds the `weigh_ins` table (`date` is
  `UNIQUE`). It runs automatically on start and touches no other table;
  databases from earlier versions upgrade in place with an empty weight log.
* Weigh-ins live in the same SQLite file, so `training-mcp backup` and the
  restore procedure ([backup-restore.md](backup-restore.md)) include them.

## Known limitations

* One value per day: a second weigh-in the same day replaces the first (no
  morning/evening pairs). The time of day is not stored.
* Kilograms only in storage and APIs; unit conversion is the client's job.
* No smoothing beyond the plain weekly mean (no moving averages or
  projections); a week with one weigh-in has the same weight in the comparison
  as a week with seven — `days_logged` says how much data stands behind it.
* The 20–400 kg range is a sanity limit, not advice.
