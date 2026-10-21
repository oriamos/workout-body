package sqlitestore

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/fullfran/training-mcp/internal/training"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store is the SQLite adapter. The unexported failpoint is only used by
// same-package tests to prove transaction rollback at an intermediate write boundary.
type Store struct {
	db        *sql.DB
	failpoint func(string) error
}

func Open(path string) (*Store, error) {
	// WAL lets readers run beside the single writer; _txlock=immediate makes
	// every transaction take the write lock up front, so concurrent writers queue
	// on busy_timeout instead of deadlocking on a read-to-write upgrade.
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(ON)&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err = migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// migrate applies each migration exactly once, recording its version in
// schema_migrations. Version tracking is required rather than replaying: some
// migrations use ALTER TABLE, which is not idempotent.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	applied := map[int]bool{}
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err = rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}

	for _, name := range mustMigrationNames() {
		version, err := strconv.Atoi(strings.SplitN(strings.TrimPrefix(name, "migrations/"), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %q has no numeric version prefix", name)
		}
		if applied[version] {
			continue
		}
		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return err
		}
		// Each migration and its bookkeeping commit together, so a failure
		// never leaves a half-applied schema marked as done.
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err = tx.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, version); err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func mustMigrationNames() []string {
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		panic(err)
	}
	sort.Strings(names)
	return names
}
func (s *Store) Close() error { return s.db.Close() }

// Snapshot writes a consistent copy of the whole database to path. VACUUM INTO
// takes the copy inside a read transaction, so it is safe while the server is
// serving and the result is a plain SQLite file that can simply be copied back.
func (s *Store) Snapshot(ctx context.Context, path string) error {
	if strings.ContainsAny(path, "'\"") {
		return training.ErrValidation
	}
	_, err := s.db.ExecContext(ctx, `VACUUM INTO '`+path+`'`)
	return err
}

