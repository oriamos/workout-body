# Nutrition tracking

A simple daily food log next to the workout log. You mostly log by talking to an
assistant connected through the plugin: the assistant interprets the message
(and estimates values when you did not give them), then calls MCP tools with
explicit numbers. The engine only validates, stores and summarizes. The web UI
shows daily summaries, lets you browse previous meals and has a small form to
add or edit an entry.

Not included, by design: an in-app chatbot, food recognition from photos,
barcode scanning, an external food database, recipes or meal templates.

## Data model and units

**Entry** — one portion actually consumed:

| Field | Type | Rules |
|---|---|---|
| `id` | integer | Stable, never reused. |
| `date` | `YYYY-MM-DD` | The **local consumption date** in the configured timezone (`WORKOUT_TIMEZONE`, default `Asia/Jerusalem`). Defaults to today; past dates allowed; future dates rejected. |
| `name` | string | 1–200 characters, whitespace collapsed. |
| `calories_kcal` | number or null | kcal for the **whole portion** (not per 100 g). 0–20000. |
| `protein_g`, `carbs_g`, `fat_g` | number or null | grams for the whole portion. 0–2000 each. |
| `items` | array (always present) | The meal's ingredients in order (see below); `[]` for a portion logged with its own values. |
| `incomplete_values` | array of nutrient names | The values (`calories_kcal`, `protein_g`, `carbs_g`, `fat_g`) that only some items provide, so that sum is partial; `[]` otherwise. |
| `copied_from` | integer or null | Set when the entry was created by copying another one (informational). |
| `revision` | integer | Increments on every change; used for `If-Match` / `expected_revision`. |
| `created_at`, `updated_at` | RFC 3339 UTC | Audit timestamps only — summaries use `date`. |

* Decimals are allowed everywhere (e.g. `12.5` g).
* **`null` means "not provided"; `0` is an explicit zero.** A missing value is
  never filled with 0.
* Values must be finite and non-negative; anything else is rejected with a
  field-addressed error and nothing is stored.
* Calories are stored exactly as supplied and are **never recomputed** from the
  macros (labels round, alcohol and fibre exist).
* Nutrition is stored in its own tables. Logging food never creates or touches a
  workout, so it works on rest days.

### Meals made of ingredients

A meal can be logged as a list of **items** (at most 50), each an ingredient
with its own values:

| Item field | Type | Rules |
|---|---|---|
| `name` | string | 1–200 characters, whitespace collapsed. |
| `amount_g` | number or null | Grams of the ingredient used; more than 0, at most 5000. Optional. |
| `calories_kcal`, `protein_g`, `carbs_g`, `fat_g` | number or null | Values **for the amount used** (not per 100 g), same limits as an entry's. |

* The entry's four values are then **computed**: each is the sum of the items
  that provide it (rounded to 0.01), or `null` when no item does — a missing
  item value is never counted as 0. When only some items provide a value, it is
  listed in `incomplete_values`.
* Values given directly on the meal together with `items` are rejected (issue
  on `items`). Issues inside an item name it, e.g. `items[1].amount_g`.
* Updating `items` **replaces the whole list**. A non-empty list recomputes the
  values; values must not be sent alongside it. An empty list removes the items
  and keeps the current sums as the entry's own values, which can then be edited
  directly. Setting a value directly on an entry that has items is rejected
  unless the items are replaced in the same update.
* Copying an entry copies its items.
* **Suggestions:** ingredients used before can be reused. For each ingredient
  name (case-insensitive) the latest use is returned — name, `amount_g`, values,
  `last_used` date and `uses` count — most recently used first, optionally
  filtered by a name substring. An assistant scales those values by `amount_g`
  when the portion differs, and says so.

## Daily summary

For any date (today by default) the engine returns, computed from the stored
entries on every read (no counters that could drift after edits or deletes):

| Per nutrient | Meaning |
|---|---|
| `consumed` | Sum of the entries that provided this value. |
| `entries_missing_value` | Entries without this value — so a total that may be incomplete says so. A meal whose `incomplete_values` lists the nutrient is counted here too, while its known part is included in `consumed`. |
| `target` | The target in effect on that date, or `null` when unset. |
| `remaining` | `max(0, target − consumed)`; `null` without a target. |
| `over` | `max(0, consumed − target)`; `null` without a target. |

