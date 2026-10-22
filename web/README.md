# Web UI (Hebrew, RTL)

React 18 + Vite + TypeScript. By default it reads and writes the **workout engine** through its
JSON API (`/api/v1`, contract: [`internal/apisrv/openapi.yaml`](../internal/apisrv/openapi.yaml)).
The original in-memory prototype is still available as a mock data source.

The UI talks to data only through `WorkoutRepository` (`src/data/repository.ts`):

| `VITE_DATA_SOURCE` | Implementation | Notes |
|---|---|---|
| unset / `api` (default) | `src/data/apiRepository.ts` | real engine through the same-origin proxy |
| `mock` | `createMockRepository` in `repository.ts` | synthetic seed in memory; prototype controls (flask) visible |

## Served by the engine (production)

`scripts/build-ui.sh` (repo root) builds this UI and copies it into
`internal/uisrv/dist`; the next `go build ./cmd/training-mcp` embeds it and the
binary serves it at `/` behind a login page (bearer token once → session
cookie). No proxy and no token in the browser. In that build, a 401 from the API
sends the browser to `/login`, and Settings has a logout button
(`src/lib/session.ts`). The Docker image does all of this.

## Run against the engine (development)

```sh
# 1. the engine (repo root)
export MCP_BEARER_TOKEN="$(openssl rand -hex 32)"
TRAINING_DB_PATH=./data/workout.db go run ./cmd/training-mcp --addr 127.0.0.1:8080

# 2. the UI (web/)
npm install
WORKOUT_API_URL=http://127.0.0.1:8080 WORKOUT_API_TOKEN="$MCP_BEARER_TOKEN" npm run dev
# open http://localhost:5173
```

