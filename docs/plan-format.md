# Plan JSON format (version 1)

A plan is a reusable routine. It describes what is **planned**; importing it
never creates performed sets. Machine-readable schema:
[`schemas/workout-plan.v1.schema.json`](schemas/workout-plan.v1.schema.json).
A realistic synthetic example: [`examples/plan-upper-lower.json`](examples/plan-upper-lower.json).

```json
{
  "format": "workout-plan",
  "version": 1,
  "name": "Upper A",
  "notes": "Heavy upper-body day.",
  "items": [
    { "exercise": "bench press", "sets": 4, "rep_min": 5, "rep_max": 8,
      "target_weight_kg": 70, "target_rpe": 8, "notes": "Pause on the chest." },
    { "exercise": "barbell row", "sets": 4, "rep_min": 6, "rep_max": 10, "target_rir": 2 }
  ]
}
```

## Fields

Top level: `format` (must be `"workout-plan"`), `version` (must be `1`), `name`
(1–100 chars), `notes` (≤ 2000), `items` (1–40).

Per item:

| Field | Required | Rules |
|---|---|---|
| `exercise` | yes | Name or alias, 1–100 chars. Normalized (lower case, collapsed spaces). Unknown names create an exercise. Each exercise at most once per plan. |
| `sets` | yes | Planned set count, 1–20. |
| `rep_min`, `rep_max` | no | 1–1000. A rep range uses both; a fixed target sets both equal. `rep_min ≤ rep_max`. Omit for "not prescribed". |
| `target_weight_kg` | no | 0–2000 kg. |
| `target_rpe` | no | 1–10. |
| `target_rir` | no | 0–10. |
| `superset` | no | Items sharing a label run back to back. ≤ 40 chars. |
| `notes` | no | ≤ 2000 chars. |

Omitted means *not prescribed* — never zero. An explicit `0` for `rep_min`, `rep_max` or `target_rpe`, and `null` for any field, are rejected (leave the field out). Whole numbers (`sets`, `rep_min`, `rep_max`) must be written as integers: `4`, not `4.0`. Unknown fields are rejected, so a
typo like `target_wieght_kg` is an error instead of being silently ignored.

Items are performed in the order listed.

## Schedule (optional)

A plan can say when it is meant to be done. Two forms, both optional and
mutually exclusive:

```json
"schedule": { "type": "weekdays", "days": ["mon", "thu"] }
"schedule": { "type": "times_per_week", "times_per_week": 3 }
```

| Form | Meaning |
|---|---|
| `weekdays` | Fixed days of the week. `days` is 1–7 distinct values from `mon tue wed thu fri sat sun`. The server also accepts full names and any case (`"Thursday"`) and stores them canonically, Monday first. |
| `times_per_week` | A weekly frequency, 1–7, on whichever days you choose. |

Days are the **local** weekday in the configured timezone; the week runs
Monday–Sunday. A rotation such as A/B/C is expressed by giving each plan its own
days (A: mon, B: wed, C: fri) or its own frequency. Several plans may share a
day.

A schedule is information about the plan. It **never creates or changes a
workout** — you (or the assistant) still start the day's workout, usually by
passing the plan's id when logging. What the schedule gives you is the
[schedule view](http-api.md#schedule) (`GET /api/v1/schedule`, MCP
`get_schedule`): which plans fall on which dates, whether each day was done, and
how many sessions of a weekly-frequency plan remain this week. Changing or
clearing a schedule never touches workouts already created.

Validation reports every problem with its path (`schedule.days[1]: "funday" is
not a weekday`). `null` is not accepted: leave the field out for an unscheduled
plan. **On import with `on_conflict=replace`, a document without `schedule`
clears the existing plan's schedule**, exactly as it replaces the exercises.

## Import semantics

1. **Validate first.** The whole document is parsed and validated, collecting
   *every* problem with its JSON path (`items[2].target_rir: must be between 0 and 10`).
   If anything is wrong nothing is written — no plan, no new exercises.
2. **Match by name**, case-insensitively (plan names are unique).
   * No plan with that name → a **new plan is created**.
   * A plan with that name exists → depends on `on_conflict`:
     * `error` (default): rejected with a conflict; nothing changes.
     * `replace`: the existing plan keeps its id; its notes and **all** its
       exercises are replaced and its `revision` increments.
3. Everything happens in one transaction; a failure part-way rolls it all back.
4. `dry_run: true` runs the same logic and reports what would happen (created
   vs. replaced, exercises that would be created) without writing.
5. History is untouched. Workouts that already followed the plan keep their own
   copy of the prescription and the old plan name.

**Export** returns the same format, so `export → import(on_conflict=replace)` is
idempotent.

Entry points: MCP `import_plan` / `export_plan` / `replace_plan`; HTTP
`POST /api/v1/plans/import`, `GET /api/v1/plans/{id}/export`,
`PUT /api/v1/plans/{id}`.