func (s *Store) ListSessions(ctx context.Context, f training.ListFilter) ([]training.SessionSummary, error) {
	q := `SELECT s.id,s.date,COUNT(st.id),COALESCE(SUM(st.si),0),s.plan_name FROM sessions s LEFT JOIN sets st ON st.session_id=s.id WHERE 1=1`
	args := []any{}
	if f.From != "" {
		q += ` AND s.date>=?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND s.date<=?`
		args = append(args, f.To)
	}
	q += ` GROUP BY s.id ORDER BY s.date DESC,s.id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.SessionSummary
	for rows.Next() {
		var v training.SessionSummary
		if err = rows.Scan(&v.ID, &v.Date, &v.SetCount, &v.TotalSI, &v.PlanName); err != nil {
			return nil, err
		}
		v.TotalSI = training.NormalizeSI(v.TotalSI)
		out = append(out, v)
	}
	return out, rows.Err()
}

// RecentExercises returns the newest set of each distinct exercise, most
// recently used first. MAX(id) picks the latest row per exercise because ids
// are monotonic within a table lifetime.
func (s *Store) RecentExercises(ctx context.Context, limit int) ([]training.ExerciseMemory, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT exercise,weight_kg,reps,rpe FROM sets
WHERE id IN (SELECT MAX(id) FROM sets WHERE set_type<>'warmup' GROUP BY exercise)
ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.ExerciseMemory
	for rows.Next() {
		var v training.ExerciseMemory
		var w, r sql.NullFloat64
		var reps sql.NullInt64
		if err = rows.Scan(&v.Exercise, &w, &reps, &r); err != nil {
			return nil, err
		}
		v.WeightKG, v.Reps, v.RPE = ptrF(w), ptrI(reps), ptrF(r)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) CreatePlan(ctx context.Context, p training.Plan) (training.Plan, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.Plan{}, err
	}
	defer tx.Rollback()
	if other, taken, err := planNameTaken(ctx, tx, p.Name, 0); err != nil {
		return training.Plan{}, err
	} else if taken {
		return training.Plan{}, errPlanNameTaken(p.Name, other)
	}
	id, _, err := insertPlan(ctx, tx, p)
	if err != nil {
		return training.Plan{}, err
	}
	out, err := loadPlan(ctx, tx, id)
	if err != nil {
		return training.Plan{}, err
	}
	return out, tx.Commit()
}

func (s *Store) ListPlans(ctx context.Context) ([]training.Plan, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT p.id,p.name,p.notes,COALESCE(SUM(i.target_sets),0),COUNT(i.id),p.revision,p.schedule_type,p.schedule_days,p.schedule_times
FROM plans p LEFT JOIN plan_items i ON i.plan_id=p.id
GROUP BY p.id ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.Plan
	for rows.Next() {
		var p training.Plan
		var items, times int
		var typ, days string
		if err = rows.Scan(&p.ID, &p.Name, &p.Notes, &p.TotalSets, &items, &p.Revision, &typ, &days, &times); err != nil {
			return nil, err
		}
		p.Schedule = decodeSchedule(typ, days, times)
		// Items are left empty here; the list view only needs the totals.
		p.Items = make([]training.PlanItem, 0, items)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetPlan(ctx context.Context, id int64) (training.Plan, error) {
	return loadPlan(ctx, s.db, id)
}

// UpdatePlan edits a plan's name and notes, leaving its exercises alone.
func (s *Store) UpdatePlan(ctx context.Context, id int64, p training.PlanPatch) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var name, notes string
	err = tx.QueryRowContext(ctx, `SELECT name,notes FROM plans WHERE id=?`, id).Scan(&name, &notes)
	if errors.Is(err, sql.ErrNoRows) {
		return training.ErrNotFound
	}
	if err != nil {
		return err
	}
	if p.Name != nil {
		name = *p.Name
		if other, taken, err := planNameTaken(ctx, tx, name, id); err != nil {
			return err
		} else if taken {
			return errPlanNameTaken(name, other)
		}
	}
	if p.Notes != nil {
		notes = *p.Notes
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plans SET name=?,notes=?,revision=revision+1 WHERE id=?`, name, notes, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetPlanItem adds an exercise to a plan, or replaces its prescription if it is
// already there. New items go last. Matched by exercise name in code rather
// than by a unique constraint, so existing plans need no schema change.
func (s *Store) SetPlanItem(ctx context.Context, planID int64, it training.PlanItem) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM plans WHERE id=?`, planID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return training.ErrNotFound
	}
	ex, err := resolveExercise(ctx, tx, 0, it.Exercise)
	if err != nil {
		return err
	}
	it.Exercise = ex.name
	var pos int
	err = tx.QueryRowContext(ctx, `SELECT position FROM plan_items WHERE plan_id=? AND exercise=?`, planID, it.Exercise).Scan(&pos)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),0)+1 FROM plan_items WHERE plan_id=?`, planID).Scan(&pos); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO plan_items(plan_id,position,exercise,target_sets,rep_min,rep_max,target_rpe,target_weight_kg,target_rir,superset,notes) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			planID, pos, it.Exercise, it.TargetSets, it.RepMin, it.RepMax, it.TargetRPE, nf(it.TargetWeightKG), nf(it.TargetRIR), it.Superset, it.Notes)
	case err != nil:
		return err
	default:
		_, err = tx.ExecContext(ctx, `UPDATE plan_items SET target_sets=?,rep_min=?,rep_max=?,target_rpe=?,target_weight_kg=?,target_rir=?,superset=?,notes=? WHERE plan_id=? AND exercise=?`,
			it.TargetSets, it.RepMin, it.RepMax, it.TargetRPE, nf(it.TargetWeightKG), nf(it.TargetRIR), it.Superset, it.Notes, planID, it.Exercise)
	}
	if err != nil {
		return err
	}
	if err = bumpPlan(ctx, tx, planID); err != nil {
		return err
	}
	return tx.Commit()
}

