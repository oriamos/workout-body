# Audit of the upstream project, and feature matrix

This records what the inherited code (Training MCP at commit `26ef0ec`) really
did when this project started — read from the code, not from its README — and
where each requirement stands now. "Verified" means an automated test in this
repository exercises it; the test is named.

## 1. What upstream did (from the code)

| Question | Finding at import |
|---|---|
| How `log_set` picked a workout | `ListSessions(today)`, take the newest, else create. **No unique constraint** on date, list-then-insert with no transaction: two concurrent first writes created two sessions for one date. |
| Date / timezone | `time.Now()` formatted in the server's local zone (UTC in the distroless image). **No configurable timezone.** `log_set` accepted no date. |
| Set update / delete | `update_set` / `delete_set` by id existed. Last-write-wins (no revision). Positions renumbered on delete. |
| Retry safety | **None.** A retried `log_set` duplicated sets. |
| Weight / RPE / RIR | Weight, reps and RPE were `NOT NULL` and required; RIR did not exist; weight 0 (bodyweight) was rejected. Plans had no target weight, by design. |
| Partial records | Impossible. |
| Plan import / export | **None.** No JSON format. A plan listing an exercise twice failed at session start *after* inserting the session (orphan). |
| HTTP endpoints | **No JSON API.** Only a server-rendered htmx PWA returning HTML (validation errors as HTTP 200 text), plus `/health` and one JSON helper. |
| Concurrent MCP + HTTP writes | Default journal, deferred transactions, no pool limit: concurrent `AddSet` could hit `SQLITE_BUSY` on lock upgrade. No version or ETag anywhere. |
| History / analytics | Per-exercise history with Epley best, weekly volume and per-muscle volume in a custom "SI" unit; warm-ups did not exist, so everything counted. |
| Authentication | One static bearer token, constant-time compare. No OAuth. MCP over stateless Streamable HTTP only; **no stdio**. |
| Exercise identity | Free-text names as keys in every table; no ids, aliases or ambiguity handling. |
| License | MIT, © Francisco Manuel Olmedo Cortés. Two vendored third-party agent skills had no license text (removed here). `htmx.min.js` 2.0.4 (0BSD) bundled for the PWA. |

README claims that did not match the code: none were found to be false, but the
README says "no concurrent-writer controls" and "no REST", which were accurate
and are what this project changed.

## 2. Feature matrix

**Implemented and verified**

| Capability | Evidence |
|---|---|
| One workout per local date, created on first write, reused by later writes and other conversations | `integration/TestOneWorkoutPerLocalDate`, `e2e/TestAcceptanceFlow` |
| Concurrent first writes → one workout | `integration` (24 concurrent `LogSets` + `EnsureWorkout`), `e2e/TestConcurrentFirstWritesFromMCPAndHTTP`, `apisrv` (12 concurrent POSTs) |
| Date from the configured timezone, incl. both DST transitions | `integration/TestDateBoundariesInJerusalem` (11 cases) |
| Explicit historical dates; future dates refused for sets | `integration` |
| Incremental logging scenario of the brief (3 bench sets, set 2 → 7 reps, squats planned 3×4@130 with 0 done) | `e2e/TestAcceptanceFlow` (MCP writes, HTTP reads), `mcpsrv` |
| Partial sets; nil ≠ 0; clearing a value | `integration`, `mcpsrv` |
| RIR, set types, notes, warm-up exclusion | `integration`, analytics tests |
| Idempotent retries; identical intentional sets allowed; key reuse conflict; across MCP and HTTP | `integration`, `e2e/TestIdempotencyKeyAcrossAdapters` |
| Optimistic concurrency (sets, workouts, plans, prescriptions); one of two racing editors wins | `integration`, `e2e/TestConcurrentEditSameRevisionExactlyOneWins` |
| Correcting one set leaves others untouched | `integration`, `e2e` |
| Plan import/export, dry run, replace, conflict, all-errors validation, atomicity, history preserved after plan edits | `integration`, `apisrv`, `mcpsrv`, `e2e` |
| Stable exercise ids, aliases, no silent merge, rename | `integration` |
| Exercise stats, summaries (hand-computed numbers) | `integration`, `e2e` |
| Migration from upstream data (merged duplicate dates, rebuilt `sets`) | `integration/TestMigrationFromLegacyV9` |
| JSON API: all routes documented and contract-tested; auth on every route | `apisrv` contract test, `e2e/TestAuthenticationOnEveryExposedSurface` |
| MCP tools with annotations, actionable errors; Streamable HTTP + bearer; stdio | `mcpsrv`, `e2e` (real binary, both transports) |
| Set videos and exercise how-to (format sniffing, limits, Range streaming, cleanup, restart) | `integration`, `apisrv`, `e2e/TestSetVideoAndExerciseHowTo` |
| `backup` command; snapshot opens and migrates | `cmd` tests, `integration` |

**Implemented but with known limitations**

| Item | Limitation |
|---|---|
| Upstream features (muscle groups, feedback, landmarks, SI, load suggestions, PWA) | Kept; messages are in Spanish; not extended. They use non-warm-up sets only. |
| Legacy values above the new service limits (weight > 2000 kg, reps > 1000) | Migrated and shown as stored; the limits apply only to new input. |
| `import` with `on_conflict=replace`, and the granular plan-item tools | Explicit overwrites with no revision precondition (use `dry_run`, or `replace_plan` / `PUT /plans/{id}` for the guard). |
| `media-gc` | Spares files younger than one hour; run it against a live server only if you accept that grace period. Concurrent uploads and slow uploads are not rate-limited. |
| Videos | No transcoding or thumbnails; playback needs the bearer token; backup needs the media directory as well as the database. |
| Statistics | Recomputed on read; muscle-group mapping is manual (one group per exercise). |

**Missing / out of scope by decision**

| Item | Note |
|---|---|
| Custom UI | Not built, by design in this phase. |
| OAuth, multi-user, RBAC | Not needed for the single-user case. |
| Rotating A/B/C vs weekday scheduling | Undecided; a plan is just a named routine. |
| Workout-level delete over HTTP/MCP, resumable uploads, webhooks | Not provided. |

**Uncertain pending runtime verification**

| Item | Why |
|---|---|
| Installing the plugin in Claude Code; skill triggering | Files match current docs (reviewed), never installed. |
| Claude Desktop, claude.ai connectors, ChatGPT | Documented header support for claude.ai connectors, not tried; needs a public HTTPS URL. |
| Docker image | Dockerfile reviewed; build status is recorded in the final report. |
| Multi-process access to one SQLite file (stdio + HTTP together) | WAL and busy timeout should cope; not tested. |