The day also lists its entries in the order they were logged, and the targets in
effect (`effective_from`, values, `revision`). Totals are rounded to 0.01.

## Targets (effective-dated)

Daily targets for calories, protein, carbohydrates and fat are optional; each can
be unset. Nothing is ever calculated or recommended automatically.

* A target **change** has an `effective_from` date and applies from that date
  until the next change. The targets for a date are the latest change on or
  before it.
* Changing targets for a date stores a change for that date (or replaces a
  change already made on that exact date). Earlier dates keep what they had, so
  editing "current" targets never alters the targets shown for past days.
  Future-dated changes are allowed and apply only from their date.
* A change is partial: values you omit keep what was in effect on that date;
  `null` (HTTP) or `clear` (MCP) removes a target.
* Removing a change makes the previous change apply again from that date.
* Every change has a `revision` that is unique across all changes and never
  reused. Pass the revision you read (0 when none is set) to make the write fail
  instead of overwriting a change made meanwhile.

## Integrity

* **Retries:** an add or copy may carry an idempotency key (`Idempotency-Key`
  header or `idempotency_key`). The same key with the same payload returns the
  original entry (`replayed: true`) and writes nothing; the same key with a
  different payload is a conflict. Keys share the namespace used by workout
  logging and are kept 90 days.
* **Intentional duplicates** (two identical coffees) are simply two writes
  without a key, or with different keys.
* **Copying** creates a new, independent entry: editing or deleting either one
  never changes the other.
* **Concurrent edits:** updates and deletes check the entry's revision (required
  over HTTP via `If-Match`, optional `expected_revision` over MCP), so a UI edit
  and an assistant edit never silently overwrite each other.

## HTTP API

All under `/api/v1`, bearer-authenticated like the rest. Full schemas in
[`internal/apisrv/openapi.yaml`](../internal/apisrv/openapi.yaml); conventions
(errors, `If-Match`, idempotency) in [http-api.md](http-api.md).

| Method and path | Purpose |
|---|---|
| `GET /nutrition/entries?date` or `?from&to`, `q`, `limit` | List entries (newest date first, chronological within a day); no date filter = latest entries across dates; `q` searches names case-insensitively |
| `POST /nutrition/entries` | Add an entry, optionally with `items` (`Idempotency-Key` optional) → `{entry, replayed}` |
| `GET` / `PATCH` / `DELETE /nutrition/entries/{id}` | Read; partial update (merge-patch, `null` clears a value, `items` replaces the ingredient list and `items: []` removes it, `If-Match` required); delete (`If-Match` required) |
| `POST /nutrition/entries/{id}/copy` | Copy to `{date}` (default today) → new entry (items included) |
| `GET /nutrition/items?q&limit` | Ingredients used before (latest use of each name; `limit` 1–100, default 20) → `{items: [{name, amount_g, calories_kcal, protein_g, carbs_g, fat_g, last_used, uses}]}` |
| `GET /nutrition/days/{date}` | Daily summary (`today` accepted) |
| `GET /nutrition/days?from&to` | One summary per date, empty days included (≤ 366 days) |
| `GET /nutrition/targets?date` | Targets in effect on a date, plus the history of changes |
| `PUT /nutrition/targets/{date}` | Change targets from that date (`If-Match` = revision in effect, `"0"` when none) |
| `DELETE /nutrition/targets/{date}` | Remove the change made on that date |

```sh
curl -X POST "$URL/api/v1/nutrition/entries" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -H "Idempotency-Key: 2026-03-10-breakfast" \
  -d '{"name":"Oatmeal with berries","calories_kcal":350,"protein_g":12.5,"carbs_g":58,"fat_g":7}'

curl -X POST "$URL/api/v1/nutrition/entries" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"Yogurt bowl","items":[
        {"name":"Greek yogurt","amount_g":200,"calories_kcal":194,"protein_g":20,"carbs_g":7.6,"fat_g":10},
        {"name":"Granola","amount_g":40,"calories_kcal":180,"protein_g":4,"carbs_g":26,"fat_g":6.5},
        {"name":"Blueberries","amount_g":80,"calories_kcal":46,"protein_g":0.6,"carbs_g":11.6}]}'
# → entry with calories_kcal 420, protein_g 24.6, carbs_g 45.2, fat_g 16.5,
#   incomplete_values ["fat_g"]

curl -X PUT "$URL/api/v1/nutrition/targets/today" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/merge-patch+json" \
  -H 'If-Match: "0"' -d '{"calories_kcal":2400,"protein_g":150}'
```