// RemovePlanItem drops an exercise and closes the gap in the ordering.
func (s *Store) RemovePlanItem(ctx context.Context, planID int64, exercise string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM plan_items WHERE plan_id=? AND exercise=?`, planID, exercise)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return training.ErrNotFound
	}
	if err = resequencePlan(ctx, tx, planID); err != nil {
		return err
	}
	if err = bumpPlan(ctx, tx, planID); err != nil {
		return err
	}
	return tx.Commit()
}

// MovePlanItem shifts an exercise up or down by one place. Order matters: it is
// the order the session is performed in.
func (s *Store) MovePlanItem(ctx context.Context, planID int64, exercise string, delta int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var pos int
	err = tx.QueryRowContext(ctx, `SELECT position FROM plan_items WHERE plan_id=? AND exercise=?`, planID, exercise).Scan(&pos)
	if errors.Is(err, sql.ErrNoRows) {
		return training.ErrNotFound
	}
	if err != nil {
		return err
	}
	var neighbour string
	var neighbourPos int
	order := "ASC"
	cmp := ">"
	if delta < 0 {
		order, cmp = "DESC", "<"
	}
	err = tx.QueryRowContext(ctx,
		`SELECT exercise,position FROM plan_items WHERE plan_id=? AND position`+cmp+`? ORDER BY position `+order+` LIMIT 1`,
		planID, pos).Scan(&neighbour, &neighbourPos)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // already at the end; moving further is a no-op, not an error
	}
	if err != nil {
		return err
	}
	// Park one row out of the way: (plan_id, position) is unique.
	if _, err = tx.ExecContext(ctx, `UPDATE plan_items SET position=-1 WHERE plan_id=? AND exercise=?`, planID, exercise); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plan_items SET position=? WHERE plan_id=? AND exercise=?`, pos, planID, neighbour); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plan_items SET position=? WHERE plan_id=? AND exercise=?`, neighbourPos, planID, exercise); err != nil {
		return err
	}
	if err = bumpPlan(ctx, tx, planID); err != nil {
		return err
	}
	return tx.Commit()
}

// resequencePlan compacts positions to 1..n in their current order.
func resequencePlan(ctx context.Context, tx *sql.Tx, planID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT exercise FROM plan_items WHERE plan_id=? ORDER BY position`, planID)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var n string
		if err = rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		names = append(names, n)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	// Park them all negative first, or renumbering collides with the unique key.
	for i, n := range names {
		if _, err = tx.ExecContext(ctx, `UPDATE plan_items SET position=? WHERE plan_id=? AND exercise=?`, -(i + 1), planID, n); err != nil {
			return err
		}
	}
	for i, n := range names {
		if _, err = tx.ExecContext(ctx, `UPDATE plan_items SET position=? WHERE plan_id=? AND exercise=?`, i+1, planID, n); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeletePlan(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM plans WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return training.ErrNotFound
	}
	return nil
}

// SessionProgress lists the session's planned exercises with how many sets are
// done, then any exercise performed that the plan did not prescribe. Only
// non-warm-up sets count as done; planned sets never count as done.
func (s *Store) SessionProgress(ctx context.Context, sessionID int64) ([]training.PlanProgress, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id=?`, sessionID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, training.ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT i.superset, i.notes, i.exercise, i.target_sets, i.rep_min, i.rep_max, i.target_rpe, i.target_weight_kg, i.target_rir, i.skipped,
       (SELECT COUNT(*) FROM sets st WHERE st.session_id=i.session_id AND st.exercise=i.exercise AND st.set_type<>'warmup'),
       (SELECT COUNT(*) FROM sets st WHERE st.session_id=i.session_id AND st.exercise=i.exercise AND st.set_type='warmup'),
       COALESCE(g.muscle_group,'')
FROM session_items i LEFT JOIN exercise_groups g ON g.exercise=i.exercise
WHERE i.session_id=? ORDER BY i.position`, sessionID)
	if err != nil {
		return nil, err
	}
	planned := map[string]bool{}
	var out []training.PlanProgress
	for rows.Next() {
		var v training.PlanProgress
		var tw, tr sql.NullFloat64
		if err = rows.Scan(&v.Superset, &v.Notes, &v.Exercise, &v.TargetSets, &v.RepMin, &v.RepMax, &v.TargetRPE, &tw, &tr, &v.Skipped, &v.DoneSets, &v.WarmupSets, &v.MuscleGroup); err != nil {
			rows.Close()
			return nil, err
		}
		v.TargetWeightKG, v.TargetRIR = ptrF(tw), ptrF(tr)
		planned[v.Exercise] = true
		out = append(out, v)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}

	extra, err := s.db.QueryContext(ctx, `
SELECT st.exercise, SUM(st.set_type<>'warmup'), SUM(st.set_type='warmup'), COALESCE(g.muscle_group,'')
FROM sets st LEFT JOIN exercise_groups g ON g.exercise=st.exercise
WHERE st.session_id=? GROUP BY st.exercise ORDER BY MIN(st.position)`, sessionID)
	if err != nil {
		return nil, err
	}
	defer extra.Close()
	for extra.Next() {
		var v training.PlanProgress
		if err = extra.Scan(&v.Exercise, &v.DoneSets, &v.WarmupSets, &v.MuscleGroup); err != nil {
			return nil, err
		}
		if !planned[v.Exercise] {
			out = append(out, v)
		}
	}
	return out, extra.Err()
}

