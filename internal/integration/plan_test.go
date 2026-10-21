package integration

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

const pushA = "testdata/plan_push_a.json"

func TestPlanImportExportRoundTrip(t *testing.T) {
	e := newEnv(t)
	res, err := e.svc.ImportPlan(ctx(), readFile(t, pushA), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Replaced || res.Plan.ID == 0 || res.Plan.Revision != 1 || len(res.NewExercises) != 5 {
		t.Fatalf("import result: %+v", res)
	}
	p, err := e.svc.GetPlan(ctx(), res.Plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Push A" || len(p.Items) != 5 || p.TotalSets != 14 {
		t.Fatalf("stored plan: %+v", p)
	}
	first := p.Items[0]
	if first.Exercise != "bench press" || first.TargetSets != 3 || first.RepMin != 6 || first.RepMax != 8 ||
		!eqF(first.TargetWeightKG, f(80)) || first.TargetRPE != 8 || first.TargetRIR != nil || first.Notes != "Pause on the chest." {
		t.Fatalf("first item: %+v", first)
	}
	if ohp := p.Items[2]; !eqF(ohp.TargetWeightKG, f(0)) || ohp.Superset != "A" {
		t.Fatalf("explicit 0 kg target or superset lost: %+v", ohp)
	}
	if lr := p.Items[3]; lr.TargetWeightKG != nil || lr.Superset != "A" {
		t.Fatalf("absent target became a value: %+v", lr)
	}
	if tp := p.Items[4]; !eqF(tp.TargetRIR, f(1)) || tp.TargetRPE != 9 {
		t.Fatalf("rir/rpe lost: %+v", tp)
	}
	for i, it := range p.Items {
		if it.Position != i+1 {
			t.Fatalf("item order: %+v", p.Items)
		}
	}

	doc, err := e.svc.ExportPlan(ctx(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	exported := mustJSON(t, doc)
	if _, err := training.ParsePlanDocument(exported); err != nil {
		t.Fatalf("export is not a valid import: %v", err)
	}
	exercisesBefore := e.exerciseNames()

	re, err := e.svc.ImportPlan(ctx(), exported, training.OnConflictReplace, false)
	if err != nil {
		t.Fatal(err)
	}
	if re.Created || !re.Replaced || re.Plan.ID != p.ID || re.Plan.Revision != 2 || len(re.NewExercises) != 0 {
		t.Fatalf("re-import: %+v", re)
	}
	doc2, err := e.svc.ExportPlan(ctx(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc, doc2) {
		t.Fatalf("import(export(x)) changed the plan:\n%s\n%s", exported, mustJSON(t, doc2))
	}
	if got := e.exerciseNames(); !reflect.DeepEqual(got, exercisesBefore) {
		t.Fatalf("re-import changed exercises: %v -> %v", exercisesBefore, got)
	}
	if got := e.count(`SELECT COUNT(*) FROM plans`); got != 1 {
		t.Fatalf("%d plans", got)
	}

	// on_conflict=error (and the default) refuse and change nothing.
	for _, policy := range []string{"", training.OnConflictError} {
		_, err = e.svc.ImportPlan(ctx(), readFile(t, pushA), policy, false)
		wantErr(t, err, training.ErrConflict)
	}
	after, _ := e.svc.GetPlan(ctx(), p.ID)
	if after.Revision != 2 || !reflect.DeepEqual(after.Items, re.Plan.Items) {
		t.Fatalf("refused import changed the plan: %+v", after)
	}
	_, err = e.svc.ImportPlan(ctx(), readFile(t, pushA), "merge", false)
	requireFields(t, err, "on_conflict")

	// Plan names match case-insensitively.
	renamed := strings.Replace(string(readFile(t, pushA)), `"Push A"`, `"push a"`, 1)
	_, err = e.svc.ImportPlan(ctx(), []byte(renamed), "", false)
	wantErr(t, err, training.ErrConflict)
}

func TestPlanImportReplaceAndDryRun(t *testing.T) {
	e := newEnv(t)
	orig, err := e.svc.ImportPlan(ctx(), readFile(t, pushA), "", false)
	if err != nil {
		t.Fatal(err)
	}
	exBefore := e.exerciseNames()

	replacement := `{"format":"workout-plan","version":1,"name":"Push A","notes":"v2",
	 "items":[{"exercise":"dips","sets":4,"rep_min":6,"rep_max":10},{"exercise":"bench press","sets":5,"rep_min":3,"rep_max":5}]}`

	// Dry runs write nothing: neither the replacement, a new plan, nor exercises.
	dry, err := e.svc.ImportPlan(ctx(), []byte(replacement), training.OnConflictReplace, true)
	if err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || !dry.Replaced || dry.Plan.ID != orig.Plan.ID || len(dry.Plan.Items) != 2 || len(dry.NewExercises) != 1 || dry.NewExercises[0].Name != "dips" {
		t.Fatalf("dry run result: %+v", dry)
	}
	newPlan := `{"format":"workout-plan","version":1,"name":"Pull A","items":[{"exercise":"chin up","sets":3}]}`
	dryNew, err := e.svc.ImportPlan(ctx(), []byte(newPlan), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !dryNew.Created || dryNew.Plan.ID != 0 {
		t.Fatalf("dry new: %+v", dryNew)
	}
	stored, _ := e.svc.GetPlan(ctx(), orig.Plan.ID)
	if stored.Revision != 1 || stored.Notes != orig.Plan.Notes || len(stored.Items) != 5 {
		t.Fatalf("dry run changed the plan: %+v", stored)
	}
	if got := e.count(`SELECT COUNT(*) FROM plans`); got != 1 {
		t.Fatalf("dry run created a plan (%d)", got)
	}
	if got := e.exerciseNames(); !reflect.DeepEqual(got, exBefore) {
		t.Fatalf("dry run created exercises: %v", got)
	}

	// Real replace keeps the id, bumps the revision and swaps the items.
	rep, err := e.svc.ImportPlan(ctx(), []byte(replacement), training.OnConflictReplace, false)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := e.svc.GetPlan(ctx(), orig.Plan.ID)
	if rep.Plan.ID != orig.Plan.ID || p.Revision != 2 || p.Notes != "v2" || len(p.Items) != 2 || p.Items[0].Exercise != "dips" || p.Items[1].TargetSets != 5 {
		t.Fatalf("replaced plan: %+v", p)
	}
	if got := e.count(`SELECT COUNT(*) FROM plan_items`); got != 2 {
		t.Fatalf("stale plan items remain: %d", got)
	}
}

func TestPlanImportValidationReportsEverything(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.ImportPlan(ctx(), readFile(t, pushA), "", false); err != nil {
		t.Fatal(err)
	}
	exBefore := e.exerciseNames()
	planItemsBefore := e.count(`SELECT COUNT(*) FROM plan_items`)

	bad := `{
	  "format": "workout-plan", "version": 1, "name": "Legs B", "author": "me",
	  "items": [
	    {"exercise": "front squat", "sets": "three", "rep_min": 5, "rep_max": 8},
	    {"exercise": "romanian deadlift", "sets": 3, "rep_min": 12, "rep_max": 8, "tempo": "3-1-1"},
	    {"exercise": "Front  Squat", "sets": 2},
	    {"exercise": "leg press", "sets": 3, "rep_min": "x", "target_rpe": 11, "target_weight_kg": -5}
	  ]
	}`
	_, err := e.svc.ImportPlan(ctx(), []byte(bad), training.OnConflictReplace, false)
	fields := requireFields(t, err,
		"author",                    // unknown top-level field
		"items[0].sets",             // bad type
		"items[1].rep_min",          // inverted rep range
		"items[1].tempo",            // unknown item field
		"items[2].exercise",         // duplicate after normalization
		"items[3].rep_min",          // second bad type: all type errors are reported
		"items[3].target_rpe",       // range
		"items[3].target_weight_kg", // range
	)
	if !strings.Contains(fields["items[0].sets"], "string") {
		t.Errorf("type error message should name the bad type: %q", fields["items[0].sets"])
	}
	if strings.Contains(fields["items[0].sets"], "between") {
		t.Errorf("type error must not be doubled by a range error: %q", fields["items[0].sets"])
	}
	if got := e.count(`SELECT COUNT(*) FROM plans WHERE name='Legs B'`); got != 0 {
		t.Fatal("invalid import created a plan")
	}
	if got := e.exerciseNames(); !reflect.DeepEqual(got, exBefore) {
		t.Fatalf("invalid import created exercises: %v", got)
	}
	if got := e.count(`SELECT COUNT(*) FROM plan_items`); got != planItemsBefore {
		t.Fatal("invalid import touched plan items")
	}

	for name, tc := range map[string]struct {
		doc   string
		field string
	}{
		"wrong format":    {`{"format":"csv","version":1,"name":"x","items":[{"exercise":"a","sets":1}]}`, "format"},
		"missing format":  {`{"version":1,"name":"x","items":[{"exercise":"a","sets":1}]}`, "format"},
		"future version":  {`{"format":"workout-plan","version":2,"name":"x","items":[{"exercise":"a","sets":1}]}`, "version"},
		"missing version": {`{"format":"workout-plan","name":"x","items":[{"exercise":"a","sets":1}]}`, "version"},
		"version string":  {`{"format":"workout-plan","version":"1","name":"x","items":[{"exercise":"a","sets":1}]}`, "version"},
		"no items":        {`{"format":"workout-plan","version":1,"name":"x","items":[]}`, "items"},
		"items not array": {`{"format":"workout-plan","version":1,"name":"x","items":{}}`, "items"},
		"item not object": {`{"format":"workout-plan","version":1,"name":"x","items":[3]}`, "items[0]"},
		"no name":         {`{"format":"workout-plan","version":1,"items":[{"exercise":"a","sets":1}]}`, "name"},
		"fractional sets": {`{"format":"workout-plan","version":1,"name":"x","items":[{"exercise":"a","sets":2.5}]}`, "items[0].sets"},
		"not json":        {`{"format":`, ""},
		"array":           {`[]`, ""},
		"null":            {`null`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := e.svc.ImportPlan(ctx(), []byte(tc.doc), "", false)
			requireFields(t, err, tc.field)
		})
	}
	if got := e.count(`SELECT COUNT(*) FROM plans`); got != 1 {
		t.Fatalf("invalid imports created plans: %d", got)
	}
}

func TestPlanImportRejectsAliasDuplicates(t *testing.T) {
	e := newEnv(t)
	res := e.log(batch("", set("bench press", 60, 8)))
	if err := e.svc.AddExerciseAlias(ctx(), res.Sets[0].ExerciseID, "bp"); err != nil {
		t.Fatal(err)
	}
	doc := `{"format":"workout-plan","version":1,"name":"Dup","items":[{"exercise":"bench press","sets":3},{"exercise":"BP","sets":2},{"exercise":"brand new","sets":1}]}`
	_, err := e.svc.ImportPlan(ctx(), []byte(doc), "", false)
	requireFields(t, err, "items[1].exercise")
	if got := e.count(`SELECT COUNT(*) FROM plans`); got != 0 {
		t.Fatal("alias duplicate created a plan")
	}
	if contains(e.exerciseNames(), "brand new") {
		t.Fatal("failed import left a new exercise behind")
	}
	// An alias alone resolves to the canonical exercise.
	ok := `{"format":"workout-plan","version":1,"name":"Alias","items":[{"exercise":"BP","sets":3}]}`
	r, err := e.svc.ImportPlan(ctx(), []byte(ok), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Plan.Items[0].Exercise != "bench press" || len(r.NewExercises) != 0 {
		t.Fatalf("alias not resolved: %+v", r)
	}
}

func TestWorkoutHistorySurvivesPlanEdits(t *testing.T) {
	e := newEnv(t)
	imp, err := e.svc.ImportPlan(ctx(), readFile(t, pushA), "", false)
	if err != nil {
		t.Fatal(err)
	}
	planID := imp.Plan.ID
	w, err := e.svc.StartPlannedSession(ctx(), "2026-03-09", planID)
	if err != nil {
		t.Fatal(err)
	}
	if w.PlanID != planID || w.PlanName != "Push A" {
		t.Fatalf("started workout: %+v", w)
	}
	e.log(batch("2026-03-09", set("bench press", 80, 8), set("bench press", 80, 7), set("lateral raise", 10, 15)))
	before := e.workout("2026-03-09")
	if len(before.Progress) != 5 {
		t.Fatalf("progress should list the 5 planned exercises: %+v", before.Progress)
	}
	// Starting the same date again with the same plan is a no-op; another plan
	// is a conflict.
	if again, err := e.svc.StartPlannedSession(ctx(), "2026-03-09", planID); err != nil || again.ID != w.ID {
		t.Fatalf("restart: %v", err)
	}
	other, err := e.svc.CreatePlan(ctx(), training.Plan{Name: "Other", Items: []training.PlanItem{{Exercise: "squat", TargetSets: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.StartPlannedSession(ctx(), "2026-03-09", other.ID)
	wantErr(t, err, training.ErrConflict)

	replacement := `{"format":"workout-plan","version":1,"name":"Push A","items":[{"exercise":"dips","sets":5,"rep_min":5,"rep_max":5}]}`
	cur, _ := e.svc.GetPlan(ctx(), planID)
	if _, err := e.svc.ReplacePlan(ctx(), planID, []byte(replacement), &cur.Revision); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.UpdatePlan(ctx(), planID, training.PlanPatch{Name: s("Push A (old)")}); err != nil {
		t.Fatal(err)
	}
	check := func(stage string) {
		t.Helper()
		after := e.workout("2026-03-09")
		if after.PlanName != "Push A" {
			t.Fatalf("%s: plan name snapshot changed to %q", stage, after.PlanName)
		}
		if !reflect.DeepEqual(after.Progress, before.Progress) {
			t.Fatalf("%s: prescription changed:\n%+v\n%+v", stage, before.Progress, after.Progress)
		}
		if len(after.Sets) != 3 {
			t.Fatalf("%s: %d sets", stage, len(after.Sets))
		}
		for i := range after.Sets {
			a, b := after.Sets[i], before.Sets[i]
			if a.ID != b.ID || a.Revision != b.Revision || !eqF(a.WeightKG, b.WeightKG) || !eqI(a.Reps, b.Reps) {
				t.Fatalf("%s: set %d changed", stage, i)
			}
		}
	}
	check("after replace+rename")

	cur, _ = e.svc.GetPlan(ctx(), planID)
	stale := cur.Revision - 1
	wantErr(t, e.svc.DeletePlanChecked(ctx(), planID, &stale), training.ErrConflict)
	if err := e.svc.DeletePlanChecked(ctx(), planID, &cur.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GetPlan(ctx(), planID); err == nil {
		t.Fatal("plan not deleted")
	}
	check("after delete")
	if bp, _ := progressFor(e.workout("2026-03-09"), "bench press"); bp.TargetSets != 3 || bp.DoneSets != 2 || !eqF(bp.TargetWeightKG, f(80)) {
		t.Fatalf("bench prescription after delete: %+v", bp)
	}
	if _, err := e.svc.ExportPlan(ctx(), planID); err == nil {
		t.Fatal("export of deleted plan should fail")
	}
}
