package integration

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/training-mcp/internal/training"
)

func planDoc(schedule string) []byte {
	s := ""
	if schedule != "" {
		s = `"schedule":` + schedule + `,`
	}
	return []byte(`{"format":"workout-plan","version":1,"name":"Upper A",` + s +
		`"items":[{"exercise":"bench press","sets":3,"rep_min":5,"rep_max":8}]}`)
}

func importPlanDoc(t *testing.T, e *env, doc []byte, onConflict string) training.PlanImportResult {
	t.Helper()
	res, err := e.svc.ImportPlan(ctx(), doc, onConflict, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return res
}

func TestPlanScheduleImportExportRoundTrip(t *testing.T) {
	e := newEnv(t)
	// Full names, odd case, out of order: stored canonically.
	res := importPlanDoc(t, e, planDoc(`{"type":"weekdays","days":["Thursday","MON"]}`), "")
	want := &training.Schedule{Type: "weekdays", Days: []string{"mon", "thu"}}
	if !reflect.DeepEqual(res.Plan.Schedule, want) {
		t.Fatalf("stored schedule: %+v", res.Plan.Schedule)
	}
	doc, err := e.svc.ExportPlan(ctx(), res.Plan.ID)
	if err != nil || !reflect.DeepEqual(doc.Schedule, want) {
		t.Fatalf("export: %+v %v", doc.Schedule, err)
	}
	plans, _ := e.svc.ListPlans(ctx())
	if len(plans) != 1 || !reflect.DeepEqual(plans[0].Schedule, want) {
		t.Fatalf("the plan list must carry the schedule: %+v", plans)
	}
	// export -> import(replace) is idempotent, schedule included
	again, _ := jsonOf(doc)
	r2 := importPlanDoc(t, e, again, "replace")
	if !reflect.DeepEqual(r2.Plan.Schedule, want) || !r2.Replaced {
		t.Fatalf("round trip: %+v", r2)
	}

	freq := importPlanDoc(t, e, []byte(strings.Replace(string(planDoc(`{"type":"times_per_week","times_per_week":3}`)), "Upper A", "Lower B", 1)), "")
	if got := freq.Plan.Schedule; got == nil || got.Type != "times_per_week" || got.TimesPerWeek != 3 || len(got.Days) != 0 {
		t.Fatalf("frequency schedule: %+v", got)
	}
	none := importPlanDoc(t, e, []byte(strings.Replace(string(planDoc("")), "Upper A", "Unscheduled C", 1)), "")
	if none.Plan.Schedule != nil {
		t.Fatalf("no schedule means null: %+v", none.Plan.Schedule)
	}
}

func TestPlanScheduleValidationReportsEverythingAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	for name, tc := range map[string]struct{ schedule, field string }{
		"bad day":          {`{"type":"weekdays","days":["mon","funday"]}`, "schedule.days[1]"},
		"duplicate day":    {`{"type":"weekdays","days":["mon","Monday"]}`, "schedule.days[1]"},
		"no days":          {`{"type":"weekdays","days":[]}`, "schedule.days"},
		"missing days":     {`{"type":"weekdays"}`, "schedule.days"},
		"times zero":       {`{"type":"times_per_week","times_per_week":0}`, "schedule.times_per_week"},
		"times eight":      {`{"type":"times_per_week","times_per_week":8}`, "schedule.times_per_week"},
		"days with times":  {`{"type":"times_per_week","times_per_week":2,"days":["mon"]}`, "schedule.days"},
		"times with days":  {`{"type":"weekdays","days":["mon"],"times_per_week":2}`, "schedule.times_per_week"},
		"unknown type":     {`{"type":"monthly"}`, "schedule.type"},
		"unknown field":    {`{"type":"weekdays","days":["mon"],"hour":7}`, "schedule.hour"},
		"wrong type days":  {`{"type":"weekdays","days":"mon"}`, "schedule.days"},
		"null schedule":    {`null`, "schedule"},
		"not an object":    {`"mon"`, "schedule"},
		"fractional times": {`{"type":"times_per_week","times_per_week":2.5}`, "schedule.times_per_week"},
	} {
		_, err := e.svc.ImportPlan(ctx(), planDoc(tc.schedule), "", false)
		if !errors.Is(err, training.ErrValidation) {
			t.Errorf("%s: want a validation error, got %v", name, err)
			continue
		}
		found := false
		for _, i := range training.IssuesOf(err) {
			if i.Field == tc.field {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: want an issue on %s, got %v", name, tc.field, training.IssuesOf(err))
		}
	}
	if plans, _ := e.svc.ListPlans(ctx()); len(plans) != 0 {
		t.Fatalf("invalid imports must leave no plan behind: %+v", plans)
	}
}

func TestReplaceImportReplacesOrClearsTheSchedule(t *testing.T) {
	e := newEnv(t)
	first := importPlanDoc(t, e, planDoc(`{"type":"weekdays","days":["mon"]}`), "")
	r := importPlanDoc(t, e, planDoc(`{"type":"times_per_week","times_per_week":2}`), "replace")
	if r.Plan.ID != first.Plan.ID || r.Plan.Schedule.Type != "times_per_week" || r.Plan.Revision != first.Plan.Revision+1 {
		t.Fatalf("replace must swap the schedule in place: %+v", r.Plan)
	}
	r = importPlanDoc(t, e, planDoc(""), "replace")
	if r.Plan.Schedule != nil {
		t.Fatalf("a document without a schedule replaces it with none: %+v", r.Plan.Schedule)
	}
}

func TestSetPlanScheduleSetsClearsAndIsRevisionGuarded(t *testing.T) {
	e := newEnv(t)
	p := importPlanDoc(t, e, planDoc(""), "").Plan
	got, err := e.svc.SetPlanSchedule(ctx(), p.ID, &training.Schedule{Type: "weekdays", Days: []string{"wed", "mon"}}, &p.Revision)
	if err != nil || !reflect.DeepEqual(got.Schedule.Days, []string{"mon", "wed"}) || got.Revision != p.Revision+1 {
		t.Fatalf("set: %+v %v", got, err)
	}
	stale := p.Revision
	if _, err := e.svc.SetPlanSchedule(ctx(), p.ID, nil, &stale); !errors.Is(err, training.ErrStale) {
		t.Fatalf("a stale revision must be refused, got %v", err)
	}
	if cur, _ := e.svc.GetPlan(ctx(), p.ID); cur.Schedule == nil {
		t.Fatal("a refused change must leave the schedule alone")
	}
	cleared, err := e.svc.SetPlanSchedule(ctx(), p.ID, nil, nil)
	if err != nil || cleared.Schedule != nil {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
	if _, err := e.svc.SetPlanSchedule(ctx(), p.ID, &training.Schedule{Type: "weekdays", Days: []string{"x"}}, nil); !errors.Is(err, training.ErrValidation) {
		t.Fatalf("invalid schedule: %v", err)
	}
	if _, err := e.svc.SetPlanSchedule(ctx(), 999, nil, nil); !errors.Is(err, training.ErrNotFound) {
		t.Fatalf("unknown plan: %v", err)
	}
}

// Monday 2026-03-09 is "today" in Jerusalem even though it is still Sunday in UTC.
func TestScheduleViewUsesTheLocalWeekdayAndReportsStatus(t *testing.T) {
	e := newEnv(t)
	e.now = time.Date(2026, 3, 8, 22, 30, 0, 0, time.UTC) // 00:30 Monday in Jerusalem
	upper := importPlanDoc(t, e, planDoc(`{"type":"weekdays","days":["mon","thu"]}`), "").Plan
	lower := importPlanDoc(t, e, []byte(strings.Replace(string(planDoc(`{"type":"times_per_week","times_per_week":2}`)), "Upper A", "Lower B", 1)), "").Plan
	free := importPlanDoc(t, e, []byte(strings.Replace(string(planDoc("")), "Upper A", "Mobility", 1)), "").Plan

	v, err := e.svc.GetSchedule(ctx(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Today != "2026-03-09" || v.From != "2026-03-09" || v.To != "2026-03-15" || v.Timezone != "Asia/Jerusalem" || len(v.Days) != 7 {
		t.Fatalf("view header: %+v", v)
	}
	mon, thu, tue := v.Days[0], v.Days[3], v.Days[1]
	if mon.Weekday != "mon" || !mon.Today || len(mon.Scheduled) != 1 || mon.Scheduled[0].PlanID != upper.ID || mon.Scheduled[0].Status != "due" {
		t.Fatalf("monday (today) must show the plan as due: %+v", mon)
	}
	if thu.Weekday != "thu" || len(thu.Scheduled) != 1 || thu.Scheduled[0].Status != "upcoming" || len(tue.Scheduled) != 0 {
		t.Fatalf("thursday / tuesday: %+v %+v", thu, tue)
	}
	if len(v.Unscheduled) != 1 || v.Unscheduled[0].PlanID != free.ID {
		t.Fatalf("unscheduled plans: %+v", v.Unscheduled)
	}

	// Start today's planned workout: started, then done once a working set exists.
	if _, err := e.svc.StartPlannedSession(ctx(), "", upper.ID); err != nil {
		t.Fatal(err)
	}
	v, _ = e.svc.GetSchedule(ctx(), "", "")
	if st := v.Days[0].Scheduled[0].Status; st != "started" || v.Days[0].Workout == nil || v.Days[0].Workout.PlanID == nil {
		t.Fatalf("a workout following the plan with no sets is 'started': %+v", v.Days[0])
	}
	if _, err := e.svc.LogSets(ctx(), batch("", set("bench press", 60, 8))); err != nil {
		t.Fatal(err)
	}
	// Only warm-ups would not count: add one more day to check below.
	v, _ = e.svc.GetSchedule(ctx(), "", "")
	if v.Days[0].Scheduled[0].Status != "done" || v.Days[0].Workout.WorkingSets != 1 {
		t.Fatalf("done: %+v", v.Days[0])
	}

	// A frequency plan is tallied per Monday-start week.
	e.now = time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)
	if _, err := e.svc.StartPlannedSession(ctx(), "", lower.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.LogSets(ctx(), batch("", set("squat", 100, 5))); err != nil {
		t.Fatal(err)
	}
	v, _ = e.svc.GetSchedule(ctx(), "", "")
	if len(v.Weekly) != 1 {
		t.Fatalf("weekly: %+v", v.Weekly)
	}
	w := v.Weekly[0]
	if w.PlanID != lower.ID || w.Target != 2 || w.Completed != 1 || w.Remaining != 1 || !reflect.DeepEqual(w.Dates, []string{"2026-03-11"}) {
		t.Fatalf("weekly tally: %+v", w)
	}

	// A past week: Thursday's scheduled plan was never done.
	v, _ = e.svc.GetSchedule(ctx(), "2026-03-09", "2026-03-15")
	if st := v.Days[3].Scheduled[0].Status; st != "upcoming" {
		t.Fatalf("future day: %s", st)
	}
	e.now = time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	v, _ = e.svc.GetSchedule(ctx(), "2026-03-09", "2026-03-15")
	if st := v.Days[3].Scheduled[0].Status; st != "missed" {
		t.Fatalf("a passed day without the plan is 'missed', got %s", st)
	}
	if st := v.Days[0].Scheduled[0].Status; st != "done" {
		t.Fatalf("history keeps its status: %s", st)
	}
}

func TestWarmupOnlyWorkoutIsNotDone(t *testing.T) {
	e := newEnv(t)
	e.now = time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	p := importPlanDoc(t, e, planDoc(`{"type":"weekdays","days":["mon"]}`), "").Plan
	if _, err := e.svc.StartPlannedSession(ctx(), "", p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.LogSets(ctx(), training.BatchInput{Sets: []training.AddSetInput{{Exercise: "bench press", WeightKG: f(20), Reps: n(10), SetType: training.SetTypeWarmup}}}); err != nil {
		t.Fatal(err)
	}
	v, _ := e.svc.GetSchedule(ctx(), "", "")
	if st := v.Days[0].Scheduled[0].Status; st != "started" {
		t.Fatalf("a warm-up alone is not completed work, got %s", st)
	}
}

func TestChangingAScheduleNeverTouchesHistoryOrWorkouts(t *testing.T) {
	e := newEnv(t)
	e.now = time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	p := importPlanDoc(t, e, planDoc(`{"type":"weekdays","days":["mon"]}`), "").Plan
	if _, err := e.svc.StartPlannedSession(ctx(), "", p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.LogSets(ctx(), batch("", set("bench press", 60, 8))); err != nil {
		t.Fatal(err)
	}
	before, _ := e.svc.GetWorkout(ctx(), "")
	if _, err := e.svc.SetPlanSchedule(ctx(), p.ID, &training.Schedule{Type: "weekdays", Days: []string{"fri"}}, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := e.svc.GetWorkout(ctx(), "")
	if after.Revision != before.Revision || len(after.Sets) != 1 || after.PlanID != p.ID || len(after.Progress) != 1 {
		t.Fatalf("a schedule change must not alter existing workouts: %+v -> %+v", before, after)
	}
}

func TestScheduleRangeValidation(t *testing.T) {
	e := newEnv(t)
	for name, tc := range map[string]struct{ from, to string }{
		"bad from": {"nope", ""},
		"to only":  {"", "2026-03-09"},
		"inverted": {"2026-03-10", "2026-03-09"},
		"too long": {"2026-01-01", "2026-12-31"},
	} {
		if _, err := e.svc.GetSchedule(ctx(), tc.from, tc.to); !errors.Is(err, training.ErrValidation) {
			t.Errorf("%s: want validation error, got %v", name, err)
		}
	}
}

func jsonOf(doc training.PlanDocument) ([]byte, error) { return json.Marshal(doc) }
