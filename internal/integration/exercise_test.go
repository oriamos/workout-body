package integration

import (
	"strconv"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

func TestExerciseIdentity(t *testing.T) {
	e := newEnv(t)
	a := e.log(batch("2026-03-09", set("bench press", 60, 8)))
	b := e.log(batch("2026-03-10", set("  Bench   PRESS\t", 62.5, 8)))
	bench := a.Sets[0].ExerciseID
	if b.Sets[0].ExerciseID != bench || b.Sets[0].Exercise != "bench press" || len(b.NewExercises) != 0 {
		t.Fatalf("normalization failed: %+v / new=%v", b.Sets[0], b.NewExercises)
	}

	// Aliases resolve to the same id.
	if err := e.svc.AddExerciseAlias(ctx(), bench, " BP "); err != nil {
		t.Fatal(err)
	}
	c := e.log(batch("2026-03-10", set("bp", 65, 5)))
	if c.Sets[0].ExerciseID != bench || c.Sets[0].Exercise != "bench press" || len(c.NewExercises) != 0 {
		t.Fatalf("alias did not resolve: %+v", c.Sets[0])
	}
	ex, err := e.svc.GetExercise(ctx(), "BP")
	if err != nil || ex.ID != bench || ex.SetCount != 3 || !contains(ex.Aliases, "bp") {
		t.Fatalf("GetExercise by alias: %v %+v", err, ex)
	}
	if byID, err := e.svc.GetExercise(ctx(), strconv.FormatInt(bench, 10)); err != nil || byID.Name != "bench press" {
		t.Fatalf("GetExercise by id: %v", err)
	}
	// An explicit exercise_id wins over the name.
	d := e.log(batch("2026-03-10", training.AddSetInput{ExerciseID: bench, Exercise: "ignored", Reps: n(3)}))
	if d.Sets[0].ExerciseID != bench || contains(e.exerciseNames(), "ignored") {
		t.Fatalf("exercise_id not honoured: %+v", d.Sets[0])
	}

	// Aliases never merge exercises.
	e.log(batch("2026-03-10", set("squat", 100, 5)))
	wantErr(t, e.svc.AddExerciseAlias(ctx(), bench, "Squat"), training.ErrConflict)
	sq, _ := e.svc.GetExercise(ctx(), "squat")
	wantErr(t, e.svc.AddExerciseAlias(ctx(), sq.ID, "bp"), training.ErrConflict)
	wantErr(t, e.svc.AddExerciseAlias(ctx(), 999, "x"), training.ErrNotFound)
	if err := e.svc.AddExerciseAlias(ctx(), bench, "bp"); err != nil {
		t.Fatalf("re-adding an alias is idempotent: %v", err)
	}

	// A near miss creates a new exercise and says what it looks like.
	typo := e.log(batch("2026-03-10", set("bench pres", 60, 8)))
	if typo.Sets[0].ExerciseID == bench {
		t.Fatal("near miss was silently merged")
	}
	if len(typo.NewExercises) != 1 || typo.NewExercises[0].Name != "bench pres" || !contains(typo.NewExercises[0].Similar, "bench press") {
		t.Fatalf("near miss not reported: %+v", typo.NewExercises)
	}
	if _, err := e.svc.GetExercise(ctx(), "benchpress"); err == nil {
		t.Fatal("lookups must never guess")
	} else {
		wantErr(t, err, training.ErrNotFound)
	}

	// Rename updates sets, plans, prescriptions and history; ids stay.
	plan, err := e.svc.CreatePlan(ctx(), training.Plan{Name: "Push", Items: []training.PlanItem{{Exercise: "bp", TargetSets: 3, RepMin: 5, RepMax: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Items[0].Exercise != "bench press" {
		t.Fatalf("plan item via alias: %q", plan.Items[0].Exercise)
	}
	e.log(training.BatchInput{Date: "2026-03-10", Prescriptions: []training.PlanItem{{Exercise: "bp", TargetSets: 4}}})
	if err := e.svc.SetExerciseGroup(ctx(), "bp", "pecho"); err != nil {
		t.Fatal(err)
	}
	wantErr(t, e.svc.RenameExercise(ctx(), bench, "squat"), training.ErrConflict)
	if err := e.svc.RenameExercise(ctx(), bench, "Barbell Bench Press"); err != nil {
		t.Fatal(err)
	}
	w := e.workout("2026-03-10")
	for _, st := range w.Sets {
		if st.ExerciseID == bench && st.Exercise != "barbell bench press" {
			t.Fatalf("set not renamed: %+v", st)
		}
	}
	if old := setsOf(w, "bench press"); len(old) != 0 {
		t.Fatalf("old name still on %d sets", len(old))
	}
	if p, ok := progressFor(w, "barbell bench press"); !ok || p.TargetSets != 4 || p.DoneSets != 3 || p.MuscleGroup != "pecho" {
		t.Fatalf("prescription/group after rename: %+v", w.Progress)
	}
	if p, _ := e.svc.GetPlan(ctx(), plan.ID); p.Items[0].Exercise != "barbell bench press" {
		t.Fatalf("plan not renamed: %+v", p.Items)
	}
	if _, err := e.svc.GetExercise(ctx(), "bench press"); err == nil {
		t.Fatal("old name should no longer resolve (rename does not add an alias)")
	}
	if got := e.count(`SELECT COUNT(*) FROM sets WHERE exercise='bench press'`); got != 0 {
		t.Fatalf("%d rows still carry the old name", got)
	}

	// History and stats via the alias.
	hist, err := e.svc.ExerciseHistory(ctx(), "BP", 0)
	if err != nil {
		t.Fatal(err)
	}
	if hist.Exercise != "barbell bench press" || len(hist.Sets) != 4 || hist.Sets[0].Date != "2026-03-10" || hist.MuscleGroup != "pecho" {
		t.Fatalf("history via alias: %+v", hist)
	}
	if hist.Best == nil || *hist.Best.WeightKG != 62.5 || hist.Best.Est1RM != 79.2 { // 62.5x8 beats 65x5 by Epley
		t.Fatalf("best via alias: %+v", hist.Best)
	}
	stats, err := e.svc.ExerciseStats(ctx(), "bp", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if stats.ExerciseID != bench || stats.WorkingSets != 4 || stats.Workouts != 2 {
		t.Fatalf("stats via alias: %+v", stats)
	}
	if _, err := e.svc.ExerciseStats(ctx(), "nonexistent", "", ""); err == nil {
		t.Fatal("stats for an unknown exercise must fail")
	}

	// Removing the alias keeps the sets on the exercise.
	if err := e.svc.RemoveExerciseAlias(ctx(), "bp"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GetExercise(ctx(), "bp"); err == nil {
		t.Fatal("removed alias still resolves")
	}
	if ex, _ := e.svc.GetExercise(ctx(), "barbell bench press"); ex.SetCount != 4 {
		t.Fatalf("sets lost with alias: %+v", ex)
	}
}
