package integration

import (
	"bytes"
	"errors"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

// Regression tests for defects found by the independent code review.

func TestMergedRepRangeCannotBecomeInverted(t *testing.T) {
	e := newEnv(t)
	res, err := e.svc.LogSets(ctx(), training.BatchInput{Prescriptions: []training.PlanItem{
		{Exercise: "bench press", TargetSets: 3, RepMin: 8, RepMax: 12},
	}})
	if err != nil {
		t.Fatal(err)
	}
	w := res.Workout
	six := 6
	// A patch sending only rep_max=6 would leave the stored range at 8-6.
	err = e.svc.AdjustSessionItem(ctx(), w.ID, "bench press", training.SessionItemPatch{RepMax: &six})
	if !errors.Is(err, training.ErrValidation) {
		t.Fatalf("patch must be rejected, got %v", err)
	}
	// The same through a batch prescription (rep_min omitted => keeps 8).
	_, err = e.svc.LogSets(ctx(), training.BatchInput{Prescriptions: []training.PlanItem{
		{Exercise: "bench press", TargetSets: 3, RepMax: 6},
	}})
	if !errors.Is(err, training.ErrValidation) {
		t.Fatalf("batch merge must be rejected, got %v", err)
	}
	got, _ := e.svc.GetWorkout(ctx(), "")
	if p := got.Progress[0]; p.RepMin != 8 || p.RepMax != 12 {
		t.Fatalf("a rejected change must leave the range untouched, got %d-%d", p.RepMin, p.RepMax)
	}
	// Sending both together is fine.
	if _, err := e.svc.LogSets(ctx(), training.BatchInput{Prescriptions: []training.PlanItem{
		{Exercise: "bench press", TargetSets: 3, RepMin: 4, RepMax: 6},
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestFutureWorkoutRejectsSetsOnEveryPath(t *testing.T) {
	e := newEnv(t)
	w, err := e.svc.StartSession(ctx(), "2026-04-01") // planning a future day is allowed
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = e.svc.AddSet(ctx(), training.AddSetInput{SessionID: w.ID, Exercise: "squat", WeightKG: f(100), Reps: n(5)})
	if !errors.Is(err, training.ErrValidation) {
		t.Fatalf("add_set into a future workout must be rejected, got %v", err)
	}
	if got, _ := e.svc.GetWorkout(ctx(), "2026-04-01"); len(got.Sets) != 0 {
		t.Fatal("a rejected set was stored")
	}
}

func TestLoadSuggestionIgnoresWarmupsAndIncompleteSets(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.LogSets(ctx(), training.BatchInput{Date: "2026-03-09", Sets: []training.AddSetInput{
		{Exercise: "bench press", WeightKG: f(100), Reps: n(6), RPE: f(8)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.LogSets(ctx(), training.BatchInput{Date: "2026-03-10", Sets: []training.AddSetInput{
		{Exercise: "bench press", WeightKG: f(40), Reps: n(8), RPE: f(4), SetType: training.SetTypeWarmup},
		{Exercise: "bench press", Reps: n(5)}, // weight unknown
	}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := e.svc.SuggestLoad(ctx(), "bench press")
	if err != nil {
		t.Fatal(err)
	}
	if s.LastWeightKG != 100 {
		t.Fatalf("advice must come from the last complete working set (100 kg), got %+v", s)
	}
}

func TestSwapOntoAlreadyPrescribedExerciseIsAConflict(t *testing.T) {
	e := newEnv(t)
	res, err := e.svc.LogSets(ctx(), training.BatchInput{Prescriptions: []training.PlanItem{
		{Exercise: "bench press", TargetSets: 3}, {Exercise: "incline press", TargetSets: 3},
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = e.svc.SwapSessionItem(ctx(), res.Workout.ID, "bench press", "incline press")
	if !errors.Is(err, training.ErrConflict) {
		t.Fatalf("want a clean conflict (not a raw SQLite error), got %v", err)
	}
}

func TestMaxRepsIgnoresTechniqueSets(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.LogSets(ctx(), training.BatchInput{Sets: []training.AddSetInput{
		{Exercise: "curl", WeightKG: f(20), Reps: n(10)},
		{Exercise: "curl", WeightKG: f(8), Reps: n(25), Technique: "drop set"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := e.svc.ExerciseStats(ctx(), "curl", "", "")
	if st.MaxRepsAtTop == nil || st.MaxRepsAtTop.Value != 10 {
		t.Fatalf("max_reps must come from a straight set, got %+v", st.MaxRepsAtTop)
	}
	if st.VolumeKG != 20*10+8*25 {
		t.Fatalf("technique sets still count toward volume, got %v", st.VolumeKG)
	}
}

func TestSummaryWorkoutCountsAgreeWithWeeks(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.LogSets(ctx(), training.BatchInput{Date: "2026-03-09", Sets: []training.AddSetInput{set("squat", 100, 5)}})
	if err != nil {
		t.Fatal(err)
	}
	// A day with only a warm-up is not a completed-work day.
	_, err = e.svc.LogSets(ctx(), training.BatchInput{Date: "2026-03-10", Sets: []training.AddSetInput{
		{Exercise: "squat", WeightKG: f(40), Reps: n(5), SetType: training.SetTypeWarmup},
	}})
	if err != nil {
		t.Fatal(err)
	}
	sum, err := e.svc.TrainingSummary(ctx(), "2026-03-01", "2026-03-31")
	if err != nil {
		t.Fatal(err)
	}
	weeks := 0
	for _, w := range sum.ByWeek {
		weeks += w.Workouts
	}
	if sum.Workouts != 1 || weeks != sum.Workouts || sum.WarmupSets != 1 {
		t.Fatalf("workouts=%d, per-week total=%d, warmups=%d", sum.Workouts, weeks, sum.WarmupSets)
	}
}

func TestPlanNamesAreUnique(t *testing.T) {
	e := newEnv(t)
	mk := func(name string) error {
		_, err := e.svc.CreatePlan(ctx(), training.Plan{Name: name, Items: []training.PlanItem{{Exercise: "squat", TargetSets: 3}}})
		return err
	}
	if err := mk("Leg Day"); err != nil {
		t.Fatal(err)
	}
	if err := mk("leg day"); !errors.Is(err, training.ErrConflict) {
		t.Fatalf("duplicate name (any case) must conflict, got %v", err)
	}
	if err := mk("Push"); err != nil {
		t.Fatal(err)
	}
	other := "LEG DAY"
	plans, _ := e.svc.ListPlans(ctx())
	var push int64
	for _, p := range plans {
		if p.Name == "Push" {
			push = p.ID
		}
	}
	if err := e.svc.UpdatePlan(ctx(), push, training.PlanPatch{Name: &other}); !errors.Is(err, training.ErrConflict) {
		t.Fatalf("renaming onto an existing name must conflict, got %v", err)
	}
	data := []byte(`{"format":"workout-plan","version":1,"name":"Leg Day","items":[{"exercise":"deadlift","sets":3}]}`)
	if _, err := e.svc.ReplacePlan(ctx(), push, data, nil); !errors.Is(err, training.ErrConflict) {
		t.Fatalf("replace must not steal another plan's name, got %v", err)
	}
}

func TestStaleRevisionIsDistinguishableFromOtherConflicts(t *testing.T) {
	e := newEnv(t)
	res, _ := e.svc.LogSets(ctx(), batch("", set("row", 50, 10)))
	s := res.Sets[0]
	stale := s.Revision + 3
	_, _, err := e.svc.UpdateSet(ctx(), s.ID, training.SetPatch{Reps: n(11), ExpectedRevision: &stale})
	if !errors.Is(err, training.ErrStale) || !errors.Is(err, training.ErrConflict) {
		t.Fatalf("a stale revision must be both ErrStale and ErrConflict, got %v", err)
	}
	// An idempotency-key clash is a conflict but not a stale revision.
	_, _ = e.svc.LogSets(ctx(), training.BatchInput{IdempotencyKey: "k", Sets: []training.AddSetInput{set("row", 1, 1)}})
	_, err = e.svc.LogSets(ctx(), training.BatchInput{IdempotencyKey: "k", Sets: []training.AddSetInput{set("row", 2, 2)}})
	if !errors.Is(err, training.ErrConflict) || errors.Is(err, training.ErrStale) {
		t.Fatalf("key reuse is a plain conflict, got %v", err)
	}
}

func TestRenamingAnExerciseBumpsAffectedRevisions(t *testing.T) {
	e := newEnv(t)
	res, _ := e.svc.LogSets(ctx(), batch("", set("bench pres", 60, 8)))
	s, w := res.Sets[0], res.Workout
	if err := e.svc.RenameExercise(ctx(), s.ExerciseID, "bench press"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.GetSet(ctx(), s.ID)
	cur, _ := e.svc.GetWorkout(ctx(), "")
	if got.Revision <= s.Revision || cur.Revision <= w.Revision {
		t.Fatalf("rename changes displayed data, so revisions must move: set %d->%d, workout %d->%d", s.Revision, got.Revision, w.Revision, cur.Revision)
	}
}

func TestVideoSnifferRejectsImagesAndNonWebmEBML(t *testing.T) {
	e, _ := mediaEnv(t, 1<<20)
	logOne(t, e)
	heic := append([]byte{0, 0, 0, 0x18}, []byte("ftypheic")...)
	heic = append(heic, make([]byte, 64)...)
	if _, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "photo.mp4", "", bytes.NewReader(heic)); !errors.Is(err, training.ErrValidation) {
		t.Fatalf("a HEIC image must not be accepted as a video, got %v", err)
	}
	matroska := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, []byte("....matroska....")...)
	if _, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "x.webm", "", bytes.NewReader(matroska)); !errors.Is(err, training.ErrValidation) {
		t.Fatalf("a non-webm EBML file must not be accepted as webm, got %v", err)
	}
	webm := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, []byte("....webm....")...)
	if _, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "x.webm", "", bytes.NewReader(webm)); err != nil {
		t.Fatalf("a real webm header must be accepted: %v", err)
	}
}

func TestVideoLimitIsCheckedBeforeWritingTheFile(t *testing.T) {
	e, dir := mediaEnv(t, 1<<20)
	logOne(t, e)
	for i := 0; i < training.MaxMediaPerSet; i++ {
		if _, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "v.mp4", "", bytes.NewReader(mp4(64))); err != nil {
			t.Fatal(err)
		}
	}
	before := filesIn(t, dir)
	if _, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "v.mp4", "", &failIfRead{t}); !errors.Is(err, training.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if filesIn(t, dir) != before {
		t.Fatal("files changed")
	}
}

// failIfRead fails the test if the body is consumed: a doomed upload must be
// refused before any bytes are read.
type failIfRead struct{ t *testing.T }

func (f *failIfRead) Read([]byte) (int, error) {
	f.t.Error("the upload body was read although the owner is already at its video limit")
	return 0, errors.New("unexpected read")
}
