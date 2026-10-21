package integration

import (
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

// The product-spec conversation, one call per utterance.
func TestIncrementalLoggingScenario(t *testing.T) {
	e := newEnv(t)

	// (a) "bench 8 reps at 60, planning 3 sets"
	a := e.log(training.BatchInput{
		Sets:          []training.AddSetInput{set("bench press", 60, 8)},
		Prescriptions: []training.PlanItem{rx("bench press", 3, 8, 8, f(60))},
	})
	// (b) "second set, same"
	b := e.log(batch("", set("bench press", 60, 8)))
	// (c) "third set, and I'm doing squats 130 for 4, 3 sets"
	c := e.log(training.BatchInput{
		Sets:          []training.AddSetInput{set("bench press", 60, 8)},
		Prescriptions: []training.PlanItem{rx("squat", 3, 4, 4, f(130))},
	})
	if a.Workout.ID != b.Workout.ID || b.Workout.ID != c.Workout.ID {
		t.Fatal("the conversation must build one workout")
	}
	set1, set2, set3 := a.Sets[0], b.Sets[0], c.Sets[0]
	if set2.SetNumber != 0 && set2.SetNumber != 2 {
		t.Fatalf("set number %d", set2.SetNumber)
	}

	// (d) "actually the second set was 7"
	updated, _, err := e.svc.UpdateSet(ctx(), set2.ID, training.SetPatch{Reps: n(7)})
	if err != nil {
		t.Fatal(err)
	}
	if *updated.Reps != 7 {
		t.Fatalf("update returned %+v", updated)
	}

	w := e.workout("")
	bench := setsOf(w, "bench press")
	if len(bench) != 3 {
		t.Fatalf("want 3 bench sets, got %d", len(bench))
	}
	for i, st := range bench {
		if st.SetNumber != i+1 || *st.WeightKG != 60 {
			t.Fatalf("bench set %d: %+v", i, st)
		}
	}
	if bench[0].ID != set1.ID || bench[1].ID != set2.ID || bench[2].ID != set3.ID {
		t.Fatal("set ids changed")
	}
	if *bench[1].Reps != 7 || *bench[0].Reps != 8 || *bench[2].Reps != 8 {
		t.Fatalf("reps %d/%d/%d, want 8/7/8", *bench[0].Reps, *bench[1].Reps, *bench[2].Reps)
	}
	if bench[0].Revision != set1.Revision || bench[2].Revision != set3.Revision || bench[1].Revision != set2.Revision+1 {
		t.Fatal("only the corrected set may change revision")
	}
	if len(setsOf(w, "squat")) != 0 {
		t.Fatal("a squat prescription must not create performed sets")
	}

	bp, ok := progressFor(w, "bench press")
	if !ok || bp.TargetSets != 3 || bp.DoneSets != 3 || bp.RepMin != 8 || bp.RepMax != 8 || !eqF(bp.TargetWeightKG, f(60)) || !bp.Done() {
		t.Fatalf("bench progress: %+v", bp)
	}
	sq, ok := progressFor(w, "squat")
	if !ok || sq.TargetSets != 3 || sq.RepMin != 4 || sq.RepMax != 4 || !eqF(sq.TargetWeightKG, f(130)) {
		t.Fatalf("squat prescription: %+v", sq)
	}
	if sq.DoneSets != 0 || sq.Done() {
		t.Fatalf("squat must have zero completed sets, got %d", sq.DoneSets)
	}
}

func TestPlannedVersusCompleted(t *testing.T) {
	e := newEnv(t)

	res := e.log(training.BatchInput{Prescriptions: []training.PlanItem{
		{Exercise: "squat", TargetSets: 3, RepMin: 6, RepMax: 8, TargetRPE: 8, TargetWeightKG: f(100)},
		{Exercise: "leg curl", TargetSets: 2},
	}})
	if len(res.Sets) != 0 || len(res.Workout.Sets) != 0 {
		t.Fatal("prescriptions created sets")
	}
	if got := e.count(`SELECT COUNT(*) FROM sets`); got != 0 {
		t.Fatalf("%d set rows after prescriptions only", got)
	}

	e.log(batch("", warmup("squat", 60, 5), warmup("squat", 80, 3), set("squat", 100, 8), set("squat", 100, 7)))
	// A later batch that only says "make it 4 sets" keeps the rest.
	e.log(training.BatchInput{Prescriptions: []training.PlanItem{{Exercise: "Squat", TargetSets: 4}}})

	w := e.workout("")
	sq, _ := progressFor(w, "squat")
	if sq.TargetSets != 4 || sq.RepMin != 6 || sq.RepMax != 8 || sq.TargetRPE != 8 || !eqF(sq.TargetWeightKG, f(100)) {
		t.Fatalf("merge lost earlier prescription values: %+v", sq)
	}
	if sq.DoneSets != 2 || sq.WarmupSets != 2 {
		t.Fatalf("done=%d warmup=%d, want 2/2", sq.DoneSets, sq.WarmupSets)
	}
	lc, _ := progressFor(w, "leg curl")
	if lc.TargetSets != 2 || lc.DoneSets != 0 {
		t.Fatalf("leg curl: %+v", lc)
	}
	// A later explicit rep range does replace the old one.
	e.log(training.BatchInput{Prescriptions: []training.PlanItem{{Exercise: "squat", TargetSets: 4, RepMin: 3, RepMax: 5}}})
	sq, _ = progressFor(e.workout(""), "squat")
	if sq.RepMin != 3 || sq.RepMax != 5 || !eqF(sq.TargetWeightKG, f(100)) {
		t.Fatalf("explicit update not applied: %+v", sq)
	}
	// Off-plan work is visible with target 0.
	e.log(batch("", set("calf raise", 50, 15)))
	cr, ok := progressFor(e.workout(""), "calf raise")
	if !ok || cr.TargetSets != 0 || cr.DoneSets != 1 {
		t.Fatalf("off-plan progress: %+v", cr)
	}
	// SI totals ignore warm-ups.
	if w.TotalSI != 0 {
		t.Fatalf("no RPE recorded, TotalSI must be 0, got %v", w.TotalSI)
	}
}

func TestCorrectAndDeleteOneSetLeavesOthersIntact(t *testing.T) {
	e := newEnv(t)
	res := e.log(batch("", set("bench press", 60, 8), set("bench press", 62.5, 8), set("row", 50, 10), set("bench press", 65, 6)))
	before := e.workout("")

	upd, _, err := e.svc.UpdateSet(ctx(), res.Sets[1].ID, training.SetPatch{WeightKG: f(61.25), RPE: f(8)})
	if err != nil {
		t.Fatal(err)
	}
	if *upd.WeightKG != 61.25 || *upd.Reps != 8 || upd.Revision != 2 || upd.Position != 2 {
		t.Fatalf("updated set: %+v", upd)
	}
	after := e.workout("")
	if after.Revision <= before.Revision {
		t.Fatal("workout revision must increase on a set change")
	}
	for i := range before.Sets {
		if i == 1 {
			continue
		}
		b, a := before.Sets[i], after.Sets[i]
		if a.ID != b.ID || a.Position != b.Position || a.Revision != b.Revision || !eqF(a.WeightKG, b.WeightKG) || !eqI(a.Reps, b.Reps) || a.Exercise != b.Exercise {
			t.Fatalf("set %d changed: %+v -> %+v", i, b, a)
		}
	}

	// Delete the second bench set: ids stable, positions compacted.
	if _, _, remaining, err := e.svc.DeleteSet(ctx(), res.Sets[1].ID, nil); err != nil || remaining != 3 {
		t.Fatalf("delete: remaining=%d err=%v", remaining, err)
	}
	w := e.workout("")
	wantIDs := []int64{res.Sets[0].ID, res.Sets[2].ID, res.Sets[3].ID}
	if len(w.Sets) != 3 {
		t.Fatalf("%d sets after delete", len(w.Sets))
	}
	for i, st := range w.Sets {
		if st.ID != wantIDs[i] || st.Position != i+1 {
			t.Fatalf("after delete set %d: id=%d pos=%d", i, st.ID, st.Position)
		}
	}
	if w.Sets[2].SetNumber != 2 || *w.Sets[2].WeightKG != 65 {
		t.Fatalf("last bench set should now be bench #2: %+v", w.Sets[2])
	}
	if _, err := e.svc.GetSet(ctx(), res.Sets[1].ID); err == nil {
		t.Fatal("deleted set still readable")
	}
	// Appending after a delete continues from the compacted end.
	more := e.log(batch("", set("bench press", 60, 5)))
	if more.Sets[0].Position != 4 {
		t.Fatalf("new set position %d, want 4", more.Sets[0].Position)
	}
	if _, _, _, err := e.svc.DeleteSet(ctx(), res.Sets[1].ID, nil); err == nil {
		t.Fatal("deleting a deleted set must fail")
	} else {
		wantErr(t, err, training.ErrNotFound)
	}
}

func TestPartialValuesRoundTrip(t *testing.T) {
	e := newEnv(t)
	res := e.log(batch("",
		training.AddSetInput{Exercise: "pull up", Reps: n(10)},                                       // weight unknown
		training.AddSetInput{Exercise: "pull up", Reps: n(8), WeightKG: f(0)},                        // explicit zero load
		training.AddSetInput{Exercise: "pull up", Reps: n(6), WeightKG: f(10), RPE: f(8), RIR: f(2)}, // full
		training.AddSetInput{Exercise: "plank"},                                                      // nothing but the exercise
	))
	w := e.workout("")
	if len(w.Sets) != 4 {
		t.Fatalf("%d sets", len(w.Sets))
	}
	unknown, zero, full, bare := w.Sets[0], w.Sets[1], w.Sets[2], w.Sets[3]
	if unknown.WeightKG != nil || *unknown.Reps != 10 || unknown.RPE != nil || unknown.RIR != nil || unknown.SI != 0 {
		t.Fatalf("reps-only set: %+v", unknown)
	}
	if zero.WeightKG == nil || *zero.WeightKG != 0 {
		t.Fatalf("explicit 0 kg lost: weight=%s", fmtF(zero.WeightKG))
	}
	if full.SI != training.CalculateSI(8) || full.SI == 0 || *full.RIR != 2 {
		t.Fatalf("full set: %+v", full)
	}
	if bare.WeightKG != nil || bare.Reps != nil || bare.SetType != training.SetTypeWorking {
		t.Fatalf("bare set: %+v", bare)
	}
	if got := e.count(`SELECT COUNT(*) FROM sets WHERE weight_kg IS NULL`); got != 2 {
		t.Fatalf("want 2 NULL weights in the db, got %d", got)
	}
	if got := e.count(`SELECT COUNT(*) FROM sets WHERE weight_kg = 0`); got != 1 {
		t.Fatalf("want 1 explicit zero weight in the db, got %d", got)
	}

	// Clear flags turn values back into "not recorded".
	id := res.Sets[2].ID
	cleared, _, err := e.svc.UpdateSet(ctx(), id, training.SetPatch{ClearWeight: true, ClearRPE: true, ClearRIR: true})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.WeightKG != nil || cleared.RPE != nil || cleared.RIR != nil || cleared.SI != 0 || *cleared.Reps != 6 {
		t.Fatalf("clear: %+v", cleared)
	}
	if _, _, err = e.svc.UpdateSet(ctx(), id, training.SetPatch{ClearReps: true}); err != nil {
		t.Fatal(err)
	}
	got := e.set(id)
	if got.Reps != nil || got.WeightKG != nil || got.Revision != 3 {
		t.Fatalf("after ClearReps: %+v", got)
	}
	// Setting and clearing the same value is contradictory.
	_, _, err = e.svc.UpdateSet(ctx(), id, training.SetPatch{WeightKG: f(5), ClearWeight: true})
	requireFields(t, err, "weight_kg")
	_, _, err = e.svc.UpdateSet(ctx(), id, training.SetPatch{})
	wantErr(t, err, training.ErrValidation)
	_, _, err = e.svc.UpdateSet(ctx(), id, training.SetPatch{Reps: n(0), RPE: f(11)})
	requireFields(t, err, "reps", "rpe")
	if e.set(id).Revision != 3 {
		t.Fatal("rejected updates changed the set")
	}
}

func TestInvalidBatchWritesNothing(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.LogSets(ctx(), training.BatchInput{Date: "2026-03-09", Sets: []training.AddSetInput{
		set("bench press", 60, 8),
		{Exercise: "bench press", WeightKG: f(60), Reps: n(0)},
		{Exercise: "", WeightKG: f(-1), RPE: f(12), RIR: f(-1)},
	}, Prescriptions: []training.PlanItem{{Exercise: "squat", TargetSets: 0, RepMin: 8, RepMax: 5}}})
	fields := requireFields(t, err, "sets[1].reps", "sets[2].exercise", "sets[2].weight_kg", "sets[2].rpe", "sets[2].rir",
		"prescriptions[0].target_sets", "prescriptions[0].rep_min")
	if _, bad := fields["sets[0].reps"]; bad {
		t.Fatalf("valid set reported: %v", joinFields(fields))
	}
	for _, table := range []string{"sessions", "sets", "session_items", "exercises"} {
		if got := e.count(`SELECT COUNT(*) FROM ` + table); got != 0 {
			t.Fatalf("invalid batch left %d rows in %s", got, table)
		}
	}

	// A failure discovered inside the transaction (unknown exercise_id in the
	// second set) also rolls everything back, including the new workout and
	// the exercise created by the first set.
	_, err = e.svc.LogSets(ctx(), batch("2026-03-09", set("bench press", 60, 8), training.AddSetInput{ExerciseID: 9999, Reps: n(5)}))
	wantErr(t, err, training.ErrNotFound)
	for _, table := range []string{"sessions", "sets", "exercises"} {
		if got := e.count(`SELECT COUNT(*) FROM ` + table); got != 0 {
			t.Fatalf("failed batch left %d rows in %s", got, table)
		}
	}

	// Too many sets is reported, not truncated.
	many := make([]training.AddSetInput, training.MaxBatchSets+1)
	for i := range many {
		many[i] = set("bench press", 60, 8)
	}
	_, err = e.svc.LogSets(ctx(), batch("", many...))
	requireFields(t, err, "sets")
}

func TestOptionalMetricsPersist(t *testing.T) {
	e := newEnv(t)
	types := []string{"warmup", "working", "backoff", "drop", "failure"}
	var in []training.AddSetInput
	for i, ty := range types {
		x := set("bench press", 60+float64(i), 8)
		x.SetType = ty
		x.RIR = f(float64(i))
		x.Notes = "  note " + ty + "  "
		in = append(in, x)
	}
	in[1].SetType = " WORKING " // normalized
	e.log(batch("", in...))
	w := e.workout("")
	for i, st := range w.Sets {
		if st.SetType != types[i] || *st.RIR != float64(i) || st.Notes != "note "+types[i] {
			t.Fatalf("set %d: type=%q rir=%s notes=%q", i, st.SetType, fmtF(st.RIR), st.Notes)
		}
	}
	bp, _ := progressFor(w, "bench press")
	if bp.DoneSets != 4 || bp.WarmupSets != 1 {
		t.Fatalf("progress done=%d warmup=%d", bp.DoneSets, bp.WarmupSets)
	}

	bad := set("bench press", 60, 8)
	bad.SetType = "amrap"
	_, err := e.svc.LogSets(ctx(), batch("", set("bench press", 60, 8), bad))
	requireFields(t, err, "sets[1].set_type")
	if len(e.workout("").Sets) != 5 {
		t.Fatal("rejected batch wrote sets")
	}

	id := w.Sets[0].ID
	_, _, err = e.svc.UpdateSet(ctx(), id, training.SetPatch{SetType: s("amrap")})
	requireFields(t, err, "set_type")
	upd, _, err := e.svc.UpdateSet(ctx(), id, training.SetPatch{SetType: s("Working"), Notes: s("felt easy"), RIR: f(3)})
	if err != nil {
		t.Fatal(err)
	}
	got := e.set(id)
	if got.SetType != "working" || got.Notes != "felt easy" || *got.RIR != 3 || upd.Revision != 2 {
		t.Fatalf("metric update: %+v", got)
	}
	bp, _ = progressFor(e.workout(""), "bench press")
	if bp.DoneSets != 5 || bp.WarmupSets != 0 {
		t.Fatalf("reclassified warm-up not counted: %+v", bp)
	}
}
