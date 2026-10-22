# MCP tool reference

<!-- GENERATED from the registered tools by internal/mcpsrv/docs_test.go. Do not edit by hand; run:
     go test ./internal/mcpsrv -run ToolReference -update-docs -->

This server registers 65 tools. Descriptions below are exactly what the model receives.

Worked examples with real calls and results: [mcp-examples.md](mcp-examples.md). Connection and plugin setup: [plugin-setup.md](plugin-setup.md).

Conventions that apply to every tool:

* Units: weight in **kilograms**, RPE 1–10, RIR 0–10.
* Dates are local calendar dates `YYYY-MM-DD` in the configured timezone (`WORKOUT_TIMEZONE`, default `Asia/Jerusalem`). Omitting a date means today there.
* Omitted/`null` metrics mean *not recorded*. Nothing is ever defaulted or guessed.
* `expected_revision` makes a write fail with a conflict if the record changed since you read it.
* `idempotency_key` makes a retried write safe: same key and payload replays the first result; same key with a different payload is a conflict.
* Validation errors list every problem with its field path, e.g. `sets[1].reps: …`.

| Tool | Kind |
|---|---|
| [`add_exercise_alias`](#add_exercise_alias) | write (idempotent) |
| [`add_nutrition_entry`](#add_nutrition_entry) | write |
| [`add_session_exercise`](#add_session_exercise) | destructive write |
| [`add_set`](#add_set) | write |
| [`adjust_session_exercise`](#adjust_session_exercise) | destructive write |
| [`copy_nutrition_entry`](#copy_nutrition_entry) | write |
| [`create_plan`](#create_plan) | write |
| [`delete_nutrition_entry`](#delete_nutrition_entry) | destructive write |
| [`delete_plan`](#delete_plan) | destructive write |
| [`delete_session`](#delete_session) | destructive write |
| [`delete_set`](#delete_set) | destructive write |
| [`delete_video`](#delete_video) | destructive write |
| [`delete_weigh_in`](#delete_weigh_in) | destructive write |
| [`exercise_history`](#exercise_history) | read-only |
| [`exercise_stats`](#exercise_stats) | read-only |
| [`export_plan`](#export_plan) | read-only |
| [`get_exercise_guide`](#get_exercise_guide) | read-only |
| [`get_nutrition_day`](#get_nutrition_day) | read-only |
| [`get_nutrition_entry`](#get_nutrition_entry) | read-only |
| [`get_nutrition_targets`](#get_nutrition_targets) | read-only |
| [`get_plan`](#get_plan) | read-only |
| [`get_schedule`](#get_schedule) | read-only |
| [`get_session`](#get_session) | read-only |
| [`get_set`](#get_set) | read-only |
| [`get_weigh_in`](#get_weigh_in) | read-only |
| [`get_weight_trend`](#get_weight_trend) | read-only |
| [`get_workout`](#get_workout) | read-only |
| [`import_plan`](#import_plan) | destructive write |
| [`list_exercise_groups`](#list_exercise_groups) | read-only |
| [`list_exercise_notes`](#list_exercise_notes) | read-only |
| [`list_exercises`](#list_exercises) | read-only |
| [`list_nutrition_entries`](#list_nutrition_entries) | read-only |
| [`list_plans`](#list_plans) | read-only |
| [`list_sessions`](#list_sessions) | read-only |
| [`list_set_videos`](#list_set_videos) | read-only |
| [`list_volume_landmarks`](#list_volume_landmarks) | read-only |
| [`log_set`](#log_set) | write |
| [`log_sets`](#log_sets) | write |
| [`log_weigh_in`](#log_weigh_in) | destructive write |
| [`move_plan_exercise`](#move_plan_exercise) | destructive write |
| [`record_feedback`](#record_feedback) | destructive write |
| [`remove_exercise_alias`](#remove_exercise_alias) | destructive write |
| [`remove_plan_exercise`](#remove_plan_exercise) | destructive write |
| [`remove_session_exercise`](#remove_session_exercise) | destructive write |
| [`rename_exercise`](#rename_exercise) | destructive write |
| [`replace_plan`](#replace_plan) | destructive write |
| [`save_session_as_plan`](#save_session_as_plan) | write |
| [`session_progress`](#session_progress) | read-only |
| [`set_exercise_group`](#set_exercise_group) | destructive write |
| [`set_exercise_instructions`](#set_exercise_instructions) | destructive write |
| [`set_exercise_note`](#set_exercise_note) | destructive write |
| [`set_nutrition_targets`](#set_nutrition_targets) | destructive write |
| [`set_plan_exercise`](#set_plan_exercise) | destructive write |
| [`set_plan_schedule`](#set_plan_schedule) | destructive write |
| [`set_volume_landmarks`](#set_volume_landmarks) | destructive write |
| [`start_session`](#start_session) | write (idempotent) |
| [`suggest_load`](#suggest_load) | read-only |
| [`swap_session_exercise`](#swap_session_exercise) | destructive write |
| [`training_summary`](#training_summary) | read-only |
| [`update_nutrition_entry`](#update_nutrition_entry) | destructive write |
| [`update_plan`](#update_plan) | destructive write |
| [`update_set`](#update_set) | destructive write |
| [`volume_by_muscle`](#volume_by_muscle) | read-only |
| [`volume_recommendation`](#volume_recommendation) | read-only |
| [`weekly_volume`](#weekly_volume) | read-only |

## add_exercise_alias

**Add exercise alias** — write (idempotent)

Make another name resolve to an existing exercise. An alias that is already an exercise name or belongs to another exercise is rejected as a conflict; aliases never merge exercises.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `alias` | string | yes | Other name that should resolve to this exercise, e.g. 'bp' for 'bench press'. |
| `exercise` | string | no | Current canonical name or alias of the exercise. |
| `exercise_id` | integer | no | Exact id of the exercise. Give exercise_id or exercise. |

## add_nutrition_entry

**Add nutrition entry** — write

Log one food or meal eaten. Returns the persisted entry (id, date, name, values, revision, created_at, updated_at) and replayed. Units: calories_kcal in kilocalories (kcal); protein_g, carbs_g and fat_g in grams. Values describe the WHOLE portion eaten, not per 100 g. Omit a value that is unknown: omitted means 'not recorded' and is never treated as 0; send 0 only for an explicit zero. Calories are stored exactly as given and never recomputed from macros. Dates are local calendar dates YYYY-MM-DD in the configured timezone (default Asia/Jerusalem); omit date for today. Food cannot be logged for a future date. Nutrition never creates or changes a workout. idempotency_key (optional, at most 128 characters, global to this single user): retrying with the same key and the same payload replays the stored entry (replayed=true) without writing again; the same key with a different payload is a conflict. A meal eaten twice on purpose needs no key or a new key.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `calories_kcal` | number | no | Energy of the whole portion in kcal, 0-20000. Omit if unknown. |
| `carbs_g` | number | no | Carbohydrate of the whole portion in grams, 0-2000. Omit if unknown. |
| `date` | string | no | Local date YYYY-MM-DD it was eaten. Omit for today. |
| `fat_g` | number | no | Fat of the whole portion in grams, 0-2000. Omit if unknown. |
| `idempotency_key` | string | no | Optional retry key. Same key + same payload replays; different payload is a conflict. |
| `name` | string | yes | What was eaten, e.g. 'chicken and rice, large plate'. 1-200 characters. |
| `protein_g` | number | no | Protein of the whole portion in grams, 0-2000. Omit if unknown. |

## add_session_exercise

**Set workout prescription** — destructive write

Add an exercise to one workout's prescription, or replace its prescription entirely (unlike log_sets prescriptions, which merge). Never creates completed sets and never edits the plan the workout came from. A plan prescribes what is planned (sets, rep range, optional target load in kg, target RPE 1-10, target RIR 0-10); omitted targets mean 'not prescribed'. Plans never create completed sets.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | yes | Exercise name or alias; normalized to lower case. |
| `notes` | string | no | Free-text intent: cues, tempo, whether to push the last set. |
| `rep_max` | integer | no | Upper bound of the target rep range, e.g. 12 for '10 to 12'. |
| `rep_min` | integer | no | Lower bound of the target rep range, e.g. 10 for '10 to 12'. |
| `session_id` | integer | yes | Id of the workout (session) to adjust. |
| `superset` | string | no | Label grouping exercises done back to back, e.g. 'A'. Omit for a standalone exercise. |
| `target_rir` | number | no | Prescribed reps in reserve 0-10. Omit if not prescribed. |
| `target_rpe` | number | no | Prescribed RPE 1-10. Omit if not prescribed. |
| `target_sets` | integer | yes | Planned number of sets, 1-20. |
| `target_weight_kg` | number | no | Prescribed load in kilograms, 0-2000. Omit if not prescribed. |

## add_set

**Add set by session id** — write

Append one completed set to an existing workout addressed by session id, returning the persisted set (with its id) and the workout's total SI. Prefer log_sets, which needs no id and supports idempotent retries. Units: weight_kg in kilograms; rpe is rate of perceived exertion 1-10; rir is reps in reserve 0-10. An omitted value means 'not recorded' and is stored as unknown: never invent, estimate or default a weight, reps, RPE or RIR the user did not state. weight_kg 0 means an explicit zero load, not unknown.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | no | Exercise name or alias. Give exercise or exercise_id. |
| `exercise_id` | integer | no | Exact id of an existing exercise; wins over exercise. |
| `notes` | string | no | Free-text note for this set. |
| `reps` | integer | no | Completed repetitions, 1-1000. Omit if not stated. |
| `rir` | number | no | Reps in reserve 0-10. Omit if not stated. |
| `rpe` | number | no | RPE 1-10. Omit if not stated. |
| `session_id` | integer | yes | Id of the existing workout (session). |
| `set_type` | string | no | warmup, working (default), backoff, drop or failure. |
| `technique` | string | no | Optional intensity technique, e.g. 'drop set'. |
| `weight_kg` | number | no | Load in kilograms, 0-2000. Omit if not stated. |

## adjust_session_exercise

**Adjust workout prescription** — destructive write

Change part of one exercise's prescription in a workout: target_sets, rep range, target load (kg), RPE 1-10, RIR 0-10, or whether it is skipped. Omitted fields stay as they are; use clear to remove a target load or RIR. Never creates completed sets. expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `clear` | array of string | no | Targets to reset to 'not prescribed': any of target_weight_kg, target_rir. Cannot be combined with a new value for the same field. |
| `exercise` | string | yes | Exercise already prescribed in this workout. |
| `expected_revision` | integer | no | The workout's revision from your last read (get_workout); rejected with a conflict if the workout changed since. |
| `rep_max` | integer | no | New upper bound of the rep range. Omit to leave unchanged. |
| `rep_min` | integer | no | New lower bound of the rep range. Omit to leave unchanged. |
| `session_id` | integer | yes | Id of the workout (session) to adjust. |
| `skipped` | boolean | no | Mark the exercise as consciously skipped today, or unmark it. |
| `target_rir` | number | no | New target reps in reserve 0-10. Omit to leave unchanged. |
| `target_rpe` | number | no | New target RPE 1-10. Omit to leave unchanged. |
| `target_sets` | integer | no | New planned set count, 1-20. Omit to leave unchanged. |
| `target_weight_kg` | number | no | New target load in kilograms, 0-2000. Omit to leave unchanged. |

## copy_nutrition_entry

**Copy nutrition entry** — write

Log an earlier meal again: creates a NEW, independent entry with the same name and values on date (today when omitted), with copied_from set. Editing or deleting the copy never changes the original, and vice versa. Find the original first with list_nutrition_entries or get_nutrition_day. Dates are local calendar dates YYYY-MM-DD in the configured timezone (default Asia/Jerusalem); omit date for today. Food cannot be logged for a future date. Nutrition never creates or changes a workout. idempotency_key (optional, at most 128 characters, global to this single user): retrying with the same key and the same payload replays the stored entry (replayed=true) without writing again; the same key with a different payload is a conflict. A meal eaten twice on purpose needs no key or a new key.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `date` | string | no | Local date YYYY-MM-DD for the copy. Omit for today. |
| `entry_id` | integer | yes | Id of the entry to copy. |
| `idempotency_key` | string | no | Optional retry key. Same key + same payload replays; different payload is a conflict. |

## create_plan

**Create plan** — write

Create a reusable workout plan: an ordered list of exercises, each with target_sets and optional rep range, target load, RPE and RIR, plus an optional schedule. A plan prescribes what is planned (sets, rep range, optional target load in kg, target RPE 1-10, target RIR 0-10); omitted targets mean 'not prescribed'. Plans never create completed sets. A schedule says when a plan is meant to be done: {type:'weekdays', days:[...]} for fixed days (mon..sun in the user's configured timezone) or {type:'times_per_week', times_per_week:N} for a frequency; the week starts on Monday. A schedule NEVER creates or changes a workout. To create from a plan document use import_plan.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `items` | array of object | yes | Ordered exercises; order is the order performed. Each exercise at most once. |
| `name` | string | yes | Name of the plan, e.g. 'Upper A'. |
| `notes` | string | no | Optional free-text notes about the plan. |
| `schedule` | object | no | Optional schedule: {type:'weekdays', days:['mon','thu']} or {type:'times_per_week', times_per_week:3}. Omit for an unscheduled plan. |

## delete_nutrition_entry

**Delete nutrition entry** — destructive write

Permanently delete one nutrition entry by id. Irreversible: confirm with the user first, naming the entry. expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `entry_id` | integer | yes | Id of the entry to delete. |
| `expected_revision` | integer | no | The entry's revision from your last read; rejected with a conflict if it changed since. |

## delete_plan

**Delete plan** — destructive write

Permanently delete a plan. Irreversible: confirm with the user first. Workouts that followed it keep their copied prescription and plan name. expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `expected_revision` | integer | no | The plan's revision from your last read; rejected with a conflict if it changed since. |
| `plan_id` | integer | yes | Id of the plan to delete. |

## delete_session

**Delete workout** — destructive write

Permanently delete a workout and every set in it. Irreversible: confirm with the user first. Returns how many sets were destroyed. A 'workout' and a 'session' are the same thing: there is exactly one per local calendar date.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | integer | yes | Id of the existing workout (session). |

## delete_set

**Delete set** — destructive write

Permanently delete one completed set by its stable set_id. Irreversible: confirm with the user first unless they explicitly asked for this deletion. Remaining sets keep their ids. expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `expected_revision` | integer | no | The set's revision from your last read; rejected with a conflict if it changed since. |
| `set_id` | integer | yes | Stable id of the set to delete. |

## delete_video

**Delete video** — destructive write

Permanently delete one video (of a set or of an exercise) and its stored file. Irreversible: confirm with the user first, naming the video (filename/caption and what it is attached to). Deleting a set or workout already removes its videos. Videos are uploaded only through the HTTP API (POST /api/v1/sets/{id}/videos for a performed set, POST /api/v1/exercises/{ref}/videos for an exercise how-to), typically from the app; there is no MCP upload tool. Tools see video metadata only (id, filename, content type, size, caption, date), never the video content: do not describe or judge what a video shows. Video storage can be disabled on the server; then these tools report that media storage is not configured.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `media_id` | integer | yes | Id of the video (from list_set_videos or get_exercise_guide). |

## delete_weigh_in

**Delete weigh-in** — destructive write

Permanently delete the weigh-in of one local date. Irreversible: confirm with the user first, naming the date and the weight. To correct a value, call log_weigh_in for that date instead (it replaces). expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `date` | string | yes | Local date YYYY-MM-DD of the weigh-in to delete. |
| `expected_revision` | integer | no | The weigh-in's revision from your last read; rejected with a conflict if it changed since. |

## exercise_history

**Exercise history** — read-only

Recorded sets of one exercise, newest first, each with its date, values (null = not recorded) and estimated 1RM, plus the best set by estimated 1RM.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | yes | Exercise name or alias. |
| `limit` | integer | no | Maximum sets to return; defaults to 50, accepts 1-500. |

## exercise_stats

**Exercise stats** — read-only

Progression for one exercise over an optional local date range: per-workout working sets, total reps, volume (kg x reps), top weight (kg) and best estimated 1RM, plus bests with the set that produced them. Warm-ups are counted separately and excluded from volume and bests. Sets missing weight or reps count as sets but add no volume (reported as unrecorded_sets); nothing is estimated for them.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | yes | Exercise name, alias or numeric id. |
| `from` | string | no | Inclusive earliest local date YYYY-MM-DD. Omit for no lower bound. |
| `to` | string | no | Inclusive latest local date YYYY-MM-DD. Omit for no upper bound. |

## export_plan

**Export plan** — read-only

Return a plan as a portable "workout-plan" version 1 document. Passing it unchanged to import_plan (or replace_plan) recreates the same prescriptions.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `plan_id` | integer | yes | Id of the plan. |

## get_exercise_guide

**Get exercise guide** — read-only

Read how to perform an exercise: the user's stored written instructions (empty string when none exist) and metadata of attached how-to videos. Answer 'how do I do X' from this; when instructions are empty, say no guide is stored rather than presenting invented technique advice as the user's guide. Videos are uploaded only through the HTTP API (POST /api/v1/sets/{id}/videos for a performed set, POST /api/v1/exercises/{ref}/videos for an exercise how-to), typically from the app; there is no MCP upload tool. Tools see video metadata only (id, filename, content type, size, caption, date), never the video content: do not describe or judge what a video shows. Video storage can be disabled on the server; then these tools report that media storage is not configured.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | yes | Exercise name, alias or numeric id. |

## get_nutrition_day

**Get nutrition day** — read-only

Read one local day of nutrition (or several with from/to): the entries in logged order, consumed totals per nutrient, entries_missing_value per nutrient (entries with that value not recorded, so a total may be an undercount), the targets in effect that day with their revision, and remaining/over per nutrient when a target exists. Always returns today and the timezone, so 'today'/'yesterday' resolve correctly. Units: calories_kcal in kilocalories (kcal); protein_g, carbs_g and fat_g in grams. Values describe the WHOLE portion eaten, not per 100 g. Dates are local calendar dates YYYY-MM-DD in the configured timezone (default Asia/Jerusalem); omit date for today. Food cannot be logged for a future date. Nutrition never creates or changes a workout.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `date` | string | no | Local date YYYY-MM-DD. Omit for today. Do not combine with from/to. |
| `from` | string | no | First local date of a multi-day read (returns days[] instead of day). At most 366 days. |
| `to` | string | no | Last local date of a multi-day read; defaults to from. |

## get_nutrition_entry

**Get nutrition entry** — read-only

Read one nutrition entry by id, with its revision. Values are null when not recorded. Units: calories_kcal in kilocalories (kcal); protein_g, carbs_g and fat_g in grams. Values describe the WHOLE portion eaten, not per 100 g.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `entry_id` | integer | yes | Id of the nutrition entry. |

## get_nutrition_targets

**Get nutrition targets** — read-only

Read the daily nutrition targets in effect on a local date (default today) with their revision (0 when none were ever set), plus the full history of target changes. A null target means none is set. Units: calories_kcal in kilocalories (kcal); protein_g, carbs_g and fat_g in grams. Values describe the WHOLE portion eaten, not per 100 g.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `date` | string | no | Local date YYYY-MM-DD. Omit for today. |

## get_plan

**Get plan** — read-only

Read one plan with its revision (pass it as expected_revision to replace_plan/delete_plan/set_plan_schedule), its schedule (null = unscheduled) and ordered exercises with target sets, rep ranges, target load (kg), RPE and RIR.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `plan_id` | integer | yes | Id of the plan. |

## get_schedule

**Get schedule** — read-only

What is planned for a range of local dates (default: the current Monday-Sunday week in the configured timezone). Read-only: never creates a workout. Returns today and timezone; days[] with each date's weekday, the plans scheduled on it by fixed weekdays and their status, and the workout that exists on that date (id, plan, working sets, revision) if any; weekly_targets[] for times_per_week plans per Monday-start week (target, completed = workouts following the plan with completed working sets, remaining, dates); and unscheduled_plans[]. Statuses: done = a workout following this plan has completed working sets on that date; started = a workout following this plan exists but has no completed working sets yet; missed = the date has passed without it; due = today and not done yet; upcoming = a future date. A missed day is information only. To follow a scheduled plan, pass its plan_id to log_sets. A schedule says when a plan is meant to be done: {type:'weekdays', days:[...]} for fixed days (mon..sun in the user's configured timezone) or {type:'times_per_week', times_per_week:N} for a frequency; the week starts on Monday. A schedule NEVER creates or changes a workout.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `from` | string | no | Inclusive first local date YYYY-MM-DD. Omit both from and to for the current Monday-Sunday week; with only from, the 7 days starting there. |
| `to` | string | no | Inclusive last local date YYYY-MM-DD; requires from. At most 62 days. |

## get_session

**Get session by id** — read-only

Read a workout by its session id with its ordered sets (stable ids, values; null = not recorded), revision and total SI. Use get_workout to read by date with set numbers and progress. A 'workout' and a 'session' are the same thing: there is exactly one per local calendar date.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | integer | yes | Id of the existing workout (session). |

## get_set

**Get set** — read-only

Read one completed set by its stable id, including its current revision. Values are null when not recorded.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `set_id` | integer | yes | Stable id of the set. |

## get_weigh_in

**Get weigh-in** — read-only

Read the weigh-in of one local date (today when omitted). found=false (and no weigh_in) when nothing was logged that day - do not make a value up. Always returns today and the timezone. For several days or how the weight is going, use get_weight_trend. Body weight is in kilograms (20-400 kg). If the user gives pounds, convert to kg yourself (1 lb = 0.45359237 kg) and tell them you converted.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `date` | string | no | Local date YYYY-MM-DD. Omit for today. |

## get_weight_trend

**Get weight trend** — read-only

How the user's body weight is going: the last `weeks` weeks (default 8) ending with the week that contains `to` (default today), oldest first, each with days_logged, average_kg, min_kg, max_kg, change_kg, change_pct and trend; plus the weigh-ins in the range, latest (the most recent weigh-in on or before `to`) and overall_change_kg (last week with data minus the first one). Weeks run Monday to Sunday. A week's average_kg (and min_kg/max_kg) uses only the days that have a weigh-in (days_logged says how many); change_kg/change_pct compare it with the previous week's average. A null average or change means there is not enough data (no weigh-in that week or the week before) - report it as missing and never invent, estimate or interpolate a number for it. trend is up, down or flat (|change_kg| < 0.1 kg). Daily weight fluctuates; the weekly average is the signal. Read-only. Body weight is in kilograms (20-400 kg). If the user gives pounds, convert to kg yourself (1 lb = 0.45359237 kg) and tell them you converted.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `to` | string | no | Local date YYYY-MM-DD the view ends on (its week is the last one). Omit for today. |
| `weeks` | integer | no | How many weeks to return, 1-104. Defaults to 8. |

## get_workout

**Get workout** — read-only

Read the workout for a local date without creating anything. Returns exists=false when there is no workout for that date. Always returns the configured timezone and today's local date, so 'today' and 'yesterday' can be resolved correctly. The workout lists every completed set with its stable id (use it for update_set/delete_set), set_number (1-based order within its exercise; for reading only), revision and values (null = not recorded), plus progress: planned (target_sets, rep range, target load/RPE/RIR) versus completed working sets (done_sets) per exercise. Planned sets are never counted as done. A 'workout' and a 'session' are the same thing: there is exactly one per local calendar date. Dates are local calendar dates in exact YYYY-MM-DD format, in the server's configured timezone (get_workout reports the timezone and today's date); omit date to mean today.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `date` | string | no | Local date YYYY-MM-DD in the configured timezone. Omit for today. |

## import_plan

**Import plan** — destructive write

Validate a plan document and store it atomically. Plans are matched by name (case-insensitive): a new name creates a plan; an existing name is rejected unless on_conflict='replace', which replaces that plan's notes and exercises in place (keeping its id). Workouts already created from the plan are never changed. The document may carry a schedule; replacing a plan with a document that lacks schedule clears the plan's schedule. Every problem is reported with its JSON path (e.g. items[2].sets) and nothing is written. Use dry_run=true to preview. new_exercises lists exercise names that would be/were created, with similar existing names. A plan prescribes what is planned (sets, rep range, optional target load in kg, target RPE 1-10, target RIR 0-10); omitted targets mean 'not prescribed'. Plans never create completed sets.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `dry_run` | boolean | no | Validate and report what would happen without writing anything. |
| `on_conflict` | string | no | What to do when a plan with the same name (case-insensitive) exists: 'error' (default) rejects the import; 'replace' replaces that plan's notes and exercises in place, keeping its id. |
| `plan` | object | yes | Plan document in the portable "workout-plan" version 1 format (the same object export_plan returns): {"format":"workout-plan","version":1,"name":"Upper A","notes":"...","items":[{"exercise":"bench press","sets":3,"rep_min":6,"rep_max":8,"target_weight_kg":60,"target_rpe":8}]}. Each exercise may appear once (use sets for volume); 1-40 items. Omit a field to mean 'not prescribed'. |

## list_exercise_groups

**List exercise groups** — read-only

List every exercise that has a muscle group assigned, ordered by group.

_No parameters._

## list_exercise_notes

**List exercise notes** — read-only

List every exercise that has a setup note.

_No parameters._

## list_exercises

**List exercises** — read-only

List every known exercise with its stable id, canonical name, aliases, muscle group and number of logged sets. Use it to match what the user says to an existing exercise before logging, so history is not split across near-duplicate names.

_No parameters._

## list_nutrition_entries

**List nutrition entries** — read-only

Find nutrition entries, newest first. With no filters returns the latest entries across all dates - use it (optionally with query) to find a previous meal before copy_nutrition_entry. Filter by one date, or by an inclusive from/to range, and/or by query text in the name. Units: calories_kcal in kilocalories (kcal); protein_g, carbs_g and fat_g in grams. Values describe the WHOLE portion eaten, not per 100 g. Dates are local calendar dates YYYY-MM-DD in the configured timezone (default Asia/Jerusalem); omit date for today. Food cannot be logged for a future date. Nutrition never creates or changes a workout.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `date` | string | no | Only entries of this local date YYYY-MM-DD. Do not combine with from/to. |
| `from` | string | no | Inclusive earliest local date YYYY-MM-DD. |
| `limit` | integer | no | Maximum entries; defaults to 100, at most 500. |
| `query` | string | no | Case-insensitive text to match in the entry name, e.g. 'oat'. |
| `to` | string | no | Inclusive latest local date YYYY-MM-DD. |

## list_plans

**List plans** — read-only

List every saved plan with its revision, schedule (null = unscheduled) and total planned set count.

_No parameters._

## list_sessions

**List workouts** — read-only

List workouts newest first, optionally within inclusive local date bounds (from must not be after to). A 'workout' and a 'session' are the same thing: there is exactly one per local calendar date.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `from` | string | no | Inclusive earliest local date YYYY-MM-DD. |
| `limit` | integer | no | Maximum workouts to return; defaults to 20, accepts 1-100. |
| `to` | string | no | Inclusive latest local date YYYY-MM-DD. |

## list_set_videos

**List set videos** — read-only

List metadata of the videos attached to one performed set (by its stable set id). Videos are uploaded only through the HTTP API (POST /api/v1/sets/{id}/videos for a performed set, POST /api/v1/exercises/{ref}/videos for an exercise how-to), typically from the app; there is no MCP upload tool. Tools see video metadata only (id, filename, content type, size, caption, date), never the video content: do not describe or judge what a video shows. Video storage can be disabled on the server; then these tools report that media storage is not configured.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `set_id` | integer | yes | Stable id of the set. |

## list_volume_landmarks

**List volume landmarks** — read-only

List the muscle groups that have personal volume landmarks recorded.

_No parameters._

## log_set

**Log identical sets** — write

Convenience for one exercise: record count (1-20) identical completed sets into the workout for the date (today when omitted), creating the workout if needed. Equivalent to log_sets with count copies of one set; returns the same result. Use log_sets for several exercises or for prescriptions. Units: weight_kg in kilograms; rpe is rate of perceived exertion 1-10; rir is reps in reserve 0-10. An omitted value means 'not recorded' and is stored as unknown: never invent, estimate or default a weight, reps, RPE or RIR the user did not state. weight_kg 0 means an explicit zero load, not unknown. idempotency_key (optional, at most 128 characters) is global to this single-user server, not scoped per date or tool. Retrying with the same key and the same payload replays the stored result (replayed=true) without writing anything again; the same key with a different payload is rejected as a conflict. Reuse a key only to retry the same intended write; use a new key for every new write. Dates are local calendar dates in exact YYYY-MM-DD format, in the server's configured timezone (get_workout reports the timezone and today's date); omit date to mean today.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `count` | integer | no | How many identical sets to record, 1-20; defaults to 1. Use it for '3x10 at 80kg'. |
| `date` | string | no | Local date YYYY-MM-DD of the workout. Omit for today. |
| `exercise` | string | no | Exercise name or alias. Give exercise or exercise_id. |
| `exercise_id` | integer | no | Exact id of an existing exercise; wins over exercise. |
| `idempotency_key` | string | no | Optional retry key, global to this user. Same key + same payload replays; same key + different payload is a conflict. |
| `notes` | string | no | Free-text note copied onto each set. |
| `reps` | integer | no | Completed repetitions per set, 1-1000. Omit if not stated. |
| `rir` | number | no | Reps in reserve 0-10. Omit if not stated. |
| `rpe` | number | no | RPE 1-10. Omit if not stated. |
| `set_type` | string | no | warmup, working (default), backoff, drop or failure. |
| `technique` | string | no | Optional intensity technique, e.g. 'drop set'. |
| `weight_kg` | number | no | Load in kilograms, 0-2000. Omit if not stated. |

## log_sets

**Log sets and prescriptions** — write

Primary write for incremental logging. In one atomic transaction: finds or creates the workout for the date, appends the given completed sets in order, and merges the given prescriptions (planned work). Nothing is written if any part is invalid. Prescriptions describe what is planned and never create completed sets: 'planning 3 sets of squats' is a prescription {exercise, target_sets: 3}, '130 kg x 4' is a set. Returns the persisted sets with their ids and stored values, the stored prescriptions, the updated workout (revision, progress), created=true if the workout was new, new_exercises for names that did not exist yet (each with similar existing names, so a typo can be caught and fixed with update_set or rename_exercise), and replayed=true when served from an earlier identical request. Units: weight_kg in kilograms; rpe is rate of perceived exertion 1-10; rir is reps in reserve 0-10. An omitted value means 'not recorded' and is stored as unknown: never invent, estimate or default a weight, reps, RPE or RIR the user did not state. weight_kg 0 means an explicit zero load, not unknown. idempotency_key (optional, at most 128 characters) is global to this single-user server, not scoped per date or tool. Retrying with the same key and the same payload replays the stored result (replayed=true) without writing anything again; the same key with a different payload is rejected as a conflict. Reuse a key only to retry the same intended write; use a new key for every new write. expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry. A 'workout' and a 'session' are the same thing: there is exactly one per local calendar date. Dates are local calendar dates in exact YYYY-MM-DD format, in the server's configured timezone (get_workout reports the timezone and today's date); omit date to mean today.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `date` | string | no | Local date YYYY-MM-DD of the workout. Omit for today. Sets cannot be logged for a future date. |
| `expected_revision` | integer | no | Optional workout revision from your last read; the write is rejected with a conflict if the workout changed since. Ignored when the workout does not exist yet. |
| `idempotency_key` | string | no | Optional retry key, global to this user. Same key + same payload replays; same key + different payload is a conflict. New write = new key. |
| `plan_id` | integer | no | Optional plan to copy into the workout when it follows no plan yet. A workout already following a different plan is a conflict. |
| `prescriptions` | array of object | no | Planned work to add or merge into this workout's prescription (at most 40). Merging keeps stored fields you omit. Never creates completed sets. |
| `sets` | array of object | no | Completed sets to append, in the order performed (at most 100). Only include sets the user actually did. |

## log_weigh_in

**Log weigh-in** — destructive write

Record the user's body weight for a date (today when omitted). There is at most one weigh-in per local date: logging a date that already has one REPLACES it, and sending the same values again changes nothing, so a retry after a timeout is harmless and needs no key. Returns the stored weigh_in (date, weight_kg, note, revision), created (false when it replaced or matched an existing weigh-in), today and timezone. Body weight is in kilograms (20-400 kg). If the user gives pounds, convert to kg yourself (1 lb = 0.45359237 kg) and tell them you converted. Dates are local calendar dates YYYY-MM-DD in the configured timezone (default Asia/Jerusalem); omit date for today there. A weigh-in cannot be logged for a future date. Body weight never creates or changes a workout.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `date` | string | no | Local date YYYY-MM-DD of the weigh-in. Omit for today. |
| `expected_revision` | integer | no | Optional. The weigh-in's revision from your last read; 0 means the date must not have a weigh-in yet. Rejected with a conflict if it does not match. |
| `note` | string | no | Optional short note the user gave (at most 200 characters), e.g. 'after travel'. |
| `weight_kg` | number | yes | Body weight in kilograms, 20-400. Convert pounds to kg first. |

## move_plan_exercise

**Move plan exercise** — destructive write

Move an exercise one place up or down within a plan. The order is the order the workout is performed in.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `direction` | string | yes | 'up' or 'down'. Order is the order the workout is performed in. |
| `exercise` | string | yes | Exercise currently in the plan. |
| `plan_id` | integer | yes | Id of the plan to edit. |

## record_feedback

**Record feedback** — destructive write

Rate how one muscle group responded to a workout: fatigue, pump and recovery, each 0-3, as stated by the user. Only rate groups actually trained; never invent ratings. Re-rating replaces the previous rating. Drives volume_recommendation.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `fatigue` | integer | yes | Fatigue the muscle took, 0-3. 0 = barely noticed, 3 = severe. |
| `muscle_group` | string | yes | Muscle group trained; one of the supported values. |
| `pump` | integer | yes | Pump the muscle got, 0-3. 0 = none, 3 = extreme. |
| `recovery` | integer | yes | How sore/unrecovered it felt afterwards, 0-3. 0 = nothing, 3 = still wrecked days later. |
| `session_id` | integer | yes | Id of the workout (session) being rated. |

## remove_exercise_alias

**Remove exercise alias** — destructive write

Delete an alias. Sets logged under it keep pointing at the exercise.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `alias` | string | yes | Alias to remove. |

## remove_plan_exercise

**Remove plan exercise** — destructive write

Remove an exercise from a saved plan and close the gap in its order.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | yes | Exercise currently in the plan (name or alias). |
| `plan_id` | integer | yes | Id of the plan to edit. |

## remove_session_exercise

**Remove workout exercise** — destructive write

Drop an exercise from a workout's prescription. Completed sets already logged for it are kept and reappear as off-plan work. expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | yes | Exercise to drop from this workout's prescription. |
| `expected_revision` | integer | no | The workout's revision from your last read (get_workout); rejected with a conflict if the workout changed since. |
| `session_id` | integer | yes | Id of the workout (session) to adjust. |

## rename_exercise

**Rename exercise** — destructive write

Change an exercise's canonical name everywhere it is referenced (sets, plans). The old name does not become an alias automatically (add one with add_exercise_alias if wanted). A name that already belongs to another exercise is a conflict: renaming never merges exercises.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | no | Current canonical name or alias of the exercise. |
| `exercise_id` | integer | no | Exact id of the exercise. Give exercise_id or exercise. |
| `name` | string | yes | New canonical name. |

## replace_plan

**Replace plan** — destructive write

Replace an existing plan's name, notes and exercises from a plan document, atomically, keeping its id. Workouts already created from it are not changed. Typical flow: export_plan or get_plan, edit, replace_plan with expected_revision. expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `expected_revision` | integer | no | The plan's revision from your last read (get_plan); rejected with a conflict if it changed since. |
| `plan` | object | yes | Plan document in the portable "workout-plan" version 1 format (the same object export_plan returns): {"format":"workout-plan","version":1,"name":"Upper A","notes":"...","items":[{"exercise":"bench press","sets":3,"rep_min":6,"rep_max":8,"target_weight_kg":60,"target_rpe":8}]}. Each exercise may appear once (use sets for volume); 1-40 items. Omit a field to mean 'not prescribed'. |
| `plan_id` | integer | yes | Id of the plan to replace. |

## save_session_as_plan

**Save workout as plan** — write

Turn a workout's prescription, including in-workout adjustments and off-plan work, into a new reusable plan. Skipped exercises are left out.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Name for the new plan. |
| `session_id` | integer | yes | Id of the workout (session) to turn into a plan. |

## session_progress

**Workout progress** — read-only

For a workout by session id: planned versus completed working sets per exercise, with the prescribed rep range, target load (kg), RPE and RIR. Exercises done off-plan are listed with target_sets 0. get_workout returns the same progress by date.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | integer | yes | Id of the existing workout (session). |

## set_exercise_group

**Set exercise muscle group** — destructive write

Assign an exercise to one muscle group so its sets count toward that group's volume. Re-assigning replaces the previous group.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | yes | Exercise name or alias. |
| `muscle_group` | string | yes | Muscle group the exercise trains; one of the supported values. |

## set_exercise_instructions

**Set exercise instructions** — destructive write

Store (replace) the written how-to for an existing exercise; an empty string clears it. Write only text the user dictated or explicitly approved - never invent coaching content the user did not provide. Returns the updated guide. Requires media storage to be configured.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | yes | Existing exercise name, alias or numeric id (never created here). |
| `instructions` | string | yes | Written how-to text exactly as the user provided or approved it, at most 5000 characters. Empty string clears it. |

## set_exercise_note

**Set exercise note** — destructive write

Store a persistent setup reminder for an exercise, such as seat height or grip width. An empty note removes it.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | yes | Exercise name or alias. |
| `note` | string | yes | Setup reminder, e.g. 'seat at 4, wide grip'. Empty string removes it. |

## set_nutrition_targets

**Set nutrition targets** — destructive write

Set daily nutrition targets from effective_from (default today) until the next change. Earlier dates never change. Omitted values keep what is in effect on effective_from; clear removes a target. Targets are only ever what the user explicitly asks for: never invent, suggest-and-save or recommend targets. Units: calories_kcal in kcal per day; protein_g, carbs_g, fat_g in grams per day. Dates are local YYYY-MM-DD in the configured timezone (default Asia/Jerusalem). Never creates a workout. expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `calories_kcal` | number | no | Daily energy target in kcal. Omit to keep the value in effect on effective_from. |
| `carbs_g` | number | no | Daily carbohydrate target in grams. Omit to keep. |
| `clear` | array of string | no | Targets to remove from effective_from on: any of calories_kcal, protein_g, carbs_g, fat_g. |
| `effective_from` | string | no | First local date YYYY-MM-DD the targets apply to; omit for today. They apply until the next change; earlier dates never change. |
| `expected_revision` | integer | no | targets.revision from get_nutrition_targets or get_nutrition_day (0 when no targets were ever set); rejected with a conflict if targets changed since. |
| `fat_g` | number | no | Daily fat target in grams. Omit to keep. |
| `protein_g` | number | no | Daily protein target in grams. Omit to keep. |

## set_plan_exercise

**Set plan exercise** — destructive write

Add an exercise to a saved plan, or replace its prescription. Editing a plan never changes workouts already started from it. A plan prescribes what is planned (sets, rep range, optional target load in kg, target RPE 1-10, target RIR 0-10); omitted targets mean 'not prescribed'. Plans never create completed sets.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | yes | Exercise name or alias; normalized to lower case. |
| `notes` | string | no | Free-text intent: cues, tempo, whether to push the last set. |
| `plan_id` | integer | yes | Id of the plan to edit. |
| `rep_max` | integer | no | Upper bound of the target rep range, e.g. 12 for '10 to 12'. |
| `rep_min` | integer | no | Lower bound of the target rep range, e.g. 10 for '10 to 12'. |
| `superset` | string | no | Label grouping exercises done back to back, e.g. 'A'. Omit for a standalone exercise. |
| `target_rir` | number | no | Prescribed reps in reserve 0-10. Omit if not prescribed. |
| `target_rpe` | number | no | Prescribed RPE 1-10. Omit if not prescribed. |
| `target_sets` | integer | yes | Planned number of sets, 1-20. |
| `target_weight_kg` | number | no | Prescribed load in kilograms, 0-2000. Omit if not prescribed. |

## set_plan_schedule

**Set plan schedule** — destructive write

Set or replace a plan's schedule; pass schedule null (or omit it) to clear it. Returns the updated plan with its new revision. A schedule says when a plan is meant to be done: {type:'weekdays', days:[...]} for fixed days (mon..sun in the user's configured timezone) or {type:'times_per_week', times_per_week:N} for a frequency; the week starts on Monday. A schedule NEVER creates or changes a workout. Use explicit days or a count exactly as the user stated them ('Mondays and Thursdays' -> days ['mon','thu']; 'three times a week' -> times_per_week 3). Replacing a plan via import_plan(on_conflict='replace') with a document lacking schedule clears it. expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `expected_revision` | integer | no | The plan's revision from your last read; rejected with a conflict if it changed since. |
| `plan_id` | integer | yes | Id of the plan. |
| `schedule` | object | no | The new schedule. null or omitted clears it (the plan becomes unscheduled). |

## set_volume_landmarks

**Set volume landmarks** — destructive write

Record a muscle group's personal weekly-set boundaries: MEV (below which it stops growing) and MRV (above which fatigue outpaces recovery). Leave a value at 0 if unknown. Replaces previous values.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `mev` | integer | no | Minimum effective volume in weekly sets. 0 means unknown. |
| `mrv` | integer | no | Maximum recoverable volume in weekly sets. 0 means unknown. |
| `muscle_group` | string | yes | Muscle group; one of the supported values. |

## start_session

**Start or get workout** — write (idempotent)

Get-or-create the workout for a local date (today when omitted), optionally copying a plan into it. Calling it for a date that already has a workout returns that workout; nothing is duplicated. A workout already following a different plan is a conflict. You rarely need this: log_sets creates the workout on first write, and get_workout reads it without creating. A 'workout' and a 'session' are the same thing: there is exactly one per local calendar date. Dates are local calendar dates in exact YYYY-MM-DD format, in the server's configured timezone (get_workout reports the timezone and today's date); omit date to mean today.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `date` | string | no | Local date YYYY-MM-DD. Omit for today. |
| `plan_id` | integer | no | Optional plan to copy into the workout. Omit for a free-form workout. |

## suggest_load

**Suggest load** — read-only

Advisory only: suggest the next working weight (kg) for an exercise, judged against its prescribed rep range and RPE, explaining the reasoning. Never changes anything. Declines when there is no comparable set or no prescription.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | yes | Exercise name or alias. |

## swap_session_exercise

**Swap workout exercise** — destructive write

Substitute one exercise for another in a workout's prescription, keeping the same prescription. Use when the planned machine is taken.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `exercise` | string | yes | Exercise currently prescribed in the workout. |
| `replacement` | string | yes | Exercise to use instead, keeping the same prescription. |
| `session_id` | integer | yes | Id of the workout (session) to adjust. |

## training_summary

**Training summary** — read-only

Overview of completed working sets over a local date range (default: the 28 days ending today in the configured timezone): workouts, working and warm-up sets, volume in kg, sets missing weight/reps, and breakdowns by exercise, muscle group and week (Monday start).

| Parameter | Type | Required | Description |
|---|---|---|---|
| `from` | string | no | Inclusive earliest local date YYYY-MM-DD. |
| `to` | string | no | Inclusive latest local date YYYY-MM-DD. |

## update_nutrition_entry

**Update nutrition entry** — destructive write

Correct one nutrition entry by id (read it first). Only the fields given change; omitted fields stay as stored. Use clear to turn a value back into 'not recorded'. Returns the persisted entry with its new revision. Units: calories_kcal in kilocalories (kcal); protein_g, carbs_g and fat_g in grams. Values describe the WHOLE portion eaten, not per 100 g. Omit a value that is unknown: omitted means 'not recorded' and is never treated as 0; send 0 only for an explicit zero. Calories are stored exactly as given and never recomputed from macros. expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `calories_kcal` | number | no | Energy of the whole portion in kcal, 0-20000. Omit if unknown. |
| `carbs_g` | number | no | Carbohydrate of the whole portion in grams, 0-2000. Omit if unknown. |
| `clear` | array of string | no | Values to reset to 'not recorded': any of calories_kcal, protein_g, carbs_g, fat_g. |
| `date` | string | no | Move the entry to this local date YYYY-MM-DD (not in the future). |
| `entry_id` | integer | yes | Id of the entry to change (from get_nutrition_day or list_nutrition_entries). |
| `expected_revision` | integer | no | The entry's revision from your last read; rejected with a conflict if it changed since. |
| `fat_g` | number | no | Fat of the whole portion in grams, 0-2000. Omit if unknown. |
| `name` | string | no | New name. |
| `protein_g` | number | no | Protein of the whole portion in grams, 0-2000. Omit if unknown. |

## update_plan

**Update plan** — destructive write

Edit a plan's name or notes without touching its exercises.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | no | New name. Omit to leave unchanged. |
| `notes` | string | no | New notes for the whole routine. Empty string clears them. Omit to leave unchanged. |
| `plan_id` | integer | yes | Id of the plan to edit. |

## update_set

**Update set** — destructive write

Correct one completed set addressed by its stable set_id (read it from get_workout or a log result; never guess it from set_number). Only the fields given change; omitted fields stay as stored. Use clear to turn a recorded weight_kg/reps/rpe/rir back into 'not recorded'. Returns the persisted set with its new revision. Units: weight_kg in kilograms; rpe is rate of perceived exertion 1-10; rir is reps in reserve 0-10. expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `clear` | array of string | no | Metrics to reset to 'not recorded': any of weight_kg, reps, rpe, rir. Cannot be combined with a new value for the same field. |
| `exercise` | string | no | Move the set to this exercise name or alias. |
| `exercise_id` | integer | no | Move the set to this existing exercise id. |
| `expected_revision` | integer | no | The set's revision from your last read; rejected with a conflict if it changed since. |
| `notes` | string | no | New note; empty string clears it. |
| `reps` | integer | no | New repetition count, 1-1000. |
| `rir` | number | no | New reps in reserve 0-10. |
| `rpe` | number | no | New RPE 1-10. |
| `set_id` | integer | yes | Stable id of the set to change (from get_workout or a log result). Never use set_number or position as an id. |
| `set_type` | string | no | New set type: warmup, working, backoff, drop or failure. |
| `technique` | string | no | New technique; empty string clears it back to a straight set. |
| `weight_kg` | number | no | New load in kilograms, 0-2000. |

## volume_by_muscle

**Volume by muscle** — read-only

Legacy total SI and set count per muscle group within an optional local date range. Exercises with no group are reported under an empty muscle_group.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `from` | string | no | Inclusive earliest local date YYYY-MM-DD. |
| `to` | string | no | Inclusive latest local date YYYY-MM-DD. |

## volume_recommendation

**Volume recommendation** — read-only

Advisory set-count change per muscle group for next week, from recent feedback weighted toward the latest. Reports the sample count and confidence, declines to advise on a single rating, and never pushes volume past a recorded MEV or MRV. Changes nothing.

_No parameters._

## weekly_volume

**Weekly volume** — read-only

Legacy SI and set count per muscle group per training week (Monday start), newest week first. SI is derived from RPE and is 0 for sets without RPE. Prefer training_summary for kg volume.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `from` | string | no | Inclusive earliest local date YYYY-MM-DD. |
| `to` | string | no | Inclusive latest local date YYYY-MM-DD. |

