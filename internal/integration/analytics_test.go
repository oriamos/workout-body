package integration

import (
	"reflect"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

// seedAnalytics writes a small dataset whose statistics are computed by hand
// in the tests below. 2026-03-02 is a Monday, 2026-03-08 a Sunday.
func seedAnalytics(t *testing.T, e *env) {
	t.Helper()
	if err := e.svc.SetExerciseGroup(ctx(), "bench press", "pecho"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetExerciseGroup(ctx(), "squat", "cuadriceps"); err != nil {
		t.Fatal(err)
	}
	e.log(batch("2026-03-02",
		warmup("bench press", 40, 10),
		set("bench press", 80, 8),                                      // 640, e1RM 101.3
		set("bench press", 85, 6),                                      // 510, e1RM 102.0 (best)
		training.AddSetInput{Exercise: "bench press", WeightKG: f(85)}, // reps unknown: unrecorded
	))
	e.log(batch("2026-03-04",
		warmup("squat", 60, 5),
		set("squat", 100, 5), // 500
		set("squat", 110, 3), // 330
	))
	e.log(batch("2026-03-08",
		set("bench press", 60, 15),                                 // 900, reps > 12: no e1RM
		training.AddSetInput{Exercise: "bench press", Reps: n(10)}, // weight unknown: unrecorded
	))
	e.log(batch("2026-03-09",
		set("squat", 120, 2),      // 240
		set("bench press", 90, 1), // 90, e1RM 93.0
	))
	e.log(batch("2026-02-20", set("bench press", 200, 1))) // outside the ranges below
}

func TestExerciseStatsHandComputed(t *testing.T) {
	e := newEnv(t)
	seedAnalytics(t, e)
	st, err := e.svc.ExerciseStats(ctx(), "Bench Press", "2026-03-01", "2026-03-10")
	if err != nil {
		t.Fatal(err)
	}
	if st.Workouts != 3 || st.WorkingSets != 6 || st.WarmupSets != 1 || st.VolumeKG != 2140 {
		t.Fatalf("totals: workouts=%d working=%d warmup=%d volume=%v", st.Workouts, st.WorkingSets, st.WarmupSets, st.VolumeKG)
	}
	if st.BestE1RM == nil || st.BestE1RM.Value != 102 || st.BestE1RM.Date != "2026-03-02" {
		t.Fatalf("best e1RM: %+v", st.BestE1RM)
	}
	if st.MaxWeight == nil || st.MaxWeight.Value != 90 || st.MaxWeight.Date != "2026-03-09" {
		t.Fatalf("max weight: %+v", st.MaxWeight)
	}
	if st.MaxRepsAtTop == nil || st.MaxRepsAtTop.Value != 15 || st.MaxRepsAtTop.Date != "2026-03-08" {
		t.Fatalf("max reps: %+v", st.MaxRepsAtTop)
	}
	want := []training.ExerciseStatsPoint{
		{Date: "2026-03-02", WorkingSets: 3, WarmupSets: 1, TotalReps: 14, VolumeKG: 1150, TopWeightKG: f(85), BestE1RM: 102, UnrecordedSets: 1},
		{Date: "2026-03-08", WorkingSets: 2, TotalReps: 25, VolumeKG: 900, TopWeightKG: f(60), BestE1RM: 0, UnrecordedSets: 1},
		{Date: "2026-03-09", WorkingSets: 1, TotalReps: 1, VolumeKG: 90, TopWeightKG: f(90), BestE1RM: 93},
	}
	if !reflect.DeepEqual(st.Points, want) {
		t.Fatalf("points:\n got %s\nwant %s", mustJSON(t, st.Points), mustJSON(t, want))
	}

	// Range filtering is by workout date, inclusive.
	mid, err := e.svc.ExerciseStats(ctx(), "bench press", "2026-03-03", "2026-03-08")
	if err != nil {
		t.Fatal(err)
	}
	if mid.Workouts != 1 || mid.WorkingSets != 2 || mid.VolumeKG != 900 || mid.BestE1RM != nil {
		t.Fatalf("mid range: %+v", mid)
	}
	all, _ := e.svc.ExerciseStats(ctx(), "bench press", "", "")
	if all.Workouts != 4 || all.BestE1RM.Value != 206.7 || all.MaxWeight.Value != 200 {
		t.Fatalf("unbounded: %+v", all)
	}
	_, err = e.svc.ExerciseStats(ctx(), "bench press", "2026-03-10", "2026-03-01")
	requireFields(t, err, "from")
}

func TestTrainingSummaryHandComputed(t *testing.T) {
	e := newEnv(t)
	seedAnalytics(t, e)
	sum, err := e.svc.TrainingSummary(ctx(), "2026-03-02", "2026-03-09")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Workouts != 4 || sum.WorkingSets != 9 || sum.WarmupSets != 2 || sum.VolumeKG != 3210 || sum.UnrecordedSets != 2 {
		t.Fatalf("totals: %+v", sum)
	}
	wantEx := []struct {
		name   string
		sets   int
		volume float64
	}{{"bench press", 6, 2140}, {"squat", 3, 1070}}
	if len(sum.ByExercise) != 2 {
		t.Fatalf("by exercise: %+v", sum.ByExercise)
	}
	for i, w := range wantEx {
		g := sum.ByExercise[i]
		if g.Exercise != w.name || g.WorkingSets != w.sets || g.VolumeKG != w.volume || g.ExerciseID == 0 {
			t.Fatalf("by exercise[%d]: %+v", i, g)
		}
	}
	wantMuscle := []training.GroupSummary{{MuscleGroup: "pecho", WorkingSets: 6, VolumeKG: 2140}, {MuscleGroup: "cuadriceps", WorkingSets: 3, VolumeKG: 1070}}
	if !reflect.DeepEqual(sum.ByMuscle, wantMuscle) {
		t.Fatalf("by muscle: %+v", sum.ByMuscle)
	}
	wantWeeks := []training.WeekSummary{
		{WeekStart: "2026-03-02", Workouts: 3, WorkingSets: 7, VolumeKG: 2880}, // Mon 2 .. Sun 8
		{WeekStart: "2026-03-09", Workouts: 1, WorkingSets: 2, VolumeKG: 330},
	}
	if !reflect.DeepEqual(sum.ByWeek, wantWeeks) {
		t.Fatalf("by week: %+v", sum.ByWeek)
	}

	// Default range is the 28 days ending today in the configured timezone.
	def, err := e.svc.TrainingSummary(ctx(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if def.To != "2026-03-10" || def.From != "2026-02-11" || def.Workouts != 5 {
		t.Fatalf("default range: %s..%s workouts=%d", def.From, def.To, def.Workouts)
	}
	empty, err := e.svc.TrainingSummary(ctx(), "2025-01-01", "2025-01-31")
	if err != nil || empty.Workouts != 0 || empty.ByWeek == nil || empty.ByExercise == nil {
		t.Fatalf("empty range: %v %+v", err, empty)
	}
}
