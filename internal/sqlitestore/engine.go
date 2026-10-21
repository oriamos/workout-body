package sqlitestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fullfran/training-mcp/internal/training"
)

// dbtx is the part of *sql.DB and *sql.Tx the helpers need, so one helper
// serves both a standalone read and a step inside a transaction.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func nf(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func ni(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func ptrF(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func ptrI(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

// ---- exercise identity ----------------------------------------------------

type resolved struct {
	id      int64
	name    string
	created bool
}

// resolveExercise maps an exact id, canonical name or alias to an exercise,
// creating a new exercise only when the name is unknown. It never guesses: a
// near-miss spelling creates a new exercise (reported to the caller), it is not
// merged into an existing one.
func resolveExercise(ctx context.Context, q dbtx, id int64, name string) (resolved, error) {
	if id > 0 {
		var r resolved
		err := q.QueryRowContext(ctx, `SELECT id,name FROM exercises WHERE id=?`, id).Scan(&r.id, &r.name)
		if errors.Is(err, sql.ErrNoRows) {
			return r, fmt.Errorf("%w: exercise_id %d does not exist", training.ErrNotFound, id)
		}
		return r, err
	}
	name = training.NormalizeExerciseName(name)
	if name == "" {
		return resolved{}, training.ErrValidation
	}
	var r resolved
	err := q.QueryRowContext(ctx, `SELECT id,name FROM exercises WHERE name=?`, name).Scan(&r.id, &r.name)
	if err == nil {
		return r, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	err = q.QueryRowContext(ctx, `SELECT e.id,e.name FROM exercise_aliases a JOIN exercises e ON e.id=a.exercise_id WHERE a.alias=?`, name).Scan(&r.id, &r.name)
	if err == nil {
		return r, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	res, err := q.ExecContext(ctx, `INSERT INTO exercises(name,created_at) VALUES (?,CURRENT_TIMESTAMP)`, name)
	if err != nil {
		return r, err
	}
	r.id, err = res.LastInsertId()
	r.name, r.created = name, true
	return r, err
}

func (s *Store) Exercises(ctx context.Context) ([]training.Exercise, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT e.id,e.name,COALESCE(g.muscle_group,''),(SELECT COUNT(*) FROM sets st WHERE st.exercise_id=e.id)
FROM exercises e LEFT JOIN exercise_groups g ON g.exercise=e.name ORDER BY e.name`)
	if err != nil {
		return nil, err
	}
	var out []training.Exercise
	idx := map[int64]int{}
	for rows.Next() {
		var e training.Exercise
		if err = rows.Scan(&e.ID, &e.Name, &e.MuscleGroup, &e.SetCount); err != nil {
			rows.Close()
			return nil, err
		}
		e.Aliases = []string{}
		idx[e.ID] = len(out)
		out = append(out, e)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	arows, err := s.db.QueryContext(ctx, `SELECT exercise_id,alias FROM exercise_aliases ORDER BY alias`)
	if err != nil {
		return nil, err
	}
	defer arows.Close()
	for arows.Next() {
		var id int64
		var alias string
		if err = arows.Scan(&id, &alias); err != nil {
			return nil, err
		}
		if i, ok := idx[id]; ok {
			out[i].Aliases = append(out[i].Aliases, alias)
		}
	}
	return out, arows.Err()
}

func (s *Store) GetExercise(ctx context.Context, ref string) (training.Exercise, error) {
	var id int64
	if n, err := strconv.ParseInt(ref, 10, 64); err == nil {
		id = n
	} else {
		r, err := lookupExercise(ctx, s.db, training.NormalizeExerciseName(ref))
		if err != nil {
			return training.Exercise{}, err
		}
		id = r.id
	}
	all, err := s.Exercises(ctx)
	if err != nil {
		return training.Exercise{}, err
	}
	for _, e := range all {
		if e.ID == id {
			return e, nil
		}
	}
	return training.Exercise{}, fmt.Errorf("%w: exercise %q does not exist", training.ErrNotFound, ref)
}

// lookupExercise resolves a name or alias without creating anything.
func lookupExercise(ctx context.Context, q dbtx, name string) (resolved, error) {
	var r resolved
	err := q.QueryRowContext(ctx, `SELECT id,name FROM exercises WHERE name=?`, name).Scan(&r.id, &r.name)
	if err == nil {
		return r, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	err = q.QueryRowContext(ctx, `SELECT e.id,e.name FROM exercise_aliases a JOIN exercises e ON e.id=a.exercise_id WHERE a.alias=?`, name).Scan(&r.id, &r.name)
	if errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("%w: exercise %q does not exist", training.ErrNotFound, name)
	}
	return r, err
}

func (s *Store) AddExerciseAlias(ctx context.Context, exerciseID int64, alias string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var name string
	if err = tx.QueryRowContext(ctx, `SELECT name FROM exercises WHERE id=?`, exerciseID).Scan(&name); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: exercise_id %d does not exist", training.ErrNotFound, exerciseID)
	} else if err != nil {
		return err
	}
	if alias == name {
		return nil
	}
	var other string
	if err = tx.QueryRowContext(ctx, `SELECT name FROM exercises WHERE name=?`, alias).Scan(&other); err == nil {
		return fmt.Errorf("%w: %q is already the name of another exercise; aliases never merge exercises", training.ErrConflict, alias)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var owner int64
	err = tx.QueryRowContext(ctx, `SELECT exercise_id FROM exercise_aliases WHERE alias=?`, alias).Scan(&owner)
	switch {
	case err == nil && owner == exerciseID:
		return nil
	case err == nil:
		return fmt.Errorf("%w: alias %q already points at exercise %d", training.ErrConflict, alias, owner)
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO exercise_aliases(alias,exercise_id) VALUES (?,?)`, alias, exerciseID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RemoveExerciseAlias(ctx context.Context, alias string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM exercise_aliases WHERE alias=?`, alias)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: alias %q does not exist", training.ErrNotFound, alias)
	}
	return nil
}

func (s *Store) RenameExercise(ctx context.Context, exerciseID int64, newName string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old string
	if err = tx.QueryRowContext(ctx, `SELECT name FROM exercises WHERE id=?`, exerciseID).Scan(&old); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: exercise_id %d does not exist", training.ErrNotFound, exerciseID)
	} else if err != nil {
		return err
	}
	if old == newName {
		return nil
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM exercises WHERE name=?`, newName).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: an exercise named %q already exists; renaming never merges exercises", training.ErrConflict, newName)
	}
	var owner int64
	switch err = tx.QueryRowContext(ctx, `SELECT exercise_id FROM exercise_aliases WHERE alias=?`, newName).Scan(&owner); {
	case err == nil && owner != exerciseID:
		return fmt.Errorf("%w: %q is an alias of another exercise", training.ErrConflict, newName)
	case err == nil:
		if _, err = tx.ExecContext(ctx, `DELETE FROM exercise_aliases WHERE alias=?`, newName); err != nil {
			return err
		}
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	for _, q := range []string{
		`UPDATE exercises SET name=? WHERE id=?`,
	} {
		if _, err = tx.ExecContext(ctx, q, newName, exerciseID); err != nil {
			return err
		}
	}
	// Everything whose displayed exercise name is about to change is a change
	// to that record, so concurrent editors holding the old revision are told.
	for _, q := range []string{
		`UPDATE sessions SET revision=revision+1 WHERE id IN (SELECT session_id FROM sets WHERE exercise_id=?)`,
		`UPDATE sets SET revision=revision+1 WHERE exercise_id=?`,
		`UPDATE plans SET revision=revision+1 WHERE id IN (SELECT plan_id FROM plan_items WHERE exercise=?)`,
	} {
		arg := any(exerciseID)
		if strings.Contains(q, "plan_items") {
			arg = old
		}
		if _, err = tx.ExecContext(ctx, q, arg); err != nil {
			return err
		}
	}
	for _, table := range []string{"sets", "plan_items", "session_items", "exercise_groups", "exercise_notes"} {
		if _, err = tx.ExecContext(ctx, `UPDATE `+table+` SET exercise=? WHERE exercise=?`, newName, old); err != nil {
			return fmt.Errorf("rename in %s: %w", table, err)
		}
	}
	return tx.Commit()
}

// ---- workouts -------------------------------------------------------------

func bumpWorkout(ctx context.Context, q dbtx, id int64) error {
	_, err := q.ExecContext(ctx, `UPDATE sessions SET revision=revision+1 WHERE id=?`, id)
	return err
}

func bumpPlan(ctx context.Context, q dbtx, id int64) error {
	_, err := q.ExecContext(ctx, `UPDATE plans SET revision=revision+1 WHERE id=?`, id)
	return err
}

// getOrCreate is atomic: the UNIQUE index on sessions(date) makes the INSERT a
// no-op for every caller but the first, whatever the interleaving.
func getOrCreate(ctx context.Context, q dbtx, date string) (training.Session, bool, error) {
	res, err := q.ExecContext(ctx, `INSERT INTO sessions(date, started_at) VALUES (?, CURRENT_TIMESTAMP) ON CONFLICT(date) DO NOTHING`, date)
	if err != nil {
		return training.Session{}, false, err
	}
	n, _ := res.RowsAffected()
	w, err := loadSession(ctx, q, `date=?`, date)
	return w, n > 0, err
}

func (s *Store) GetOrCreateWorkout(ctx context.Context, date string) (training.Session, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.Session{}, false, err
	}
	defer tx.Rollback()
	w, created, err := getOrCreate(ctx, tx, date)
	if err != nil {
		return w, false, err
	}
	return w, created, tx.Commit()
}

func (s *Store) WorkoutByDate(ctx context.Context, date string) (training.Session, error) {
	return loadSession(ctx, s.db, `date=?`, date)
}

func (s *Store) GetSession(ctx context.Context, id int64) (training.Session, error) {
	return loadSession(ctx, s.db, `id=?`, id)
}

const setCols = `id,session_id,position,exercise_id,exercise,weight_kg,reps,rpe,rir,set_type,notes,si,technique,superset,revision`

type scanner interface{ Scan(...any) error }

func scanSet(r scanner) (training.Set, error) {
	var v training.Set
	var w, rpe, rir sql.NullFloat64
	var reps sql.NullInt64
	if err := r.Scan(&v.ID, &v.SessionID, &v.Position, &v.ExerciseID, &v.Exercise, &w, &reps, &rpe, &rir, &v.SetType, &v.Notes, &v.SI, &v.Technique, &v.Superset, &v.Revision); err != nil {
		return v, err
	}
	v.WeightKG, v.Reps, v.RPE, v.RIR = ptrF(w), ptrI(reps), ptrF(rpe), ptrF(rir)
	v.SI = training.NormalizeSI(v.SI)
	return v, nil
}

func loadSession(ctx context.Context, q dbtx, where string, arg any) (training.Session, error) {
	var out training.Session
	var planID sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT id,date,plan_id,plan_name,revision FROM sessions WHERE `+where, arg).
		Scan(&out.ID, &out.Date, &planID, &out.PlanName, &out.Revision)
	out.PlanID = planID.Int64
	if errors.Is(err, sql.ErrNoRows) {
		return out, training.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+setCols+` FROM sets WHERE session_id=? ORDER BY position`, out.ID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanSet(rows)
		if err != nil {
			return out, err
		}
		out.Sets = append(out.Sets, v)
		if v.SetType != training.SetTypeWarmup {
			out.TotalSI += v.SI
		}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	out.TotalSI = training.NormalizeSI(out.TotalSI)
	return out, nil
}

func (s *Store) GetSet(ctx context.Context, id int64) (training.Set, error) {
	v, err := scanSet(s.db.QueryRowContext(ctx, `SELECT `+setCols+` FROM sets WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, fmt.Errorf("%w: set %d does not exist", training.ErrNotFound, id)
	}
	return v, err
}

