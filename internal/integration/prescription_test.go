package integration

import (
	"errors"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

func prescribed(t *testing.T, e *env) training.Session {
	t.Helper()
	res, err := e.svc.LogSets(ctx(), training.BatchInput{Prescriptions: []training.PlanItem{
		{Exercise: "squat", TargetSets: 3, RepMin: 4, RepMax: 4, TargetWeightKG: f(130), TargetRIR: f(2)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return res.Workout
}

func TestPrescriptionEditsAreRevisionGuarded(t *testing.T) {
	e := newEnv(t)
	w := prescribed(t, e)
	stale := w.Revision - 1
	sets := 4
	err := e.svc.AdjustSessionItem(ctx(), w.ID, "squat", training.SessionItemPatch{TargetSets: &sets, ExpectedRevision: &stale})
	if !errors.Is(err, training.ErrConflict) {
		t.Fatalf("stale revision must conflict, got %v", err)
	}
	got, _ := e.svc.GetWorkout(ctx(), "")
	if got.Revision != w.Revision || got.Progress[0].TargetSets != 3 {
		t.Fatalf("a rejected edit must change nothing: %+v", got.Progress[0])
	}
	cur := w.Revision
	if err := e.svc.AdjustSessionItem(ctx(), w.ID, "squat", training.SessionItemPatch{TargetSets: &sets, ExpectedRevision: &cur}); err != nil {
		t.Fatal(err)
	}
	got, _ = e.svc.GetWorkout(ctx(), "")
	if got.Revision != w.Revision+1 || got.Progress[0].TargetSets != 4 {
		t.Fatalf("matching revision must apply and bump: rev %d, %+v", got.Revision, got.Progress[0])
	}
}

func TestRemovingPrescriptionBumpsRevisionAndHonoursPrecondition(t *testing.T) {
	e := newEnv(t)
	w := prescribed(t, e)
	stale := w.Revision + 5
	if err := e.svc.RemoveSessionItemChecked(ctx(), w.ID, "squat", &stale); !errors.Is(err, training.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if err := e.svc.RemoveSessionItemChecked(ctx(), w.ID, "squat", &w.Revision); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.GetWorkout(ctx(), "")
	if got.Revision != w.Revision+1 || len(got.Progress) != 0 {
		t.Fatalf("removal must bump the revision so ETags change: rev %d progress %+v", got.Revision, got.Progress)
	}
}

func TestClearingTargetWeightAndRIR(t *testing.T) {
	e := newEnv(t)
	w := prescribed(t, e)
	if err := e.svc.AdjustSessionItem(ctx(), w.ID, "squat", training.SessionItemPatch{ClearTargetWeight: true, ClearTargetRIR: true}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.GetWorkout(ctx(), "")
	p := got.Progress[0]
	if p.TargetWeightKG != nil || p.TargetRIR != nil || p.TargetSets != 3 || p.RepMin != 4 {
		t.Fatalf("only the cleared targets may change: %+v", p)
	}
	w2 := f(100)
	err := e.svc.AdjustSessionItem(ctx(), w.ID, "squat", training.SessionItemPatch{TargetWeightKG: w2, ClearTargetWeight: true})
	if !errors.Is(err, training.ErrValidation) || len(training.IssuesOf(err)) == 0 {
		t.Fatalf("set+clear must be a field-level validation error, got %v", err)
	}
}

func TestAdjustValidationNamesFields(t *testing.T) {
	e := newEnv(t)
	w := prescribed(t, e)
	bad := 99
	err := e.svc.AdjustSessionItem(ctx(), w.ID, "squat", training.SessionItemPatch{TargetSets: &bad})
	requireFields(t, err, "target_sets")
	err = e.svc.AdjustSessionItem(ctx(), w.ID, "squat", training.SessionItemPatch{})
	if !errors.Is(err, training.ErrValidation) || len(training.IssuesOf(err)) == 0 {
		t.Fatalf("empty patch must explain itself, got %v", err)
	}
}

func TestTechniqueSetsNeverBecomeRecords(t *testing.T) {
	e := newEnv(t)
	heavyDrop := training.AddSetInput{Exercise: "leg press", WeightKG: f(300), Reps: n(5), Technique: "drop set"}
	_, err := e.svc.LogSets(ctx(), training.BatchInput{Sets: []training.AddSetInput{set("leg press", 200, 5), heavyDrop}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := e.svc.ExerciseStats(ctx(), "leg press", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if st.MaxWeight == nil || st.MaxWeight.Value != 200 {
		t.Fatalf("a technique set must not be the max weight: %+v", st.MaxWeight)
	}
	if st.VolumeKG != 200*5+300*5 {
		t.Fatalf("technique sets still count toward volume, got %v", st.VolumeKG)
	}
	h, _ := e.svc.ExerciseHistory(ctx(), "leg press", 10)
	if h.Best == nil || *h.Best.WeightKG != 200 {
		t.Fatalf("history record must agree with stats: %+v", h.Best)
	}
}
