# History and analytics

All statistics are computed from **performed sets**. Planned prescriptions never
count.

## Inclusion rules

| Rule | Detail |
|---|---|
| Completed working set | Any set whose `set_type` is not `warmup` (so `working`, `backoff`, `drop`, `failure`). |
| Warm-ups | Counted separately (`warmup_sets`) and excluded from working sets, volume, bests, SI totals, weekly/muscle volume and load suggestions. |
| Load suggestions | `suggest_load` uses the newest *complete working* set: warm-ups, technique sets and sets missing weight or reps are skipped. |
| Missing values | A working set without weight or reps still counts as a set, but adds nothing to volume or 1RM; it is reported in `unrecorded_sets` so the gap is visible. |
| Dates | Workout local dates in the configured timezone. Ranges are inclusive. |

## Formulas

* **Volume (kg)** = Σ `weight_kg × reps` over working sets that have both values.
  A `weight_kg` of 0 contributes 0; a null weight contributes nothing and is
  counted as unrecorded.
* **Estimated 1RM** (Epley) = `weight × (1 + reps / 30)`, only for working sets
  with weight > 0 and `reps ≤ 12`. Beyond 12 reps the estimate is not produced:
  extrapolating further is not believable.
* **Max weight** = heaviest working set with a recorded weight.
* **Training week** starts on the configured day: Sunday by default, or Monday with `WORKOUT_WEEK_START=monday`. (The legacy SI-per-week view still uses Monday.)
* **SI** (inherited from upstream) = `max(0, round1(0.2 × RPE − 0.6))`, 0 without
  RPE. See [si-formula.md](si-formula.md).

## Operations

| Operation | MCP | HTTP | Returns |
|---|---|---|---|
| Exercise history | `exercise_history` | `GET /exercises/{ref}/history` | Newest-first sets with all fields, plus the personal record (best estimated 1RM, excluding warm-ups and technique sets). |
| Exercise stats | `exercise_stats` | `GET /exercises/{ref}/stats?from&to` | Per-date points (working/warm-up sets, reps, volume, top weight, best e1RM), totals, and bests (max weight, best e1RM, max reps) with the set that produced each. |
| Training summary | `training_summary` | `GET /stats/summary?from&to` | Workouts (dates with at least one completed working set; a warm-up-only day is not counted, so per-week totals add up), working/warm-up sets, volume, unrecorded sets; breakdowns by exercise, muscle group and week. Default range: the last 28 days ending today. |
| Planned vs completed | `get_workout` | `GET /workouts/{date}` | `progress[]` per exercise: `target_sets`, `done_sets`, `warmup_sets`, targets. |

## Limitations

* Muscle groups come from the upstream catalogue (one group per exercise).
  Unmapped exercises are reported under an empty group so gaps stay visible.
* Volume load ignores bodyweight and assisted movements (no body-mass model).
* e1RM estimates are descriptive only and never stored.
* Statistics are recomputed on read from raw sets (fine for a single user's
  history; no caches to go stale).
