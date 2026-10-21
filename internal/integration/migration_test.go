package integration

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

// buildV9 creates a database at schema version 9 by applying the legacy
// migration files from disk, exactly as an old binary would have left it.
func buildV9(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("../sqlitestore/migrations/00*.sql")
	if err != nil || len(files) != 9 {
		t.Fatalf("want migrations 001..009, got %v (%v)", files, err)
	}
	sort.Strings(files)
	for _, file := range files {
		if _, err := db.Exec(string(readFile(t, file))); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		v, _ := strconv.Atoi(strings.SplitN(filepath.Base(file), "_", 2)[0])
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, v); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func exec(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func TestMigrationFromLegacyV9(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db := buildV9(t, path)
	const ts = `'2026-01-05 10:00:00'`
	exec(t, db,
		`INSERT INTO plans(id,name,notes,created_at) VALUES (1,'Push A','legacy plan',`+ts+`)`,
		`INSERT INTO plan_items(plan_id,position,exercise,target_sets,rep_min,rep_max,target_rpe,superset,notes) VALUES
		 (1,1,'bench press',3,6,8,8,'',''),(1,2,'overhead press',3,8,10,0,'A','strict')`,
		// Two sessions on the same date (the bug v2 fixes) plus one other date.
		`INSERT INTO sessions(id,date,started_at,plan_id,plan_name) VALUES (1,'2026-01-05',`+ts+`,1,'Push A')`,
		`INSERT INTO sessions(id,date,started_at,plan_id,plan_name) VALUES (2,'2026-01-05',`+ts+`,NULL,'')`,
		`INSERT INTO sessions(id,date,started_at,plan_id,plan_name) VALUES (3,'2026-01-07',`+ts+`,NULL,'')`,
		`INSERT INTO session_items(session_id,position,exercise,target_sets,rep_min,rep_max,target_rpe,superset,notes) VALUES
		 (1,1,'bench press',3,6,8,8,'',''),(1,2,'overhead press',3,8,10,0,'A','strict'),
		 (2,1,'bench press',5,1,3,9,'',''),(2,2,'lateral raise',3,12,15,0,'','')`,
		`INSERT INTO sets(id,session_id,position,exercise,weight_kg,reps,rpe,si,technique,superset,created_at,updated_at) VALUES
		 (10,1,1,'bench press',80,8,8,1.0,'','',`+ts+`,`+ts+`),
		 (11,1,2,'bench press',80,7,9,1.2,'','',`+ts+`,`+ts+`),
		 (12,2,1,'lateral raise',10,15,8,1.0,'','',`+ts+`,`+ts+`),
		 (13,2,2,'bench press',75,10,7,0.8,'drop set','',`+ts+`,`+ts+`),
		 (14,3,1,'squat',120,5,8,1.0,'','',`+ts+`,`+ts+`)`,
		`INSERT INTO session_feedback(session_id,muscle_group,fatigue,pump,recovery,created_at) VALUES
		 (1,'pecho',2,2,1,`+ts+`),(2,'pecho',0,0,0,`+ts+`),(2,'hombro lateral',1,3,2,`+ts+`)`,
		`INSERT INTO exercise_groups(exercise,muscle_group,updated_at) VALUES
		 ('bench press','pecho',`+ts+`),('leg curl','isquios',`+ts+`)`,
		`INSERT INTO exercise_notes(exercise,note,updated_at) VALUES ('squat','pins at 7',`+ts+`)`,
		// A second duplicated date where only the NEWER session followed a plan.
		`INSERT INTO sessions(id,date,started_at,plan_id,plan_name) VALUES (4,'2026-01-09',`+ts+`,NULL,'')`,
		`INSERT INTO sessions(id,date,started_at,plan_id,plan_name) VALUES (5,'2026-01-09',`+ts+`,1,'Push A')`,
		`INSERT INTO session_items(session_id,position,exercise,target_sets,rep_min,rep_max,target_rpe,superset,notes) VALUES
		 (5,1,'bench press',3,6,8,8,'','')`,
		`INSERT INTO sets(id,session_id,position,exercise,weight_kg,reps,rpe,si,technique,superset,created_at,updated_at) VALUES
		 (15,4,1,'squat',100,5,7,0.8,'','',`+ts+`,`+ts+`),
		 (16,5,1,'bench press',82.5,6,8,1.0,'','',`+ts+`,`+ts+`)`,
	)
	db.Close()

	e := openEnv(t, path) // applies 010
	if got := e.count(`SELECT MAX(version) FROM schema_migrations`); got < 10 {
		t.Fatalf("schema version %d", got)
	}
	if got := e.count(`SELECT COUNT(*) FROM sessions WHERE date='2026-01-05'`); got != 1 {
		t.Fatalf("duplicates not merged: %d sessions", got)
	}
	w := e.workout("2026-01-05")
	if w.ID != 1 || w.PlanID != 1 || w.PlanName != "Push A" || w.Revision != 1 {
		t.Fatalf("survivor must be the oldest session: %+v", w)
	}
	wantIDs := []int64{10, 11, 12, 13}
	if len(w.Sets) != 4 {
		t.Fatalf("want 4 merged sets, got %d", len(w.Sets))
	}
	for i, st := range w.Sets {
		if st.ID != wantIDs[i] || st.Position != i+1 || st.ExerciseID == 0 || st.Revision != 1 || st.SetType != "working" {
			t.Fatalf("set %d: %+v", i, st)
		}
	}
	if s13 := w.Sets[3]; *s13.WeightKG != 75 || *s13.Reps != 10 || *s13.RPE != 7 || s13.Technique != "drop set" || s13.SetNumber != 3 {
		t.Fatalf("set values not preserved: %+v", s13)
	}
	if got := e.count(`SELECT COUNT(*) FROM sets`); got != 7 {
		t.Fatalf("sets lost: %d", got)
	}
	if other := e.workout("2026-01-07"); other.ID != 3 || len(other.Sets) != 1 || other.Sets[0].Position != 1 {
		t.Fatalf("untouched session changed: %+v", other)
	}

	// Prescriptions: the survivor's own win, the duplicate's extras move over.
	bp, _ := progressFor(w, "bench press")
	if bp.TargetSets != 3 || bp.RepMin != 6 || bp.RepMax != 8 || bp.TargetRPE != 8 || bp.DoneSets != 3 || bp.MuscleGroup != "pecho" {
		t.Fatalf("bench prescription: %+v", bp)
	}
	if ohp, _ := progressFor(w, "overhead press"); ohp.TargetSets != 3 || ohp.Superset != "A" || ohp.Notes != "strict" || ohp.DoneSets != 0 {
		t.Fatalf("ohp prescription: %+v", ohp)
	}
	if lr, ok := progressFor(w, "lateral raise"); !ok || lr.TargetSets != 3 || lr.RepMin != 12 || lr.DoneSets != 1 {
		t.Fatalf("moved prescription: %+v", lr)
	}
	if got := e.count(`SELECT COUNT(*) FROM session_items WHERE session_id=1`); got != 3 {
		t.Fatalf("session items: %d", got)
	}
	// The plan snapshot of a merged duplicate is kept when the survivor had none.
	w9 := e.workout("2026-01-09")
	if w9.ID != 4 || w9.PlanID != 1 || w9.PlanName != "Push A" || len(w9.Sets) != 2 || w9.Sets[0].ID != 15 || w9.Sets[1].ID != 16 || w9.Sets[1].Position != 2 {
		t.Fatalf("second merge: %+v", w9)
	}
	if bp9, ok := progressFor(w9, "bench press"); !ok || bp9.TargetSets != 3 || bp9.DoneSets != 1 {
		t.Fatalf("second merge prescription: %+v", w9.Progress)
	}
	if got := e.count(`SELECT COUNT(*) FROM sessions`); got != 3 {
		t.Fatalf("want 3 sessions after merging, got %d", got)
	}

	fb, err := e.svc.SessionFeedback(ctx(), 1)
	if err != nil {
		t.Fatal(err)
	}
	wantFB := []training.Feedback{{MuscleGroup: "hombro lateral", Fatigue: 1, Pump: 3, Recovery: 2}, {MuscleGroup: "pecho", Fatigue: 2, Pump: 2, Recovery: 1}}
	if !reflect.DeepEqual(fb, wantFB) {
		t.Fatalf("feedback: %+v", fb)
	}

	if got := e.exerciseNames(); !reflect.DeepEqual(got, []string{"bench press", "lateral raise", "leg curl", "overhead press", "squat"}) {
		t.Fatalf("exercises: %v", got)
	}
	if got := e.count(`SELECT COUNT(*) FROM sets st JOIN exercises x ON x.id=st.exercise_id AND x.name=st.exercise`); got != 7 {
		t.Fatalf("exercise ids inconsistent with names: %d", got)
	}
	p, err := e.svc.GetPlan(ctx(), 1)
	if err != nil || p.Revision != 1 || len(p.Items) != 2 || p.Items[1].Notes != "strict" || p.Items[0].TargetWeightKG != nil {
		t.Fatalf("plan: %v %+v", err, p)
	}

	// The rule holds from now on: the DB refuses a second row for a date and
	// new writes join the merged workout using the existing exercise ids.
	if _, err := e.sqlDB().Exec(`INSERT INTO sessions(date,started_at) VALUES ('2026-01-05',CURRENT_TIMESTAMP)`); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("UNIQUE(date) not enforced: %v", err)
	}
	res := e.log(batch("2026-01-05", set("Bench Press", 82.5, 5)))
	if res.Created || res.Workout.ID != 1 || res.Sets[0].Position != 5 || res.Sets[0].ExerciseID != w.Sets[0].ExerciseID || len(res.NewExercises) != 0 {
		t.Fatalf("post-migration write: %+v", res)
	}
	// Legacy sets keep counting in analytics.
	st, err := e.svc.ExerciseStats(ctx(), "bench press", "", "")
	if err != nil || st.WorkingSets != 5 || st.VolumeKG != 80*8+80*7+75*10+82.5*5+82.5*6 {
		t.Fatalf("stats over migrated data: %v %+v", err, st)
	}
}

func TestSnapshotBackup(t *testing.T) {
	e := newEnv(t)
	e.log(training.BatchInput{Date: "2026-03-09",
		Sets:          []training.AddSetInput{set("bench press", 60, 8), {Exercise: "bench press", Reps: n(5)}},
		Prescriptions: []training.PlanItem{rx("squat", 3, 5, 5, f(100))}})
	if _, err := e.svc.ImportPlan(ctx(), readFile(t, pushA), "", false); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.db")
	if err := e.svc.Snapshot(ctx(), backup); err != nil {
		t.Fatal(err)
	}
	wantErr(t, e.svc.Snapshot(ctx(), "  "), training.ErrValidation)

	// Writes after the snapshot are not in it.
	e.log(batch("2026-03-10", set("deadlift", 150, 3)))

	restored := openEnv(t, backup)
	w := restored.workout("2026-03-09")
	if len(w.Sets) != 2 || w.Sets[1].WeightKG != nil || *w.Sets[0].WeightKG != 60 {
		t.Fatalf("restored sets: %+v", w.Sets)
	}
	if sq, ok := progressFor(w, "squat"); !ok || !eqF(sq.TargetWeightKG, f(100)) {
		t.Fatalf("restored prescription: %+v", w.Progress)
	}
	plans, err := restored.svc.ListPlans(ctx())
	if err != nil || len(plans) != 1 || plans[0].Name != "Push A" {
		t.Fatalf("restored plans: %v %+v", err, plans)
	}
	if _, err := restored.svc.GetWorkout(ctx(), "2026-03-10"); err == nil {
		t.Fatal("snapshot contains a write made after it")
	}
	// The restored copy is a fully working database.
	restored.log(batch("2026-03-09", set("bench press", 60, 6)))
	if len(restored.workout("2026-03-09").Sets) != 3 || len(e.workout("2026-03-09").Sets) != 2 {
		t.Fatal("snapshot is not independent of the source")
	}
}

// Upstream never capped weight or reps, so rows it accepted must still migrate.
func TestMigrationAcceptsLegacyValuesBeyondNewServiceLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db := buildV9(t, path)
	const ts = `'2026-01-05 10:00:00'`
	exec(t, db,
		`INSERT INTO sessions(id,date,started_at) VALUES (1,'2026-01-05',`+ts+`)`,
		`INSERT INTO sets(id,session_id,position,exercise,weight_kg,reps,rpe,si,created_at,updated_at) VALUES
		 (1,1,1,'leg press',2200,12,8,1.0,`+ts+`,`+ts+`),
		 (2,1,2,'plank',40,1500,6,0.6,`+ts+`,`+ts+`)`)
	db.Close()

	e := openEnv(t, path)
	w, err := e.svc.GetWorkout(ctx(), "2026-01-05")
	if err != nil {
		t.Fatalf("the server must start on legacy data: %v", err)
	}
	if len(w.Sets) != 2 || *w.Sets[0].WeightKG != 2200 || *w.Sets[1].Reps != 1500 {
		t.Fatalf("legacy values must be preserved exactly: %+v", w.Sets)
	}
	// New input is still held to the service limits.
	_, err = e.svc.LogSets(ctx(), batch("2026-01-05", set("leg press", 2200, 12)))
	if !errors.Is(err, training.ErrValidation) {
		t.Fatalf("new input above the limit must be rejected, got %v", err)
	}
}

