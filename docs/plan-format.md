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

Omitted means *not prescribed* — never zero. Unknown fields are rejected, so a
typo like `target_wieght_kg` is an error instead of being silently ignored.

Items are performed in the order listed. Whether routines rotate (A/B/C) or
follow weekdays is deliberately not modelled: a plan is just a named routine, and
you choose which plan to follow on a given date.

## Import semantics

1. **Validate first.** The whole document is parsed and validated, collecting
   *every* problem with its JSON path (`items[2].target_rir: must be between 0 and 10`).
   If anything is wrong nothing is written — no plan, no new exercises.
2. **Match by name**, case-insensitively.
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