// insertSet appends one set to a workout inside a transaction.
func insertSet(ctx context.Context, tx dbtx, sessionID int64, in training.AddSetInput) (training.Set, resolved, error) {
	ex, err := resolveExercise(ctx, tx, in.ExerciseID, in.Exercise)
	if err != nil {
		return training.Set{}, ex, err
	}
	var pos int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),0)+1 FROM sets WHERE session_id=?`, sessionID).Scan(&pos); err != nil {
		return training.Set{}, ex, err
	}
	if in.Superset == "" {
		err = tx.QueryRowContext(ctx, `SELECT superset FROM session_items WHERE session_id=? AND exercise=?`, sessionID, ex.name).Scan(&in.Superset)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return training.Set{}, ex, err
		}
	}
	// SI needs an RPE. Without one it is 0, never an invented value.
	var si float64
	if in.RPE != nil {
		si = training.CalculateSI(*in.RPE)
	}
	setType := in.SetType
	if setType == "" {
		setType = training.SetTypeWorking
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO sets(session_id,position,exercise,exercise_id,weight_kg,reps,rpe,rir,set_type,notes,si,technique,superset,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		sessionID, pos, ex.name, ex.id, nf(in.WeightKG), ni(in.Reps), nf(in.RPE), nf(in.RIR), setType, in.Notes, si, in.Technique, in.Superset)
	if err != nil {
		return training.Set{}, ex, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return training.Set{}, ex, err
	}
	set, err := scanSet(tx.QueryRowContext(ctx, `SELECT `+setCols+` FROM sets WHERE id=?`, id))
	return set, ex, err
}

func (s *Store) AddSet(ctx context.Context, in training.AddSetInput) (training.Set, float64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.Set{}, 0, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id=?`, in.SessionID).Scan(&exists); err != nil {
		return training.Set{}, 0, err
	}
	if exists == 0 {
		return training.Set{}, 0, training.ErrNotFound
	}
	set, _, err := insertSet(ctx, tx, in.SessionID, in)
	if err != nil {
		return training.Set{}, 0, err
	}
	if err = bumpWorkout(ctx, tx, in.SessionID); err != nil {
		return training.Set{}, 0, err
	}
	total, err := sessionTotal(ctx, tx, in.SessionID)
	if err != nil {
		return training.Set{}, 0, err
	}
	return set, total, tx.Commit()
}