// SetSessionItem adds an exercise to today's session plan, or replaces its
// prescription if it is already there. New items go last.
func (s *Store) SetSessionItem(ctx context.Context, sessionID int64, it training.PlanItem) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id=?`, sessionID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return training.ErrNotFound
	}
	ex, err := resolveExercise(ctx, tx, 0, it.Exercise)
	if err != nil {
		return err
	}
	it.Exercise = ex.name
	var pos int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),0)+1 FROM session_items WHERE session_id=?`, sessionID).Scan(&pos); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO session_items (session_id,position,exercise,target_sets,rep_min,rep_max,target_rpe,target_weight_kg,target_rir,superset,notes)
VALUES (?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(session_id,exercise) DO UPDATE SET
 target_sets=excluded.target_sets, rep_min=excluded.rep_min,
 rep_max=excluded.rep_max, target_rpe=excluded.target_rpe,
 target_weight_kg=excluded.target_weight_kg, target_rir=excluded.target_rir,
 superset=excluded.superset, notes=excluded.notes`,
		sessionID, pos, it.Exercise, it.TargetSets, it.RepMin, it.RepMax, it.TargetRPE, nf(it.TargetWeightKG), nf(it.TargetRIR), it.Superset, it.Notes); err != nil {
		return err
	}
	if err = bumpWorkout(ctx, tx, sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

// PatchSessionItem changes only the fields given, so bumping the set count does
// not require restating the rep range.
func (s *Store) PatchSessionItem(ctx context.Context, sessionID int64, exercise string, p training.SessionItemPatch) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkWorkoutRevision(ctx, tx, sessionID, p.ExpectedRevision); err != nil {
		return err
	}
	var it training.PlanItem
	var skipped bool
	var tw, tr sql.NullFloat64
	err = tx.QueryRowContext(ctx, `SELECT exercise,target_sets,rep_min,rep_max,target_rpe,target_weight_kg,target_rir,skipped,notes FROM session_items WHERE session_id=? AND exercise=?`,
		sessionID, exercise).Scan(&it.Exercise, &it.TargetSets, &it.RepMin, &it.RepMax, &it.TargetRPE, &tw, &tr, &skipped, &it.Notes)
	if errors.Is(err, sql.ErrNoRows) {
		return training.ErrNotFound
	}
	if err != nil {
		return err
	}
	it.TargetWeightKG, it.TargetRIR = ptrF(tw), ptrF(tr)
	if p.TargetSets != nil {
		it.TargetSets = *p.TargetSets
	}
	if p.RepMin != nil {
		it.RepMin = *p.RepMin
	}
	if p.RepMax != nil {
		it.RepMax = *p.RepMax
	}
	if p.TargetRPE != nil {
		it.TargetRPE = *p.TargetRPE
	}
	switch {
	case p.ClearTargetWeight:
		it.TargetWeightKG = nil
	case p.TargetWeightKG != nil:
		it.TargetWeightKG = p.TargetWeightKG
	}
	switch {
	case p.ClearTargetRIR:
		it.TargetRIR = nil
	case p.TargetRIR != nil:
		it.TargetRIR = p.TargetRIR
	}
	if p.Skipped != nil {
		skipped = *p.Skipped
	}
	if p.Notes != nil {
		it.Notes = *p.Notes
	}
	if err = invertedRange(it); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE session_items SET target_sets=?,rep_min=?,rep_max=?,target_rpe=?,target_weight_kg=?,target_rir=?,skipped=?,notes=? WHERE session_id=? AND exercise=?`,
		it.TargetSets, it.RepMin, it.RepMax, it.TargetRPE, nf(it.TargetWeightKG), nf(it.TargetRIR), skipped, it.Notes, sessionID, exercise); err != nil {
		return err
	}
	if err = bumpWorkout(ctx, tx, sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

// SwapSessionItem substitutes one exercise for another in place, keeping its
// position and prescription. This is the occupied-machine case.
func (s *Store) SwapSessionItem(ctx context.Context, sessionID int64, from, to string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ex, err := resolveExercise(ctx, tx, 0, to)
	if err != nil {
		return err
	}
	var already int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_items WHERE session_id=? AND exercise=? AND exercise<>?`, sessionID, ex.name, from).Scan(&already); err != nil {
		return err
	}
	if already > 0 {
		return fmt.Errorf("%w: %q is already prescribed in this workout; adjust that prescription or remove one of them first", training.ErrConflict, ex.name)
	}
	res, err := tx.ExecContext(ctx, `UPDATE session_items SET exercise=? WHERE session_id=? AND exercise=?`, ex.name, sessionID, from)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return training.ErrNotFound
	}
	if err = bumpWorkout(ctx, tx, sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveSessionItem drops an exercise from the workout's prescription. Sets
// already logged against it are untouched and resurface as off-plan work.
func (s *Store) RemoveSessionItem(ctx context.Context, sessionID int64, exercise string, expected *int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkWorkoutRevision(ctx, tx, sessionID, expected); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM session_items WHERE session_id=? AND exercise=?`, sessionID, exercise)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return training.ErrNotFound
	}
	if err = bumpWorkout(ctx, tx, sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

// checkWorkoutRevision fails with ErrNotFound for an unknown workout and with
// ErrConflict when expected is set and stale.
func checkWorkoutRevision(ctx context.Context, q dbtx, sessionID int64, expected *int) error {
	var rev int
	err := q.QueryRowContext(ctx, `SELECT revision FROM sessions WHERE id=?`, sessionID).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return training.ErrNotFound
	}
	if err != nil {
		return err
	}
	if expected != nil && *expected != rev {
		return fmt.Errorf("%w: workout is at revision %d, not %d; re-read it and retry", training.ErrStale, rev, *expected)
	}
	return nil
}

