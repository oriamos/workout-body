-- An optional fixed schedule stored on the plan itself:
--   schedule_type '' (none)            - the plan has no schedule
--   schedule_type 'weekdays'           - schedule_days = comma-separated, e.g. 'mon,thu'
--   schedule_type 'times_per_week'     - schedule_times = 1..7, any days
-- Workouts already created from the plan are never affected: a schedule says
-- when a plan is meant to be done, it does not create or change workouts.
ALTER TABLE plans ADD COLUMN schedule_type TEXT NOT NULL DEFAULT ''
  CHECK (schedule_type IN ('', 'weekdays', 'times_per_week'));
ALTER TABLE plans ADD COLUMN schedule_days TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN schedule_times INTEGER NOT NULL DEFAULT 0;
