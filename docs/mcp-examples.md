# MCP examples

The four-message flow below is what the end-to-end acceptance test
(`internal/e2e`) runs against the real binary; the other calls are covered by
`internal/mcpsrv` (in memory and over Streamable HTTP). The assistant — not
the server — turns your sentences into these structured calls; the server only
sees explicit values. Full parameter reference: [mcp-tools.md](mcp-tools.md).

## The four-message workout

**1. "Bench press, first set: 8 reps at 60 kg. Planning 3 sets."**

```json
{ "tool": "log_sets", "arguments": {
    "sets": [ { "exercise": "bench press", "weight_kg": 60, "reps": 8 } ],
    "prescriptions": [ { "exercise": "bench press", "target_sets": 3 } ] } }
```

No date: the server uses today in the configured timezone and creates the
workout (`"created": true`). The result returns the persisted set with its stable
`id`, `set_number: 1` and `revision`. The prescription is only the *plan*; no
second or third set exists yet.

**2. "Second set, same."** The assistant resolves "same" from context (or from
`get_workout`) into explicit values:

```json
{ "tool": "log_sets", "arguments": {
    "sets": [ { "exercise": "bench press", "weight_kg": 60, "reps": 8 } ] } }
```

Same workout (`"created": false`), new set id, `set_number: 2`.

**3. "Third set, same. Starting squats: 130 kg, 4 reps, planning 3 sets."**
The third bench set is performed; the squats are *planned only*:

```json
{ "tool": "log_sets", "arguments": {
    "sets": [ { "exercise": "bench press", "weight_kg": 60, "reps": 8 } ],
    "prescriptions": [ { "exercise": "squat", "target_sets": 3,
                         "rep_min": 4, "rep_max": 4, "target_weight_kg": 130 } ] } }
```

Squats now have `target_sets: 3, rep 4–4, target_weight_kg: 130` and
`done_sets: 0`. Nothing is marked completed without evidence.

**4. "Actually, bench set two was 7 reps."** Corrected by the id returned in step
2; other sets are untouched:

```json
{ "tool": "update_set", "arguments": { "set_id": 2, "reps": 7 } }
```

`get_workout` (or `GET /api/v1/workouts/today`) now shows bench sets of 8, 7 and
8 reps.

## Reading the workout

```json
{ "tool": "get_workout", "arguments": {} }
```

Returns `exists`, the timezone and local date, the sets with `id`,
`set_number` and `revision`, and `progress` per exercise (`target_sets`,
`done_sets`, `warmup_sets`, targets). It never creates a workout.

## Partial data and clearing a value

Only the exercise is required. Never guess a missing number — leave it out:

```json
{ "tool": "log_sets", "arguments": {
    "sets": [ { "exercise": "pull-up", "reps": 10 } ] } }
```

`weight_kg` is stored as `null` (not recorded), which is different from an
explicit `0`. To turn a recorded value back into "not recorded":

```json
{ "tool": "update_set", "arguments": { "set_id": 7, "clear": ["rpe"] } }
```

## Warm-ups and optional metrics

```json
{ "tool": "log_sets", "arguments": { "sets": [
    { "exercise": "squat", "weight_kg": 60, "reps": 5, "set_type": "warmup" },
    { "exercise": "squat", "weight_kg": 130, "reps": 4, "rpe": 8, "rir": 2,
      "notes": "felt fast" } ] } }
```

Warm-ups are stored but excluded from `done_sets`, volume and records.

## Retrying safely

If a call may not have arrived, repeat it with the **same** `idempotency_key`:

```json
{ "tool": "log_sets", "arguments": {
    "idempotency_key": "2026-03-10-bench-set-4",
    "sets": [ { "exercise": "bench press", "weight_kg": 60, "reps": 8 } ] } }
```

If the first call succeeded, the second returns the same set ids with
`"replayed": true` and writes nothing. Use a **new** key for a genuinely new set;
two identical sets without a key (or with different keys) are both stored.

## A past date

```json
{ "tool": "log_sets", "arguments": { "date": "2026-03-08",
    "sets": [ { "exercise": "deadlift", "weight_kg": 140, "reps": 5 } ] } }
```

Future dates are rejected for performed sets.

## Plans

Import (validate first with `dry_run`):

```json
{ "tool": "import_plan", "arguments": { "dry_run": true, "plan": {
    "format": "workout-plan", "version": 1, "name": "Upper A",
    "items": [ { "exercise": "bench press", "sets": 4, "rep_min": 5, "rep_max": 8,
                 "target_weight_kg": 70, "target_rpe": 8 } ] } } }
```

Follow a plan for today: pass its id to `log_sets` (`"plan_id": 1`) or
`start_session`. Export with `export_plan`; replace a plan's contents with
`replace_plan` (guard with `expected_revision`). Format and import rules:
[plan-format.md](plan-format.md).

## Statistics

```json
{ "tool": "exercise_stats", "arguments": { "exercise": "bench press" } }
{ "tool": "training_summary", "arguments": { "from": "2026-03-01", "to": "2026-03-31" } }
```

Definitions: [analytics.md](analytics.md).

## Errors you will see

A rejected write changes nothing, and says exactly what to fix:

```
validation error: sets[1].rpe: must be between 1 and 10; omit it if unknown;
prescriptions[0].target_sets: must be between 1 and 20, got 0.
Nothing was written. Fix these fields and call the tool again; never fill a
missing value with a guess - omit it instead
```

A stale `expected_revision` produces a conflict telling the assistant to re-read
the record and retry.
