-- Videos attached to a performed set (form check) or to an exercise
-- (how-to). Only metadata lives in SQLite; the bytes live in the media
-- directory under storage_key, so database backups stay small.
--
-- Deleting a set or an exercise cascades to its media rows. The files are
-- removed by the service (and by `training-mcp media-gc` for anything left).
CREATE TABLE media (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 set_id INTEGER REFERENCES sets(id) ON DELETE CASCADE,
 exercise_id INTEGER REFERENCES exercises(id) ON DELETE CASCADE,
 kind TEXT NOT NULL DEFAULT 'video' CHECK (kind IN ('video')),
 storage_key TEXT NOT NULL UNIQUE,
 filename TEXT NOT NULL,
 content_type TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),
 sha256 TEXT NOT NULL,
 caption TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 CHECK ((set_id IS NULL) <> (exercise_id IS NULL))
);
CREATE INDEX idx_media_set ON media(set_id) WHERE set_id IS NOT NULL;
CREATE INDEX idx_media_exercise ON media(exercise_id) WHERE exercise_id IS NOT NULL;

-- A written explanation of how to perform the exercise. Distinct from
-- exercise_notes, which is a short setup reminder (seat height, grip).
ALTER TABLE exercises ADD COLUMN instructions TEXT NOT NULL DEFAULT '';