func (s *Store) SetFeedback(ctx context.Context, sessionID int64, f training.Feedback) error {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id=?`, sessionID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return training.ErrNotFound
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO session_feedback (session_id,muscle_group,fatigue,pump,recovery,created_at)
VALUES (?,?,?,?,?,CURRENT_TIMESTAMP)
ON CONFLICT(session_id,muscle_group) DO UPDATE SET
 fatigue=excluded.fatigue, pump=excluded.pump, recovery=excluded.recovery, created_at=CURRENT_TIMESTAMP`,
		sessionID, f.MuscleGroup, f.Fatigue, f.Pump, f.Recovery)
	return err
}

func (s *Store) SessionFeedback(ctx context.Context, sessionID int64) ([]training.Feedback, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT muscle_group,fatigue,pump,recovery FROM session_feedback WHERE session_id=? ORDER BY muscle_group`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.Feedback
	for rows.Next() {
		var f training.Feedback
		if err = rows.Scan(&f.MuscleGroup, &f.Fatigue, &f.Pump, &f.Recovery); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// LatestFeedback returns the most recent rating per muscle group, which is what
// next week's volume should respond to.
func (s *Store) LatestFeedback(ctx context.Context) (map[string]training.Feedback, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT muscle_group,fatigue,pump,recovery FROM session_feedback
WHERE id IN (SELECT MAX(id) FROM session_feedback GROUP BY muscle_group)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]training.Feedback{}
	for rows.Next() {
		var f training.Feedback
		if err = rows.Scan(&f.MuscleGroup, &f.Fatigue, &f.Pump, &f.Recovery); err != nil {
			return nil, err
		}
		out[f.MuscleGroup] = f
	}
	return out, rows.Err()
}

func (s *Store) SetExerciseNote(ctx context.Context, n training.ExerciseNote) error {
	if strings.TrimSpace(n.Note) == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM exercise_notes WHERE exercise=?`, n.Exercise)
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ex, err := resolveExercise(ctx, tx, 0, n.Exercise)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO exercise_notes(exercise,note,updated_at) VALUES (?,?,CURRENT_TIMESTAMP)
ON CONFLICT(exercise) DO UPDATE SET note=excluded.note, updated_at=CURRENT_TIMESTAMP`,
		ex.name, n.Note); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ExerciseNotes(ctx context.Context) ([]training.ExerciseNote, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT exercise,note FROM exercise_notes ORDER BY exercise`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.ExerciseNote
	for rows.Next() {
		var n training.ExerciseNote
		if err = rows.Scan(&n.Exercise, &n.Note); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// SupersetFor reports the superset label an exercise carries in a session's
// plan, so a logged set can be stamped without the caller stating it.
func (s *Store) SupersetFor(ctx context.Context, sessionID int64, exercise string) (string, error) {
	var label string
	err := s.db.QueryRowContext(ctx, `SELECT superset FROM session_items WHERE session_id=? AND exercise=?`,
		sessionID, exercise).Scan(&label)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return label, err
}

// RecentFeedback returns up to limit most recent ratings per muscle group,
// newest first, so a recommendation can look at a trend rather than a moment.
func (s *Store) RecentFeedback(ctx context.Context, limit int) (map[string][]training.Feedback, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT muscle_group,fatigue,pump,recovery FROM session_feedback ORDER BY muscle_group, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]training.Feedback{}
	for rows.Next() {
		var f training.Feedback
		if err = rows.Scan(&f.MuscleGroup, &f.Fatigue, &f.Pump, &f.Recovery); err != nil {
			return nil, err
		}
		if len(out[f.MuscleGroup]) < limit {
			out[f.MuscleGroup] = append(out[f.MuscleGroup], f)
		}
	}
	return out, rows.Err()
}