func sessionTotal(ctx context.Context, q dbtx, id int64) (float64, error) {
	var total float64
	err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(si),0) FROM sets WHERE session_id=? AND set_type<>'warmup'`, id).Scan(&total)
	return training.NormalizeSI(total), err
}

func (s *Store) UpdateSet(ctx context.Context, id int64, p training.SetPatch) (training.Set, float64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.Set{}, 0, err
	}
	defer tx.Rollback()
	set, err := scanSet(tx.QueryRowContext(ctx, `SELECT `+setCols+` FROM sets WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return training.Set{}, 0, fmt.Errorf("%w: set %d does not exist", training.ErrNotFound, id)
	}
	if err != nil {
		return training.Set{}, 0, err
	}
	if p.ExpectedRevision != nil && *p.ExpectedRevision != set.Revision {
		return training.Set{}, 0, fmt.Errorf("%w: set %d is at revision %d, not %d; re-read it and retry", training.ErrStale, id, set.Revision, *p.ExpectedRevision)
	}
	if p.Exercise != nil || p.ExerciseID != nil {
		var eid int64
		var name string
		if p.ExerciseID != nil {
			eid = *p.ExerciseID
		}
		if p.Exercise != nil {
			name = *p.Exercise
		}
		ex, err := resolveExercise(ctx, tx, eid, name)
		if err != nil {
			return training.Set{}, 0, err
		}
		set.ExerciseID, set.Exercise = ex.id, ex.name
	}
	switch {
	case p.ClearWeight:
		set.WeightKG = nil
	case p.WeightKG != nil:
		set.WeightKG = p.WeightKG
	}
	switch {
	case p.ClearReps:
		set.Reps = nil
	case p.Reps != nil:
		set.Reps = p.Reps
	}
	switch {
	case p.ClearRPE:
		set.RPE = nil
	case p.RPE != nil:
		set.RPE = p.RPE
	}
	switch {
	case p.ClearRIR:
		set.RIR = nil
	case p.RIR != nil:
		set.RIR = p.RIR
	}
	if p.SetType != nil {
		set.SetType = *p.SetType
	}
	if p.Notes != nil {
		set.Notes = *p.Notes
	}
	if p.Technique != nil {
		set.Technique = *p.Technique
	}
	set.SI = 0
	if set.RPE != nil {
		set.SI = training.CalculateSI(*set.RPE)
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE sets SET exercise=?,exercise_id=?,weight_kg=?,reps=?,rpe=?,rir=?,set_type=?,notes=?,si=?,technique=?,
 revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		set.Exercise, set.ExerciseID, nf(set.WeightKG), ni(set.Reps), nf(set.RPE), nf(set.RIR), set.SetType, set.Notes, set.SI, set.Technique, id); err != nil {
		return training.Set{}, 0, err
	}
	if err = bumpWorkout(ctx, tx, set.SessionID); err != nil {
		return training.Set{}, 0, err
	}
	out, err := scanSet(tx.QueryRowContext(ctx, `SELECT `+setCols+` FROM sets WHERE id=?`, id))
	if err != nil {
		return training.Set{}, 0, err
	}
	total, err := sessionTotal(ctx, tx, set.SessionID)
	if err != nil {
		return training.Set{}, 0, err
	}
	return out, total, tx.Commit()
}

