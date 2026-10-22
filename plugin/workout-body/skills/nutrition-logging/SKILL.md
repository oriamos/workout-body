---
name: nutrition-logging
description: Log and review food, meals, calories and macros (protein, carbs, fat) through the workout-body MCP server (add_nutrition_entry, get_nutrition_day, copy_nutrition_entry, update_nutrition_entry, nutrition targets). Use whenever the user reports something they ate or drank, asks how many calories/protein they have left or had on a day, wants to repeat a previous meal ("same breakfast as yesterday"), corrects a food entry, or sets daily nutrition targets.
---

# Nutrition logging

The workout-body MCP server stores the user's food log. Turn what the user
says into explicit tool calls, and report only what was actually saved.

## Core model

- One **entry** = one food or meal eaten on a local date: a name plus
  calories_kcal (kcal) and protein_g, carbs_g, fat_g (grams) for the **whole
  portion eaten**, never per 100 g.
- An omitted value means "not recorded". 0 means an explicit zero. Never send
  0 for "unknown". Calories are stored as given and never recomputed from the
  macros.
- Nutrition never creates or changes a workout.
- Targets are daily goals that apply from an `effective_from` date until the
  next change; earlier days keep the targets they had.

## Rules

1. **Explicit values.** Log with `add_nutrition_entry`, putting the user's
   numbers in the matching fields.
2. **Estimates must be labelled.** If the user gave no numbers, you may
   estimate them, but then your reply must say clearly which values are the
   user's and which are your estimates, that an estimate is approximate (there
   is no verified food database behind it - never claim otherwise), and that
   the user can correct them. If you do not want to estimate, log the name and
   omit the values.
3. **Ask one brief question only when a required piece is missing**: what was
   eaten, or which entry the user means. Do not ask for numbers you can omit or
   estimate.
4. **Repeat meals by copying.** For "same as yesterday" / "that breakfast
   again", first find the original: `get_nutrition_day` for that date, or
   `list_nutrition_entries` (no filters = latest entries; `query` matches the
   name). Then `copy_nutrition_entry` with its `entry_id`. The copy is
   independent; editing it never changes the original. If several entries
   match, ask which one.
5. **Read before correcting.** Get the entry (`get_nutrition_day`,
   `get_nutrition_entry` or `list_nutrition_entries`), then call
   `update_nutrition_entry` with its `entry_id`, only the changed fields, and
   its `revision` as `expected_revision`. Use `clear` to reset a value to "not
   recorded". On a conflict, re-read and retry.
6. **Dates.** Local `YYYY-MM-DD` in the configured timezone (default
   Asia/Jerusalem). `get_nutrition_day` returns `today` and `timezone`; use
   them to resolve "today", "yesterday", "last night". Food cannot be logged
   for a future date.
7. **Idempotency keys.** Use a fresh key for each new entry or copy; when a
   call fails with a timeout or connection error, retry with the same key and
   the same payload (it replays instead of duplicating). A meal eaten twice on
   purpose is two writes: no key or a new key each.
8. **Report only what was saved.** Confirm only after a successful tool
   response, quoting the saved name, values and entry id. On an error, fix the
   field it names or ask.
9. **Targets.** Never set, suggest-and-save, or recommend targets the user did
   not ask for. When they ask, call `set_nutrition_targets` with their numbers
   (and `effective_from` if they named a start date), passing
   `targets.revision` from `get_nutrition_targets`/`get_nutrition_day` as
   `expected_revision` (0 if none were set).
10. **Confirm deletes.** Name the entry and get a yes before
    `delete_nutrition_entry`, unless the user explicitly asked for that exact
    deletion.
11. **Totals can be partial.** `entries_missing_value` counts entries without a
    value for that nutrient; mention it when it makes a total an undercount.

## Worked examples

**Given numbers.** "Lunch: chicken and rice, 650 kcal, 48 g protein, 70 g
carbs, 14 g fat."

```json
add_nutrition_entry {"name": "chicken and rice", "calories_kcal": 650, "protein_g": 48, "carbs_g": 70, "fat_g": 14, "idempotency_key": "2b7e...-1"}
```

Reply: "Saved chicken and rice (entry 17): 650 kcal, 48 g protein, 70 g
carbs, 14 g fat."

**Estimates, clearly labelled.** "Had two eggs and a slice of toast."

```json
add_nutrition_entry {"name": "2 eggs and 1 slice of toast", "calories_kcal": 230, "protein_g": 15, "carbs_g": 13, "fat_g": 11, "idempotency_key": "2b7e...-2"}
```

Reply: "Saved 2 eggs and 1 slice of toast (entry 18). You didn't give numbers,
so all values are my rough estimates, not from a food database: about 230
kcal, 15 g protein, 13 g carbs, 11 g fat. Tell me if you want them changed."

**Same lunch as yesterday.**

1. `get_nutrition_day {}` -> `today: "2026-03-10"`, so yesterday is
   2026-03-09.
2. `get_nutrition_day {"date": "2026-03-09"}` (or `list_nutrition_entries
   {"date": "2026-03-09"}`) -> lunch is entry 12, "chicken and rice".
3. ```json
   copy_nutrition_entry {"entry_id": 12, "idempotency_key": "2b7e...-3"}
   ```

Reply: "Copied yesterday's chicken and rice to today (entry 19): 650 kcal, 48
g protein, 70 g carbs, 14 g fat."

**Correction.** "Actually it was 30 g protein."

1. Identify "it" (the entry just discussed, e.g. 19); if unclear, read
   today's entries and ask which one.
2. `get_nutrition_entry {"entry_id": 19}` -> `revision: 1`.
3. ```json
   update_nutrition_entry {"entry_id": 19, "protein_g": 30, "expected_revision": 1}
   ```

Reply: "Updated entry 19: protein is now 30 g; calories, carbs and fat
unchanged (650 kcal, 70 g, 14 g)."

**Remaining today.** "How much protein do I have left?" ->
`get_nutrition_day {}` and answer from `protein_g.consumed`, `target`,
`remaining` (say that no target is set if `target` is null; do not invent
one).
