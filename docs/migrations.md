# Migrations

## How they work

SQL files in `internal/sqlitestore/migrations/` are embedded in the binary and
applied at startup, in numeric order, each exactly once. A migration and its row
in `schema_migrations` commit in one transaction, so a failure never leaves a
half-applied schema. There are no down migrations: restore a backup to go back.

## Adding one

1. Create the next file, zero-padded: `012_short_name.sql` (010 and 011 exist). Never edit one that
   has shipped.
2. `ALTER TABLE … ADD COLUMN` needs a default if the column is `NOT NULL`.
3. Making a column nullable or adding a `CHECK` needs a table rebuild
   (create new → copy → drop → rename). `PRAGMA foreign_keys` cannot be changed
   inside the migration transaction, so dropping a table that other tables
   reference cascades into them. `sets` was rebuilt in 010 only because nothing
   referenced it then; since 011, `media` references `sets` and `exercises`, so
   alter those (and `sessions`) in place instead of rebuilding them.
4. Add a test in `internal/integration` that builds a database at the previous
   version with realistic legacy rows, opens it with the new code, and asserts
   the data survived.

## Upgrading from upstream Training MCP (versions ≤ 009)

Open your existing database with this version; migration `010_engine_v2.sql` runs
automatically. **Back up first** ([backup-restore.md](backup-restore.md)). It:

* **Merges duplicate sessions that share a date** into the oldest one: their
  sets are appended in original order with dense positions, prescriptions and
  feedback move over (the surviving session wins ties), and the extra sessions
  are deleted. Upstream allowed several sessions per date; this version does
  not.
* Adds a `UNIQUE` index on `sessions(date)`.
* **Normalizes legacy exercise names** (lower case, runs of whitespace collapsed,
  as new input is) in sets, plans, prescriptions, the muscle-group catalogue and
  notes, so a name stored with a doubled space stays reachable. If two spellings
  collide on a unique key (the same exercise twice in one workout's
  prescription), the already-normalized row wins.
* Rebuilds `sets` with nullable `weight_kg`/`reps`/`rpe`, new `rir`, `set_type`
  (existing sets become `working`), `notes`, `revision`, `exercise_id`, and
  `AUTOINCREMENT` ids. Existing ids and values are preserved.
* Creates `exercises` and `exercise_aliases` and back-fills them from every
  exercise name already in sets, plans, prescriptions, the muscle-group catalogue
  and notes.
* Adds `target_weight_kg`/`target_rir` to plan and session prescriptions and
  `revision` to plans and sessions.
* Adds `idempotency_keys`.

Migration `012_plan_schedule.sql` adds the optional plan schedule columns
(existing plans simply have none). Migration `011_media_and_instructions.sql` adds the `media` table (video
metadata; the files live in `MEDIA_DIR`) and `exercises.instructions`. Both
run automatically on first start.

Data written by the older version stays valid: it simply has no RIR/notes and
every set is a working set.

Not reversible in place: after the upgrade the old binary cannot open the
database correctly (merged sessions, rebuilt `sets`). Keep the pre-upgrade
backup if you might go back.