## MCP tools

`add_nutrition_entry`, `get_nutrition_entry`, `list_nutrition_entries`,
`update_nutrition_entry`, `delete_nutrition_entry`, `copy_nutrition_entry`,
`get_nutrition_day`, `get_nutrition_targets`, `set_nutrition_targets`,
`suggest_nutrition_items` (read-only). `add_nutrition_entry` and
`update_nutrition_entry` accept `items` with the rules above. Parameters and
descriptions: [mcp-tools.md](mcp-tools.md) (generated from the code).

### Plugin examples

**"Breakfast: oatmeal with berries, 350 kcal, 12 g protein, 58 g carbs, 7 g fat."**
→ `add_nutrition_entry{name:"Oatmeal with berries", calories_kcal:350, protein_g:12, carbs_g:58, fat_g:7}`;
the reply quotes the saved values and id.

**"200 g Greek yogurt with granola and blueberries."** →
`suggest_nutrition_items{query:"yogurt"}` (and the other ingredients) for values
logged before, then one `add_nutrition_entry` with three `items`, each with its
`amount_g` and values for that amount (earlier values scaled by amount, and the
reply says so; estimates labelled as estimates; unknown values omitted). The
meal's totals are computed from the items.

**"Two eggs and toast."** (no numbers) → the assistant may estimate, then logs
and says clearly which values are *its estimates*, never presenting them as
coming from a verified database; you can correct them.

**"Same lunch as yesterday."** → `get_nutrition_day{date: <yesterday in your timezone>}`
(or `list_nutrition_entries`) to find the entry, then
`copy_nutrition_entry{entry_id, date: <today>}`; ask one short question if
yesterday had several candidate lunches.

**"Actually it had 30 g protein."** → read the entry, then
`update_nutrition_entry{entry_id, protein_g:30, expected_revision}`; other values
stay as they were.

**"Set my protein target to 150 g from today."** →
`set_nutrition_targets{protein_g:150}` (calories etc. keep their current values;
past days keep their old targets).

## Web UI

The **תזונה** tab: a day view with previous/next day, a calendar picker and a
Today shortcut (and a clear badge when the selected day is not today); calories
and macros against targets with consumed / remaining / over values ("no target"
when unset, a note when some entries lack a value); the day's entries in order;
an add-meal form with exactly five fields (name, kcal, protein, carbs, fat —
empty means not provided) that uses the selected day as the date; target settings
that apply from the selected day forward; and a browser of previous meals with
search, date filters, details and copy-to-a-date. Changes made through MCP appear
through the same 5-second polling the workout screens use, without interrupting
an open form. See [web/README.md](../web/README.md).

## Storage, migration and backup

* Migration `013_nutrition.sql` adds `nutrition_entries`, `nutrition_targets` and
  a revision sequence. It runs automatically on start and does not modify any
  workout table; databases from earlier versions upgrade in place with an empty
  food log.
* Migration `015_nutrition_items.sql` adds `nutrition_items` (a meal's
  ingredients, deleted with their entry). Existing entries keep their values
  and read back with `items: []`.
* Nutrition lives in the same SQLite file, so `training-mcp backup` and the
  restore procedure ([backup-restore.md](backup-restore.md)) include it with no
  extra steps. Restoring an older backup simply yields no nutrition data (the
  migration runs on start).
* The application is single-user (one bearer token); there are no accounts to
  isolate. All data belongs to the one user of the deployment.

## Known limitations

* No food database: values come from you or from the assistant's estimate.
* The time of day is not stored; entries of a day are ordered by when they were
  logged.
* No meal templates or recipes; previous entries serve as templates via copy,
  and ingredients used before are suggested with their latest values.
* Remote changes reach the UI by polling (every 5 s while visible), not push.
* Upper bounds (20000 kcal / 2000 g per portion) are sanity limits, not
  nutrition advice.