func (s *Store) SetLandmarks(ctx context.Context, l training.Landmarks) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO volume_landmarks(muscle_group,mev,mrv,updated_at) VALUES (?,?,?,CURRENT_TIMESTAMP)
ON CONFLICT(muscle_group) DO UPDATE SET mev=excluded.mev, mrv=excluded.mrv, updated_at=CURRENT_TIMESTAMP`,
		l.MuscleGroup, l.MEV, l.MRV)
	return err
}

func (s *Store) AllLandmarks(ctx context.Context) (map[string]training.Landmarks, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT muscle_group,mev,mrv FROM volume_landmarks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]training.Landmarks{}
	for rows.Next() {
		var l training.Landmarks
		if err = rows.Scan(&l.MuscleGroup, &l.MEV, &l.MRV); err != nil {
			return nil, err
		}
		out[l.MuscleGroup] = l
	}
	return out, rows.Err()
}

// DeleteSession removes a session and, by ON DELETE CASCADE, all of its sets.
// It returns how many sets went with it so the caller can report the cost of an
// irreversible delete.
func (s *Store) DeleteSession(ctx context.Context, id int64) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var n int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sets WHERE session_id=?`, id).Scan(&n)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, training.ErrNotFound
	}
	return n, tx.Commit()
}

func (s *Store) ExerciseHistory(ctx context.Context, exercise string, limit int) (training.ExerciseHistory, error) {
	out := training.ExerciseHistory{Exercise: exercise, Sets: []training.ExerciseSet{}}
	err := s.db.QueryRowContext(ctx, `SELECT muscle_group FROM exercise_groups WHERE exercise=?`, exercise).Scan(&out.MuscleGroup)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	const cols = `st.id,st.session_id,se.date,st.weight_kg,st.reps,st.rpe,st.rir,st.set_type,st.notes,st.si,st.technique,st.superset`
	scan := func(r interface{ Scan(...any) error }) (training.ExerciseSet, error) {
		var v training.ExerciseSet
		var w, rpe, rir sql.NullFloat64
		var reps sql.NullInt64
		if err := r.Scan(&v.SetID, &v.SessionID, &v.Date, &w, &reps, &rpe, &rir, &v.SetType, &v.Notes, &v.SI, &v.Technique, &v.Superset); err != nil {
			return v, err
		}
		v.WeightKG, v.Reps, v.RPE, v.RIR = ptrF(w), ptrI(reps), ptrF(rpe), ptrF(rir)
		v.SI = training.NormalizeSI(v.SI)
		if v.WeightKG != nil && v.Reps != nil && *v.Reps <= 12 {
			v.Est1RM = training.Epley1RM(*v.WeightKG, *v.Reps)
		}
		return v, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+cols+`
FROM sets st JOIN sessions se ON se.id=st.session_id
WHERE st.exercise=?
ORDER BY se.date DESC, st.position DESC LIMIT ?`, exercise, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return out, err
		}
		out.Sets = append(out.Sets, v)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	// The record is computed over the whole history, not just the page returned.
	// Warm-ups, technique sets and sets without weight or reps cannot be records.
	best, err := scan(s.db.QueryRowContext(ctx, `
SELECT `+cols+`
FROM sets st JOIN sessions se ON se.id=st.session_id
WHERE st.exercise=? AND st.technique='' AND st.set_type<>'warmup'
  AND st.weight_kg>0 AND st.reps IS NOT NULL AND st.reps<=12
ORDER BY st.weight_kg*(1+CAST(st.reps AS REAL)/30) DESC, se.date DESC LIMIT 1`, exercise))
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Best = &best
	return out, nil
}