func (s *Store) DeleteSet(ctx context.Context, id int64, expected *int) (int64, float64, int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback()
	var sessionID int64
	var rev int
	if err = tx.QueryRowContext(ctx, `SELECT session_id,revision FROM sets WHERE id=?`, id).Scan(&sessionID, &rev); errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0, fmt.Errorf("%w: set %d does not exist", training.ErrNotFound, id)
	}
	if err != nil {
		return 0, 0, 0, err
	}
	if expected != nil && *expected != rev {
		return 0, 0, 0, fmt.Errorf("%w: set %d is at revision %d, not %d; re-read it and retry", training.ErrStale, id, rev, *expected)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sets WHERE id=?`, id); err != nil {
		return 0, 0, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM sets WHERE session_id=? ORDER BY position,id`, sessionID)
	if err != nil {
		return 0, 0, 0, err
	}
	var ids []int64
	for rows.Next() {
		var v int64
		if err = rows.Scan(&v); err != nil {
			rows.Close()
			return 0, 0, 0, err
		}
		ids = append(ids, v)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, 0, 0, err
	}
	for _, v := range ids {
		if _, err = tx.ExecContext(ctx, `UPDATE sets SET position=-position-1000000 WHERE id=?`, v); err != nil {
			return 0, 0, 0, err
		}
	}
	for i, v := range ids {
		if _, err = tx.ExecContext(ctx, `UPDATE sets SET position=? WHERE id=?`, i+1, v); err != nil {
			return 0, 0, 0, err
		}
	}
	if s.failpoint != nil {
		if err = s.failpoint("delete-resequence"); err != nil {
			return 0, 0, 0, err
		}
	}
	if err = bumpWorkout(ctx, tx, sessionID); err != nil {
		return 0, 0, 0, err
	}
	total, err := sessionTotal(ctx, tx, sessionID)
	if err != nil {
		return 0, 0, 0, err
	}
	return sessionID, total, len(ids), tx.Commit()
}

