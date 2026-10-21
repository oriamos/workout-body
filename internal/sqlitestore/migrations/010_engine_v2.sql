-- Engine v2.
--
-- 1. One workout per local date: duplicate sessions for a date are merged into
--    the oldest one, then a UNIQUE index enforces the rule from now on.
-- 2. Sets may be partial: weight, reps and RPE become nullable (NULL means
--    "not recorded", which is different from 0 kg). Adds RIR, set_type, notes,
--    a revision counter for optimistic concurrency and a stable exercise id.
-- 3. Stable exercise identity: exercises + aliases. Names stay on the other
--    tables as the canonical lowercase name (renames update them in one
--    transaction), so existing data and queries keep working.
-- 4. Prescriptions (plan_items / session_items) gain optional target weight and
--    RIR. Plans, plan items and sessions get a revision counter.
-- 5. Idempotency records for retry-safe writes.
--
-- Nothing references `sets`, so it can be rebuilt inside the migration
-- transaction (PRAGMA foreign_keys cannot be toggled here, which rules out
-- rebuilding `sessions`; it is altered in place instead).

-- ---- 1. merge duplicate sessions per date --------------------------------
CREATE TEMP TABLE dup_map AS
SELECT s.id AS old_id, k.keep_id AS keep_id
FROM sessions s
JOIN (SELECT date, MIN(id) AS keep_id FROM sessions GROUP BY date) k ON k.date = s.date
WHERE s.id <> k.keep_id;

-- Prescriptions and feedback of merged sessions move to the survivor unless it
-- already has the same exercise / muscle group (survivor wins).
INSERT OR IGNORE INTO session_items (session_id, position, exercise, target_sets, rep_min, rep_max, target_rpe, skipped, superset, notes)
SELECT m.keep_id, i.position + 100000, i.exercise, i.target_sets, i.rep_min, i.rep_max, i.target_rpe, i.skipped, i.superset, i.notes
FROM session_items i JOIN dup_map m ON m.old_id = i.session_id;

INSERT OR IGNORE INTO session_feedback (session_id, muscle_group, fatigue, pump, recovery, created_at)
SELECT m.keep_id, f.muscle_group, f.fatigue, f.pump, f.recovery, f.created_at
FROM session_feedback f JOIN dup_map m ON m.old_id = f.session_id;

-- A survivor that followed no plan inherits the plan snapshot of the oldest
-- merged duplicate that did, so "which plan was this workout" is not lost.
UPDATE sessions SET
 plan_id = (SELECT d.plan_id FROM dup_map m JOIN sessions d ON d.id = m.old_id
            WHERE m.keep_id = sessions.id AND d.plan_name <> '' ORDER BY d.id LIMIT 1),
 plan_name = (SELECT d.plan_name FROM dup_map m JOIN sessions d ON d.id = m.old_id
              WHERE m.keep_id = sessions.id AND d.plan_name <> '' ORDER BY d.id LIMIT 1)
WHERE plan_name = '' AND plan_id IS NULL AND id IN (
 SELECT m.keep_id FROM dup_map m JOIN sessions d ON d.id = m.old_id WHERE d.plan_name <> '');

-- ---- 1b. normalise legacy exercise names ---------------------------------
-- New input is lower-cased with runs of whitespace collapsed. Upstream only
-- lower-cased and trimmed, so a name typed with a doubled space would otherwise
-- stay unreachable by name. Where two spellings collide on a unique key the
-- already-normalised row wins and the duplicate is dropped.
UPDATE sets SET exercise = lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '))) WHERE exercise <> lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' ')));
UPDATE plan_items SET exercise = lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '))) WHERE exercise <> lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' ')));
UPDATE OR IGNORE session_items SET exercise = lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '))) WHERE exercise <> lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' ')));
DELETE FROM session_items WHERE exercise <> lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' ')));
UPDATE OR IGNORE exercise_groups SET exercise = lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '))) WHERE exercise <> lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' ')));
DELETE FROM exercise_groups WHERE exercise <> lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' ')));
UPDATE OR IGNORE exercise_notes SET exercise = lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '))) WHERE exercise <> lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' ')));
DELETE FROM exercise_notes WHERE exercise <> lower(trim(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(exercise, char(9), ' '), char(10), ' '), char(13), ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' '), '  ', ' ')));

