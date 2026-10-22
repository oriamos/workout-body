-- Nutrition tracking. Entirely separate from workout records: nothing here
-- references sessions or sets, so logging food never creates a workout.
--
-- One row per portion actually consumed. Nutrient values describe the whole
-- portion (not per 100 g). NULL means "not provided"; 0 is an explicit zero.
-- created_at / updated_at are UTC RFC 3339 timestamps; `date` is the LOCAL
-- consumption date in the configured timezone and is what summaries use.
CREATE TABLE nutrition_entries (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 date TEXT NOT NULL,
 name TEXT NOT NULL CHECK (length(name) > 0),
 calories_kcal REAL CHECK (calories_kcal IS NULL OR calories_kcal >= 0),
 protein_g REAL CHECK (protein_g IS NULL OR protein_g >= 0),
 carbs_g REAL CHECK (carbs_g IS NULL OR carbs_g >= 0),
 fat_g REAL CHECK (fat_g IS NULL OR fat_g >= 0),
 -- Set when the entry was created by copying another one. Informational only:
 -- the copy is independent, and deleting the original keeps the copy.
 copied_from INTEGER REFERENCES nutrition_entries(id) ON DELETE SET NULL,
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX idx_nutrition_entries_date ON nutrition_entries(date, created_at, id);

-- Effective-dated daily targets. A row applies from effective_from (a local
-- date) until the next row's date. Changing targets on a date inserts or
-- replaces the row for that date, so earlier dates keep the targets they had.
-- A NULL nutrient means "no target".
CREATE TABLE nutrition_targets (
 effective_from TEXT PRIMARY KEY,
 calories_kcal REAL CHECK (calories_kcal IS NULL OR calories_kcal >= 0),
 protein_g REAL CHECK (protein_g IS NULL OR protein_g >= 0),
 carbs_g REAL CHECK (carbs_g IS NULL OR carbs_g >= 0),
 fat_g REAL CHECK (fat_g IS NULL OR fat_g >= 0),
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);

-- Target revisions come from this sequence so they are unique across all
-- target changes and never reused (AUTOINCREMENT). A client's precondition
-- ("the targets I read were revision N") then identifies one specific change:
-- a newer change on another date can never carry the same number.
CREATE TABLE nutrition_target_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT);
