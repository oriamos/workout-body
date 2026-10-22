---
name: nutrition-logging
description: Log and review food, meals, calories and macros (protein, carbs, fat) through the workout-body MCP server (add_nutrition_entry with per-ingredient items, suggest_nutrition_items, get_nutrition_day, copy_nutrition_entry, update_nutrition_entry, nutrition targets). Use whenever the user reports something they ate or drank (including a meal with several ingredients), asks how many calories/protein they have left or had on a day, wants to repeat a previous meal ("same breakfast as yesterday"), corrects a food entry, or sets daily nutrition targets.
---

# Nutrition logging

The workout-body MCP server stores the user's food log. Turn what the user
says into explicit tool calls, and report only what was actually saved.

## Core model

- One **entry** = one food or meal eaten on a local date: a name plus
  calories_kcal (kcal) and protein_g, carbs_g, fat_g (grams) for the **whole
  portion eaten**, never per 100 g.
- A meal can be made of **items** (ingredients): each item has its own name,
  optional `amount_g` (grams used) and values **for that amount**. The meal's
  values are then computed as the sums of the items; do not also send them on
  the meal. `incomplete_values` names any value only some items have (that
  sum is partial); a value no item has stays "not recorded".
- An omitted value means "not recorded". 0 means an explicit zero. Never send
  0 for "unknown". Calories are stored as given and never recomputed from the
  macros.
- Nutrition never creates or changes a workout.
- Targets are daily goals that apply from an `effective_from` date until the
  next change; earlier days keep the targets they had.

## Rules

1. **Explicit values.** Log with `add_nutrition_entry`, putting the user's
   numbers in the matching fields.
2. **Meals as ingredients.** When the user describes a meal with several
   components ("yogurt with granola and blueberries", "chicken, rice and
   salad"), log ONE entry named for the meal with one `items` element per
   ingredient, each with its `amount_g` when known and its own values for
   that amount. Omit an item value that is unknown (never 0). A single food
   ("a banana") needs no items.
3. **Reuse ingredients logged before.** Before estimating an ingredient,
   call `suggest_nutrition_items` (with `query` = the ingredient, e.g.
   "yogurt"). If it was logged before, reuse its latest values; when the
   portion differs, scale every value by new grams / logged `amount_g` and say
   in your reply that you scaled them (e.g. "from your 200 g entry, scaled to
   150 g"). Values that are null there stay omitted.
4. **Estimates must be labelled.** If the user gave no numbers, you may
   estimate them, but then your reply must say clearly which values are the
   user's and which are your estimates, that an estimate is approximate (there
   is no verified food database behind it - never claim otherwise), and that
   the user can correct them. If you do not want to estimate, log the name and
   omit the values.
5. **Ask one brief question only when a required piece is missing**: what was
   eaten, or which entry the user means. Do not ask for numbers you can omit or
   estimate.
6. **Repeat meals by copying.** For "same as yesterday" / "that breakfast
   again", first find the original: `get_nutrition_day` for that date, or
   `list_nutrition_entries` (no filters = latest entries; `query` matches the
   name). Then `copy_nutrition_entry` with its `entry_id`. The copy is
   independent; editing it never changes the original. If several entries
   match, ask which one.
7. **Read before correcting.** Get the entry (`get_nutrition_day`,
   `get_nutrition_entry` or `list_nutrition_entries`), then call
   `update_nutrition_entry` with its `entry_id`, only the changed fields, and
   its `revision` as `expected_revision`. Use `clear` to reset a value to "not
   recorded". On a conflict, re-read and retry. For a meal with `items`,
   change ingredients by sending `items` with the **whole** new list (every
   item to keep, with the corrected one); the meal's values are recomputed.
   Its values cannot be set directly; `items: []` turns it back into a plain
   entry that keeps the sums as its own values.
8. **Dates.** Local `YYYY-MM-DD` in the configured timezone (default
   Asia/Jerusalem). `get_nutrition_day` returns `today` and `timezone`; use
   them to resolve "today", "yesterday", "last night". Food cannot be logged
   for a future date.
9. **Idempotency keys.** Use a fresh key for each new entry or copy; when a
   call fails with a timeout or connection error, retry with the same key and
   the same payload (it replays instead of duplicating). A meal eaten twice on
   purpose is two writes: no key or a new key each.
10. **Report only what was saved.** Confirm only after a successful tool
    response, quoting the saved name, values and entry id. On an error, fix
    the field it names (e.g. `items[1].amount_g`) or ask.
11. **Targets.** Never set, suggest-and-save, or recommend targets the user
    did not ask for. When they ask, call `set_nutrition_targets` with their
    numbers (and `effective_from` if they named a start date), passing
    `targets.revision` from `get_nutrition_targets`/`get_nutrition_day` as
    `expected_revision` (0 if none were set).
12. **Confirm deletes.** Name the entry and get a yes before
    `delete_nutrition_entry`, unless the user explicitly asked for that exact
    deletion.
13. **Totals can be partial.** `entries_missing_value` counts entries without a
    value for that nutrient, including meals whose `incomplete_values` lists
    it (only their known part is counted); mention it when it makes a total
    an undercount.

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

**A meal of several ingredients.** "Breakfast: 200 g Greek yogurt with
granola and blueberries."

1. `suggest_nutrition_items {"query": "yogurt"}` -> Greek yogurt, `amount_g`
   150, 145.5 kcal, 15 g protein, 5.7 g carbs, 7.5 g fat (logged before).
   `suggest_nutrition_items {"query": "granola"}` and `{"query": "blueberr"}`
   -> nothing.
2. Scale the yogurt by 200 / 150 and estimate the rest:

```json
add_nutrition_entry {"name": "Greek yogurt with granola and blueberries", "items": [
  {"name": "Greek yogurt", "amount_g": 200, "calories_kcal": 194, "protein_g": 20, "carbs_g": 7.6, "fat_g": 10},
  {"name": "Granola", "amount_g": 40, "calories_kcal": 180, "protein_g": 4, "carbs_g": 26, "fat_g": 6.5},
  {"name": "Blueberries", "amount_g": 80, "calories_kcal": 46, "protein_g": 0.6, "carbs_g": 11.6}
], "idempotency_key": "2b7e...-4"}
```

-> entry 20: 420 kcal, 24.6 g protein, 45.2 g carbs, 16.5 g fat,
`incomplete_values: ["fat_g"]`.

Reply: "Saved Greek yogurt with granola and blueberries (entry 20): 420 kcal,
24.6 g protein, 45.2 g carbs, 16.5 g fat. Yogurt: your earlier 150 g values,
scaled to 200 g. Granola (assumed 40 g) and blueberries (assumed 80 g) are my
rough estimates, not from a food database; I left the blueberries' fat out, so
the fat total is partial. Tell me if you want anything changed."

Later, "the granola was 60 g": read entry 20, then `update_nutrition_entry`
with `expected_revision` and `items` = all three items, granola rescaled to
60 g (270 kcal, 6 g protein, 39 g carbs, 9.75 g fat).

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
