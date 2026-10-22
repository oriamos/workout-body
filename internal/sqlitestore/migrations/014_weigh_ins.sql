-- Daily body-weight log. One weigh-in per local date (in the configured
-- timezone): logging the same date again replaces it, which also makes retries
-- harmless. Separate from workouts and nutrition.
CREATE TABLE weigh_ins (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 date TEXT NOT NULL UNIQUE,
 weight_kg REAL NOT NULL CHECK (weight_kg > 0),
 note TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