// WeeklyVolume buckets SI per muscle group by training week. The week start is
// computed as the Monday on or before the session date.
func (s *Store) WeeklyVolume(ctx context.Context, f training.ListFilter) ([]training.WeeklyVolume, error) {
	q := `SELECT date(se.date, '-' || ((CAST(strftime('%w', se.date) AS INTEGER) + 6) % 7) || ' days') AS wk,
COALESCE(g.muscle_group,''),COALESCE(SUM(st.si),0),COUNT(st.id)
FROM sets st
JOIN sessions se ON se.id=st.session_id
LEFT JOIN exercise_groups g ON g.exercise=st.exercise
WHERE st.set_type<>'warmup'`
	args := []any{}
	if f.From != "" {
		q += ` AND se.date>=?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND se.date<=?`
		args = append(args, f.To)
	}
	q += ` GROUP BY wk,2 ORDER BY wk DESC,3 DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.WeeklyVolume
	for rows.Next() {
		var v training.WeeklyVolume
		if err = rows.Scan(&v.WeekStart, &v.MuscleGroup, &v.TotalSI, &v.Sets); err != nil {
			return nil, err
		}
		v.TotalSI = training.NormalizeSI(v.TotalSI)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) SetExerciseGroup(ctx context.Context, g training.ExerciseGroup) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ex, err := resolveExercise(ctx, tx, 0, g.Exercise)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO exercise_groups(exercise,muscle_group,updated_at) VALUES (?,?,CURRENT_TIMESTAMP)
ON CONFLICT(exercise) DO UPDATE SET muscle_group=excluded.muscle_group, updated_at=CURRENT_TIMESTAMP`,
		ex.name, g.MuscleGroup); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ExerciseGroups(ctx context.Context) ([]training.ExerciseGroup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT exercise,muscle_group FROM exercise_groups ORDER BY muscle_group,exercise`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.ExerciseGroup
	for rows.Next() {
		var v training.ExerciseGroup
		if err = rows.Scan(&v.Exercise, &v.MuscleGroup); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// VolumeByGroup left-joins the catalogue so sets whose exercise has no mapping
// still appear, grouped under an empty muscle group.
func (s *Store) VolumeByGroup(ctx context.Context, f training.ListFilter) ([]training.GroupVolume, error) {
	q := `SELECT COALESCE(g.muscle_group,''),COALESCE(SUM(st.si),0),COUNT(st.id)
FROM sets st
JOIN sessions se ON se.id=st.session_id
LEFT JOIN exercise_groups g ON g.exercise=st.exercise
WHERE st.set_type<>'warmup'`
	args := []any{}
	if f.From != "" {
		q += ` AND se.date>=?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND se.date<=?`
		args = append(args, f.To)
	}
	q += ` GROUP BY 1 ORDER BY 2 DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.GroupVolume
	for rows.Next() {
		var v training.GroupVolume
		if err = rows.Scan(&v.MuscleGroup, &v.TotalSI, &v.Sets); err != nil {
			return nil, err
		}
		v.TotalSI = training.NormalizeSI(v.TotalSI)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) total(ctx context.Context, id int64) (float64, error) {
	var total float64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(si),0) FROM sets WHERE session_id=? AND set_type<>'warmup'`, id).Scan(&total)
	return training.NormalizeSI(total), err
}