// ---- batch logging -------------------------------------------------------

const opLogSets = "log_sets"

func batchHash(in training.BatchInput) (string, error) {
	in.IdempotencyKey = ""
	in.ExpectedRevision = nil
	b, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Store) LogBatch(ctx context.Context, in training.BatchInput) (training.BatchResult, error) {
	hash, err := batchHash(in)
	if err != nil {
		return training.BatchResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.BatchResult{}, err
	}
	defer tx.Rollback()

	if in.IdempotencyKey != "" {
		var op, h, body string
		err = tx.QueryRowContext(ctx, `SELECT operation,request_hash,response_json FROM idempotency_keys WHERE key=?`, in.IdempotencyKey).Scan(&op, &h, &body)
		switch {
		case err == nil:
			if op != opLogSets || h != hash {
				return training.BatchResult{}, fmt.Errorf("%w: idempotency_key %q was already used for a different request; use a new key for a different write", training.ErrConflict, in.IdempotencyKey)
			}
			var prev training.BatchResult
			if err = json.Unmarshal([]byte(body), &prev); err != nil {
				return training.BatchResult{}, err
			}
			prev.Replayed = true
			if w, err := loadSession(ctx, tx, `id=?`, prev.Workout.ID); err == nil {
				prev.Workout = w
			}
			return prev, nil
		case !errors.Is(err, sql.ErrNoRows):
			return training.BatchResult{}, err
		}
	}

	w, created, err := getOrCreate(ctx, tx, in.Date)
	if err != nil {
		return training.BatchResult{}, err
	}
	if in.ExpectedRevision != nil && !created && *in.ExpectedRevision != w.Revision {
		return training.BatchResult{}, fmt.Errorf("%w: workout %s is at revision %d, not %d; re-read it and retry", training.ErrStale, in.Date, w.Revision, *in.ExpectedRevision)
	}
	if in.PlanID > 0 && w.PlanID != in.PlanID {
		if w.PlanID != 0 {
			return training.BatchResult{}, fmt.Errorf("%w: workout %s already follows plan %d (%s)", training.ErrConflict, in.Date, w.PlanID, w.PlanName)
		}
		if err = attachPlan(ctx, tx, w.ID, in.PlanID); err != nil {
			return training.BatchResult{}, err
		}
	}

	res := training.BatchResult{Created: created, Sets: []training.Set{}, Prescriptions: []training.PlanItem{}}
	for _, it := range in.Prescriptions {
		ex, err := resolveExercise(ctx, tx, 0, it.Exercise)
		if err != nil {
			return training.BatchResult{}, err
		}
		if ex.created {
			res.NewExercises = append(res.NewExercises, training.NewExercise{ID: ex.id, Name: ex.name})
		}
		var pos int
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),0)+1 FROM session_items WHERE session_id=?`, w.ID).Scan(&pos); err != nil {
			return training.BatchResult{}, err
		}
		// Merge, don't replace: "planning 3 sets" must not erase a stored rep
		// range or target weight it did not mention.
		if _, err = tx.ExecContext(ctx, `