-- ---- 3. exercise identity -------------------------------------------------
CREATE TABLE exercises (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 name TEXT NOT NULL UNIQUE,
 created_at TEXT NOT NULL
);
CREATE TABLE exercise_aliases (
 alias TEXT PRIMARY KEY,
 exercise_id INTEGER NOT NULL REFERENCES exercises(id) ON DELETE CASCADE
);
CREATE INDEX idx_exercise_aliases_exercise ON exercise_aliases(exercise_id);

INSERT OR IGNORE INTO exercises (name, created_at)
SELECT DISTINCT name, CURRENT_TIMESTAMP FROM (
 SELECT exercise AS name FROM sets
 UNION SELECT exercise FROM plan_items
 UNION SELECT exercise FROM session_items
 UNION SELECT exercise FROM exercise_groups
 UNION SELECT exercise FROM exercise_notes
) WHERE name <> '' ORDER BY name;

-- ---- 2. rebuild sets ------------------------------------------------------
CREATE TABLE sets_v2 (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 position INTEGER NOT NULL,
 exercise TEXT NOT NULL,
 exercise_id INTEGER NOT NULL REFERENCES exercises(id),
 -- Upper limits (2000 kg, 1000 reps) are enforced by the service for NEW
 -- input only. Upstream never capped these, so the database must accept any
 -- legacy row instead of refusing to start.
 weight_kg REAL CHECK (weight_kg IS NULL OR weight_kg >= 0),
 reps INTEGER CHECK (reps IS NULL OR reps >= 1),
 rpe REAL CHECK (rpe IS NULL OR (rpe >= 1 AND rpe <= 10)),
 rir REAL CHECK (rir IS NULL OR (rir >= 0 AND rir <= 10)),
 set_type TEXT NOT NULL DEFAULT 'working' CHECK (set_type IN ('warmup','working','backoff','drop','failure')),
 notes TEXT NOT NULL DEFAULT '',
 si REAL NOT NULL DEFAULT 0,
 technique TEXT NOT NULL DEFAULT '',
 superset TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(session_id, position)
);

INSERT INTO sets_v2 (id, session_id, position, exercise, exercise_id, weight_kg, reps, rpe, si, technique, superset, created_at, updated_at)
SELECT st.id,
       COALESCE(m.keep_id, st.session_id),
       ROW_NUMBER() OVER (PARTITION BY COALESCE(m.keep_id, st.session_id) ORDER BY st.session_id, st.position, st.id),
       st.exercise, e.id, st.weight_kg, st.reps, st.rpe, st.si, st.technique, st.superset, st.created_at, st.updated_at
FROM sets st
LEFT JOIN dup_map m ON m.old_id = st.session_id
JOIN exercises e ON e.name = st.exercise;

DROP TABLE sets;
ALTER TABLE sets_v2 RENAME TO sets;
CREATE INDEX idx_sets_session_position ON sets(session_id, position);
CREATE INDEX idx_sets_exercise ON sets(exercise_id);
CREATE INDEX idx_sets_technique ON sets(technique) WHERE technique <> '';

-- Drop the merged duplicates (cascades their now-empty items and feedback).
DELETE FROM sessions WHERE id IN (SELECT old_id FROM dup_map);
DROP TABLE dup_map;

CREATE UNIQUE INDEX ux_sessions_date ON sessions(date);
ALTER TABLE sessions ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;

-- ---- 4. prescriptions -----------------------------------------------------
ALTER TABLE plan_items ADD COLUMN target_weight_kg REAL;
ALTER TABLE plan_items ADD COLUMN target_rir REAL;
ALTER TABLE session_items ADD COLUMN target_weight_kg REAL;
ALTER TABLE session_items ADD COLUMN target_rir REAL;
ALTER TABLE plans ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;

-- ---- 5. idempotency -------------------------------------------------------
-- A key is global to the single user. request_hash binds a key to one payload:
-- replaying the same key with the same payload returns the stored response;
-- the same key with a different payload is a conflict.
CREATE TABLE idempotency_keys (
 key TEXT PRIMARY KEY,
 operation TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 response_json TEXT NOT NULL,
 created_at TEXT NOT NULL
);
CREATE INDEX idx_idempotency_created ON idempotency_keys(created_at);