// Upstream only lower-cased and trimmed names, so "bench  press" (two spaces)
// could exist. The migration normalises it so it stays reachable by name.
func TestMigrationNormalisesLegacyExerciseNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db := buildV9(t, path)
	const ts = `'2026-01-05 10:00:00'`
	exec(t, db,
		`INSERT INTO plans(id,name,notes,created_at) VALUES (1,'P','',`+ts+`)`,
		`INSERT INTO plan_items(plan_id,position,exercise,target_sets) VALUES (1,1,'bench   press',3)`,
		`INSERT INTO sessions(id,date,started_at) VALUES (1,'2026-01-05',`+ts+`)`,
		`INSERT INTO session_items(session_id,position,exercise,target_sets) VALUES (1,1,'bench  press',3),(1,2,'bench press',4)`,
		`INSERT INTO sets(id,session_id,position,exercise,weight_kg,reps,rpe,si,created_at,updated_at) VALUES
		 (1,1,1,'bench  press',60,8,8,1.0,`+ts+`,`+ts+`),(2,1,2,'bench press',60,8,8,1.0,`+ts+`,`+ts+`)`,
		`INSERT INTO exercise_groups(exercise,muscle_group,updated_at) VALUES ('bench  press','pecho',`+ts+`)`)
	db.Close()

	e := openEnv(t, path)
	exs, err := e.svc.Exercises(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if len(exs) != 1 || exs[0].Name != "bench press" || exs[0].SetCount != 2 || exs[0].MuscleGroup != "pecho" {
		t.Fatalf("the two spellings must collapse into one exercise with its history and group: %+v", exs)
	}
	st, err := e.svc.ExerciseStats(ctx(), "bench press", "", "")
	if err != nil || st.WorkingSets != 2 {
		t.Fatalf("history must be reachable by the normalised name: %+v %v", st, err)
	}
	// Logging the normalised spelling must not create a second exercise.
	if _, err := e.svc.LogSets(ctx(), batch("2026-01-05", set("bench press", 62.5, 8))); err != nil {
		t.Fatal(err)
	}
	if exs, _ = e.svc.Exercises(ctx()); len(exs) != 1 {
		t.Fatalf("exercise list fragmented: %+v", exs)
	}
	p, err := e.st.GetPlan(ctx(), 1)
	if err != nil || p.Items[0].Exercise != "bench press" {
		t.Fatalf("plan item name: %+v %v", p.Items, err)
	}
}