INSERT INTO session_items (session_id,position,exercise,target_sets,rep_min,rep_max,target_rpe,target_weight_kg,target_rir,superset,notes)
VALUES (?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(session_id,exercise) DO UPDATE SET
 target_sets=excluded.target_sets,
 rep_min=CASE WHEN excluded.rep_min>0 THEN excluded.rep_min ELSE rep_min END,
 rep_max=CASE WHEN excluded.rep_max>0 THEN excluded.rep_max ELSE rep_max END,
 target_rpe=CASE WHEN excluded.target_rpe>0 THEN excluded.target_rpe ELSE target_rpe END,
 target_weight_kg=COALESCE(excluded.target_weight_kg,target_weight_kg),
 target_rir=COALESCE(excluded.target_rir,target_rir),
 superset=CASE WHEN excluded.superset<>'' THEN excluded.superset ELSE superset END,
 notes=CASE WHEN excluded.notes<>'' THEN excluded.notes ELSE notes END`,
			w.ID, pos, ex.name, it.TargetSets, it.RepMin, it.RepMax, it.TargetRPE, nf(it.TargetWeightKG), nf(it.TargetRIR), it.Superset, it.Notes); err != nil {
			return training.BatchResult{}, err
		}
		stored, err := loadSessionItem(ctx, tx, w.ID, ex.name)
		if err != nil {
			return training.BatchResult{}, err
		}
		if err = invertedRange(stored); err != nil {
			return training.BatchResult{}, err
		}
		res.Prescriptions = append(res.Prescriptions, stored)
	}
	for _, in := range in.Sets {
		set, ex, err := insertSet(ctx, tx, w.ID, in)
		if err != nil {
			return training.BatchResult{}, err
		}
		if ex.created {
			res.NewExercises = append(res.NewExercises, training.NewExercise{ID: ex.id, Name: ex.name})
		}
		res.Sets = append(res.Sets, set)
	}
	if err = bumpWorkout(ctx, tx, w.ID); err != nil {
		return training.BatchResult{}, err
	}
	if res.Workout, err = loadSession(ctx, tx, `id=?`, w.ID); err != nil {
		return training.BatchResult{}, err
	}
	if in.IdempotencyKey != "" {
		body, err := json.Marshal(res)
		if err != nil {
			return training.BatchResult{}, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE created_at < datetime('now','-90 days')`); err != nil {
			return training.BatchResult{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys(key,operation,request_hash,response_json,created_at) VALUES (?,?,?,?,CURRENT_TIMESTAMP)`,
			in.IdempotencyKey, opLogSets, hash, string(body)); err != nil {
			return training.BatchResult{}, err
		}
	}
	return res, tx.Commit()
}

func loadSessionItem(ctx context.Context, q dbtx, sessionID int64, exercise string) (training.PlanItem, error) {
	var it training.PlanItem
	var tw, tr sql.NullFloat64
	err := q.QueryRowContext(ctx, `SELECT position,exercise,target_sets,rep_min,rep_max,target_rpe,target_weight_kg,target_rir,superset,notes FROM session_items WHERE session_id=? AND exercise=?`,
		sessionID, exercise).Scan(&it.Position, &it.Exercise, &it.TargetSets, &it.RepMin, &it.RepMax, &it.TargetRPE, &tw, &tr, &it.Superset, &it.Notes)
	it.TargetWeightKG, it.TargetRIR = ptrF(tw), ptrF(tr)
	return it, err
}

func attachPlan(ctx context.Context, tx dbtx, sessionID, planID int64) error {
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM plans WHERE id=?`, planID).Scan(&name); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: plan %d does not exist", training.ErrNotFound, planID)
	} else if err != nil {
		return err
	}
	var current sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM sessions WHERE id=?`, sessionID).Scan(&current); err != nil {
		return err
	}
	if current.Valid && current.Int64 != planID {
		return fmt.Errorf("%w: workout already follows plan %d", training.ErrConflict, current.Int64)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET plan_id=?, plan_name=? WHERE id=?`, planID, name, sessionID); err != nil {
		return err
	}
	// Copy, never link: adjusting the workout later never edits the template,
	// and editing the template never rewrites the workout. Exercises the
	// workout already prescribes keep their own prescription.
	if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO session_items (session_id,position,exercise,target_sets,rep_min,rep_max,target_rpe,target_weight_kg,target_rir,superset,notes)
SELECT ?, position + (SELECT COALESCE(MAX(position),0) FROM session_items WHERE session_id=?),
       exercise,target_sets,rep_min,rep_max,target_rpe,target_weight_kg,target_rir,superset,notes