Optional demo data (5 plans, ~8 weeks of synthetic workouts, today's session in progress), loaded
through the public API only and safe to re-run:

```sh
WORKOUT_API_URL=http://127.0.0.1:8080 WORKOUT_API_TOKEN="$MCP_BEARER_TOKEN" npm run seed:api
#   add -- --skip-today to leave today empty
```

Dates are shifted so the seed's "today" lands on the engine's today (`GET /config`, in the engine's
timezone). The UI's "today" is always the engine's, never the browser clock.

### Environment

| Variable | Used by | Purpose |
|---|---|---|
| `WORKOUT_API_URL` | Vite proxy, scripts | Engine base URL (default `http://127.0.0.1:8080`) |
| `WORKOUT_API_TOKEN` | Vite proxy, scripts | The engine's `MCP_BEARER_TOKEN`. A warning is printed at startup if it is missing |
| `VITE_DATA_SOURCE` | client bundle | `mock` for the in-memory prototype; anything else = engine |

### How auth works

The browser calls relative `/api/v1/...` only. `vite.config.ts` proxies `/api` (both `server.proxy`
for `npm run dev` and `preview.proxy` for `npm run preview`) to `WORKOUT_API_URL` and **adds
`Authorization: Bearer $WORKOUT_API_TOKEN` on the proxy side**. The token is never in client code,
in a `VITE_*` variable or in the bundle. Videos (`/api/v1/media/{id}`) stream through the same proxy.

`npm run dev` and `npm run preview` bind to localhost on purpose: anything that can reach the proxy
can use the token. Do not add `--host` in api mode on an untrusted network.

## Scripts

| Command | What it does |
|---|---|
| `npm run dev` | UI against the engine (proxy + token) |
| `npm run dev:mock` | in-memory prototype (`VITE_DATA_SOURCE=mock`) |
| `npm run build` | `tsc --noEmit && vite build` |
| `npm run seed:api` | load the synthetic seed into a running engine |
| `npm run test:unit` | unit tests: engine to UI mapping, nutrition and weight helpers, week schedule (`scripts/unit-*.mjs`) |
| `npm run test:nutrition` | nutrition end-to-end against a real engine, same setup (`scripts/nutrition-flows.mjs`) |
| `npm run test:weight` | weight end-to-end against a real engine, same setup (`scripts/weight-flows.mjs`) |
| `npm run test:api` | end-to-end against a real engine: starts `go run ./cmd/training-mcp` on a temp DB and Vite itself, drives Chromium (`scripts/api-flows.mjs`) |
| `npm run test:served` | the UI as served by the binary: builds and embeds the UI, builds the binary, drives login → week → weight → nutrition write → logout and checks the CSP (`scripts/served-flows.mjs`) |
| `npm run test:mock` | prototype interaction checks; needs `npm run dev:mock` running on :5173 (`scripts/flows.mjs`) |

The browser scripts use `playwright-core` with Chromium at `/opt/pw-browsers/chromium`.

## What is stored where

**Engine (server, source of truth):** workouts (one per local date, created with
`PUT /workouts/{date}` or `POST /workouts/{date}/plan`), every **completed** set (weight, reps, RPE,
RIR, type), prescriptions copied from plans and their notes, plans (incl. their schedule, kept
untouched when the UI edits a plan), the exercise library, and set videos.

**This browser only (localStorage, UI metadata, per device):**

| Key | Content |
|---|---|
| `wb.exerciseMeta.v1` | per exercise id: equipment, rest time, a muscle-group override |
| `wb.pendingExercises.v1` | exercises added to a workout that have no completed set yet |
| `wb.localNotes.v1` | notes on exercises that have no prescription in that workout |
| `wb.startedAt.v1` | the workout start time shown in the summary |

**Only in the page (lost on reload):** planned rows not yet completed (they are recomputed from the
prescription: `target_sets` minus completed working sets), values typed into a not-completed row,
and a new plan that has no exercise yet.

Defaults: known exercises (the prototype's 15, matched by English name) get the prototype's
Hebrew how-to, equipment and rest time. Otherwise the muscle group comes from the engine (`pecho` ->
chest, ...; unmapped -> "אחר"), rest is 120 s, equipment empty, and the how-to is the engine's
written instructions (`GET /exercises/{id}/guide`) shown as a summary. The demo video is the
exercise's first engine video, else the labelled placeholder clip.

## Behaviour notes

* A set is created on the server when it is marked done (`POST /workouts/{date}/sets` with an
  `Idempotency-Key`, so a retried save never duplicates). Marking it not done deletes it there; the
  row stays in the UI as a draft with its values. Edits are merge-patches with `If-Match`.
* Changes made elsewhere (assistant/MCP) are picked up by polling today's workout every 5 s while the
  tab is visible (and on focus). They arrive as quiet "assistant" updates; a remote set fills the
  first empty planned row. Typed-but-unsaved values are never overwritten (the existing conflict
  prompt appears instead).
* A stale write (`412`) re-reads the workout or plan, shows the fresh value, and marks the save as
  failed with a Hebrew explanation; nothing is overwritten silently. "Retry" then deliberately
  re-applies your value on top of the fresh revision.

## Nutrition (`#/nutrition`)

A separate module in `src/nutrition/` with its own `NutritionRepository` (`repository.ts`): the API
implementation (`apiRepository.ts`, `/api/v1/nutrition/...` through the same proxy) or a deterministic
in-memory mock (`mockRepository.ts`) under `VITE_DATA_SOURCE=mock`. Pure helpers (`logic.ts`) are
unit-tested by `scripts/unit-nutrition.mjs` (sums, partial totals, scaling); `scripts/nutrition-flows.mjs`
drives the UI against a real engine. The mock has synthetic meals with ingredients (some values unknown)
and serves ingredient suggestions from them.

* **Day view** (`#/nutrition/YYYY-MM-DD`; default = the engine's today): previous/next day, a month
  calendar (days with meals marked), a "היום" shortcut and a "לא היום" badge. Calories, protein, carbs
  and fat are each shown as consumed / target with a bar and "נותרו" / "חריגה של". Without a target
  it says "אין יעד" (nothing is invented). Entries without a value add a quiet "חסר ב־N ארוחות". A
  total where every entry lacks the value shows "—", never 0. Totals come from the server (`GET
  /nutrition/days/{date}`).
* **Meals made of ingredients**: a meal is a name plus a list of ingredients (מרכיבים), each with
  its own name, optional amount in grams and optional kcal / protein / carbs / fat. The meal's values
  are the server-computed sums: each value is the sum of the ingredients that provide it, "—" when
  none does, and when only some do the total is partial (`incomplete_values`), shown as a "סכום
  חלקי" badge in the list and "חלקי: חסר ב־N מרכיבים" in the form. A partial meal also counts in the
  day's "חסר ב־N ארוחות". Each meal row in the day view expands to its ingredients.
* **Add/edit** (Sheet): new meals open in ingredients mode with live totals at the bottom (same rules
  as the server). `POST` sends `items` and never the meal-level values; an edit `PATCH`es the whole
  `items` list when it changed. Ingredient names autocomplete from `GET /nutrition/items` (used
  before, latest values); picking one fills its values, and when it has an amount and you type a
  different one the values are scaled (whole kcal, 0.1 g) with a "הותאם לכמות" hint; changing the
  amount later rescales from the picked values (editing a value by hand stops that). The
  "ארוחה פשוטה בלי מרכיבים" toggle shows the old four fields; on a meal with ingredients it converts
  it (`items: []`, the sums stay as editable values). A simple meal offers "פירוק למרכיבים". Empty
  means "not provided" and is omitted or sent as `null`, never 0. Inline validation (number, not
  negative, ≤ 20,000 kcal, ≤ 2,000 g, amount 0–5,000 g, ≤ 50 ingredients). Typed values are kept when
  a save fails. New entries send an `Idempotency-Key`, reused when the same values are retried. Edits
  are merge-patches with `If-Match`. Delete asks for confirmation. Food cannot be logged for a future
  day (the engine rejects it).
* **Targets** (Sheet): shows the targets in effect on the selected day and their `effective_from`,
  then changes them from that day forward (`PUT /nutrition/targets/{date}`, `If-Match` = revision in
  effect, `"0"` when none). Earlier days keep their targets. An empty field removes a target.
* **Browse** (Sheet): search by name, from/to dates, details (with the ingredient count and list), and
  copy to today or another date (`POST /nutrition/entries/{id}/copy` with an idempotency key; the copy
  keeps the ingredients).
* **Changes made elsewhere**: the visible day is polled every 5 s while the tab is visible. Open forms
  keep their own values. A `412` keeps your typed values, shows the server's current values, and
  makes another save an explicit overwrite.

Nutrition data lives on the engine only; nothing nutrition-related is kept in localStorage.

## Week (home, `#/today`)

The first tab, "השבוע", is this week: seven days from the week's first day (Sunday→Saturday by
default; the UI takes the first day from the engine's default `GET /schedule` week, `days[0].weekday`,
so it follows `WORKOUT_WEEK_START`). Each day shows the workout done (name, completed sets, volume)
and the plans scheduled for it with a status chip (בוצע / התחיל / פוספס / היום / מתוכנן, the engine's
`done / started / missed / due / upcoming`). Today is highlighted (`aria-current="date"`) and carries
the main action: "פתח את האימון של היום" when a workout exists, otherwise "התחל אימון · <plan>" with
the plan scheduled for today (or a still-due weekly-frequency plan) preselected, a choice when several
are due, and "אימון אחר" for the existing chooser. A summary shows workouts done vs. planned this week
and one line per frequency plan ("Upper/Lower: 2 מתוך 3 השבוע, נותר 1"). Arrows move a week back or
forward (`GET /schedule?from&to`, `#/today/<first day>`), "השבוע" returns to the current week. A past
day opens that workout (History), a future day links to its plan. With no plan scheduled the screen
explains schedules and links to Plans, where each plan has a "לוח זמנים" editor (none / fixed days /
times per week; saved with `PUT /plans/{id}/schedule`).

The logging view moved to `#/workout/<date>` (`#/workout` = today) with a back button to the week;
logging, polling and conflict handling are unchanged. The view is re-read from the engine whenever a
workout of the shown week changes in the UI (own saves, or the assistant's picked up by polling). In
the mock the same view is computed from the in-memory data (`src/data/schedule.ts`, unit-tested by
`scripts/unit-schedule.mjs`); the seed gives Upper A/Lower A/Upper B/Lower B fixed days (Mon/Tue/Thu/Fri)
and Full Body once a week.

## Weight (`#/weight`)

Reached from the scale icon in the page header (next to Progress; it is not a bottom tab).
`src/weight/WeighInCard.tsx` exports a small self-contained "שקילה היום" card (today's weight, or a
"רישום שקילה" button; both open `#/weight`) for a home screen to place; the week screen (`#/today`) shows it for the current week. A separate module in `src/weight/` with its own
`WeightRepository` (`repository.ts`): the API implementation (`apiRepository.ts`, `/api/v1/weight/...`
through the same proxy) or a deterministic in-memory mock (`mockRepository.ts`: about ten weeks with
skipped days and one empty week) under `VITE_DATA_SOURCE=mock`. Pure helpers (`logic.ts`, including a
copy of the engine's weekly computation used by the mock) are unit-tested by `scripts/unit-weight.mjs`.

* **Quick log**: one weigh-in per date. Weight in kg (`inputmode=decimal`; "81,4" works; 20–400),
  optional note (≤ 200), and a date picker for past dates (max = today). If today already has a
  weigh-in it is shown with "עריכה". Saving is `PUT /weight/entries/{date}` with `If-Match` = the
  revision being replaced, or `"0"` when the date had none, so a weigh-in logged elsewhere in the
  meantime is never overwritten silently. Choosing a date that already has one prefills it and says
  saving will update it.
* **Headline**: this week's average (Sunday→Saturday by default; the week bounds come from the API, with "N שקילות") and the change vs last week in kg
  and %, as `GET /weight/trend` reports them, with an arrow and "עלייה / ירידה / ללא שינוי משמעותי"
  in neutral colors (no red/green judgement). When the engine has no comparison (either week without
  a weigh-in) it says "אין מספיק נתונים להשוואה".
* **Chart**: daily weigh-ins as points plus the weekly average as a line (a week without weigh-ins is a
  gap); range 4/8/12/26 weeks (remembered per browser).
* **By week** table (newest first): range, average, days logged, min–max, change vs previous week.
* **Recent weigh-ins** in the range with edit (Sheet) and delete (inline confirmation, `If-Match`).
* A caption explains that daily weight fluctuates (water, salt, food) and the weekly average is the
  meaningful signal.
* **Changes made elsewhere**: the trend is polled every 5 s while the tab is visible. Open forms keep
  their values; a `412` shows the server's current value in Hebrew, keeps what you typed, and makes
  another save an explicit overwrite.

Weight data lives on the engine only (the chart range is the only thing kept in localStorage).

## Limitations

* **Start time is not stored by the engine**: it is kept in localStorage on the device that started
  the workout; elsewhere the summary shows "—".
* **Unplanned exercises without a completed set, and notes on exercises without a prescription,
  stay local** to this browser until (for exercises) the first set is completed.
* **Polling, not push**: remote changes appear within ~5 s, only for today's workout (and the nutrition day or weight trend on screen); other
  days refresh on reload. Changes to an existing prescription's targets made elsewhere also appear
  only after reload.
* Set videos are loaded for today's workout only ("הסרטונים שלי" shows today's), one video per set.
* A set can hold a video only once it is done (the engine stores performed sets only).
* **Single static token held by the dev/preview proxy.** There are no users or login. For anything
  beyond a local machine, serve the built `dist/` behind a reverse proxy that injects the bearer
  token for `/api/` **and** has its own authentication (and HTTPS) in front of it.