FROM plan_items WHERE plan_id=? ORDER BY position`, sessionID, sessionID, planID); err != nil {
		return err
	}
	return bumpWorkout(ctx, tx, sessionID)
}

func (s *Store) AttachPlan(ctx context.Context, sessionID, planID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = attachPlan(ctx, tx, sessionID, planID); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- plans ----------------------------------------------------------------

func loadPlan(ctx context.Context, q dbtx, id int64) (training.Plan, error) {
	var p training.Plan
	err := q.QueryRowContext(ctx, `SELECT id,name,notes,revision FROM plans WHERE id=?`, id).Scan(&p.ID, &p.Name, &p.Notes, &p.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return p, training.ErrNotFound
	}
	if err != nil {
		return p, err
	}
	rows, err := q.QueryContext(ctx, `SELECT position,exercise,target_sets,rep_min,rep_max,target_rpe,target_weight_kg,target_rir,superset,notes FROM plan_items WHERE plan_id=? ORDER BY position`, id)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var it training.PlanItem
		var tw, tr sql.NullFloat64
		if err = rows.Scan(&it.Position, &it.Exercise, &it.TargetSets, &it.RepMin, &it.RepMax, &it.TargetRPE, &tw, &tr, &it.Superset, &it.Notes); err != nil {
			return p, err
		}
		it.TargetWeightKG, it.TargetRIR = ptrF(tw), ptrF(tr)
		p.Items = append(p.Items, it)
		p.TotalSets += it.TargetSets
	}
	return p, rows.Err()
}

// insertPlan writes a plan and its items, resolving exercise names (aliases
// included) and reporting the exercises it created.
func insertPlan(ctx context.Context, tx dbtx, p training.Plan) (int64, []training.NewExercise, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO plans(name,notes,created_at) VALUES (?,?,CURRENT_TIMESTAMP)`, p.Name, p.Notes)
	if err != nil {
		return 0, nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, nil, err
	}
	created, err := insertPlanItems(ctx, tx, id, p.Items)
	return id, created, err
}

func insertPlanItems(ctx context.Context, tx dbtx, planID int64, items []training.PlanItem) ([]training.NewExercise, error) {
	var created []training.NewExercise
	// plan_items is only unique on (plan_id, position), so two names that
	// resolve to one exercise (a name and its alias) must be caught here.
	seen := map[int64]int{}
	for i, it := range items {
		ex, err := resolveExercise(ctx, tx, 0, it.Exercise)
		if err != nil {
			return nil, err
		}
		if first, dup := seen[ex.id]; dup {
			return nil, &training.ValidationError{Issues: []training.Issue{{
				Field:   fmt.Sprintf("items[%d].exercise", i),
				Message: fmt.Sprintf("%q is the same exercise as items[%d] (%q) through an alias; list each exercise once", it.Exercise, first, ex.name),
			}}}
		}
		seen[ex.id] = i
		if ex.created {
			created = append(created, training.NewExercise{ID: ex.id, Name: ex.name})
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO plan_items(plan_id,position,exercise,target_sets,rep_min,rep_max,target_rpe,target_weight_kg,target_rir,superset,notes) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			planID, i+1, ex.name, it.TargetSets, it.RepMin, it.RepMax, it.TargetRPE, nf(it.TargetWeightKG), nf(it.TargetRIR), it.Superset, it.Notes); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return nil, fmt.Errorf("%w: items resolve to the same exercise twice (%q); check aliases", training.ErrValidation, ex.name)
			}
			return nil, err
		}
	}
	return created, nil
}

func (s *Store) ImportPlan(ctx context.Context, p training.Plan, onConflict string, dryRun bool) (training.PlanImportResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.PlanImportResult{}, err
	}
	defer tx.Rollback()
	var existing int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM plans WHERE lower(name)=lower(?) ORDER BY id LIMIT 1`, p.Name).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return training.PlanImportResult{}, err
	}
	out := training.PlanImportResult{DryRun: dryRun}
	var created []training.NewExercise
	switch {
	case errors.Is(err, sql.ErrNoRows):
		var id int64
		if id, created, err = insertPlan(ctx, tx, p); err != nil {
			return out, err
		}
		existing, out.Created = id, true
	case onConflict != training.OnConflictReplace:
		return out, fmt.Errorf("%w: a plan named %q already exists (id %d); import with on_conflict=replace to replace its exercises, or rename the plan in the file", training.ErrConflict, p.Name, existing)
	default:
		if _, err = tx.ExecContext(ctx, `DELETE FROM plan_items WHERE plan_id=?`, existing); err != nil {
			return out, err
		}
		if created, err = insertPlanItems(ctx, tx, existing, p.Items); err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE plans SET name=?,notes=?,revision=revision+1 WHERE id=?`, p.Name, p.Notes, existing); err != nil {
			return out, err
		}
		out.Replaced = true
	}
	out.NewExercises = created
	if out.Plan, err = loadPlan(ctx, tx, existing); err != nil {
		return out, err
	}
	if dryRun {
		if out.Created {
			out.Plan.ID = 0
		}
		return out, nil // deferred Rollback discards everything
	}
	return out, tx.Commit()
}

func (s *Store) ReplacePlan(ctx context.Context, id int64, p training.Plan, expected *int) (training.Plan, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.Plan{}, err
	}
	defer tx.Rollback()
	var rev int
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM plans WHERE id=?`, id).Scan(&rev); errors.Is(err, sql.ErrNoRows) {
		return training.Plan{}, training.ErrNotFound
	} else if err != nil {
		return training.Plan{}, err
	}
	if expected != nil && *expected != rev {
		return training.Plan{}, fmt.Errorf("%w: plan %d is at revision %d, not %d; re-read it and retry", training.ErrStale, id, rev, *expected)
	}
	if other, taken, err := planNameTaken(ctx, tx, p.Name, id); err != nil {
		return training.Plan{}, err
	} else if taken {
		return training.Plan{}, errPlanNameTaken(p.Name, other)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM plan_items WHERE plan_id=?`, id); err != nil {
		return training.Plan{}, err
	}
	if _, err = insertPlanItems(ctx, tx, id, p.Items); err != nil {
		return training.Plan{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plans SET name=?,notes=?,revision=revision+1 WHERE id=?`, p.Name, p.Notes, id); err != nil {
		return training.Plan{}, err
	}
	out, err := loadPlan(ctx, tx, id)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) DeletePlanExpected(ctx context.Context, id int64, expected *int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rev int
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM plans WHERE id=?`, id).Scan(&rev); errors.Is(err, sql.ErrNoRows) {
		return training.ErrNotFound
	} else if err != nil {
		return err
	}
	if expected != nil && *expected != rev {
		return fmt.Errorf("%w: plan %d is at revision %d, not %d; re-read it and retry", training.ErrStale, id, rev, *expected)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM plans WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- analytics ------------------------------------------------------------

func (s *Store) SetRecords(ctx context.Context, from, to string) ([]training.SetRecord, error) {
	q := `SELECT st.id,st.session_id,se.date,st.exercise_id,st.exercise,COALESCE(g.muscle_group,''),st.weight_kg,st.reps,st.rpe,st.rir,st.set_type,st.technique
FROM sets st JOIN sessions se ON se.id=st.session_id
LEFT JOIN exercise_groups g ON g.exercise=st.exercise WHERE 1=1`
	var args []any
	if from != "" {
		q += ` AND se.date>=?`
		args = append(args, from)
	}
	if to != "" {
		q += ` AND se.date<=?`
		args = append(args, to)
	}
	q += ` ORDER BY se.date, st.position`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.SetRecord
	for rows.Next() {
		var r training.SetRecord
		var w, rpe, rir sql.NullFloat64
		var reps sql.NullInt64
		if err = rows.Scan(&r.SetID, &r.SessionID, &r.Date, &r.ExerciseID, &r.Exercise, &r.Muscle, &w, &reps, &rpe, &rir, &r.SetType, &r.Technique); err != nil {
			return nil, err
		}
		r.WeightKG, r.Reps, r.RPE, r.RIR = ptrF(w), ptrI(reps), ptrF(rpe), ptrF(rir)
		out = append(out, r)
	}
	return out, rows.Err()
}

// invertedRange rejects a rep range whose merged result is backwards, e.g. a
// stored 8-12 patched with rep_max=6. Each value passes validation alone; only
// the combination is wrong, so it is checked after merging.
func invertedRange(it training.PlanItem) error {
	if it.RepMax > 0 && it.RepMin > it.RepMax {
		return &training.ValidationError{Issues: []training.Issue{{
			Field:   "rep_min",
			Message: fmt.Sprintf("the stored range would become %d-%d (rep_min above rep_max); send rep_min and rep_max together", it.RepMin, it.RepMax),
		}}}
	}
	return nil
}

// planNameTaken reports whether another plan already uses name
// (case-insensitively). exceptID excludes the plan being edited.
func planNameTaken(ctx context.Context, q dbtx, name string, exceptID int64) (int64, bool, error) {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM plans WHERE lower(name)=lower(?) AND id<>? ORDER BY id LIMIT 1`, name, exceptID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

func errPlanNameTaken(name string, id int64) error {
	return fmt.Errorf("%w: a plan named %q already exists (id %d); choose another name", training.ErrConflict, name, id)
}
