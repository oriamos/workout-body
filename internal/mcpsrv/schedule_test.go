package mcpsrv

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The fixed test clock is Tuesday 2026-03-10 in Asia/Jerusalem, so the
// current week is Monday 2026-03-09 .. Sunday 2026-03-15.

func createPlan(t *testing.T, cs *mcp.ClientSession, name string, schedule any) training.Plan {
	t.Helper()
	args := map[string]any{"name": name, "items": []any{map[string]any{"exercise": name + " lift", "target_sets": 3}}}
	if schedule != nil {
		args["schedule"] = schedule
	}
	var out PlanOut
	callOK(t, cs, "create_plan", args, &out)
	return out.Plan
}

func scheduleDay(t *testing.T, v training.ScheduleView, date string) training.ScheduleDay {
	t.Helper()
	for _, d := range v.Days {
		if d.Date == date {
			return d
		}
	}
	t.Fatalf("no day %s in %+v", date, v.Days)
	return training.ScheduleDay{}
}

func statusOf(d training.ScheduleDay, planID int64) string {
	for _, s := range d.Scheduled {
		if s.PlanID == planID {
			return s.Status
		}
	}
	return ""
}

func TestPlanScheduleTools(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		upper := createPlan(t, cs, "Upper A", map[string]any{"type": "weekdays", "days": []any{"Thursday", "mon", "tue"}})
		if upper.Schedule == nil || !reflect.DeepEqual(upper.Schedule.Days, []string{"mon", "tue", "thu"}) {
			t.Fatalf("create_plan schedule = %+v", upper.Schedule)
		}
		lower := createPlan(t, cs, "Lower A", map[string]any{"type": "weekdays", "days": []any{"mon", "tue"}})
		full := createPlan(t, cs, "Full Body", map[string]any{"type": "times_per_week", "times_per_week": 2})
		mobility := createPlan(t, cs, "Mobility", nil)
		if mobility.Schedule != nil {
			t.Fatalf("unscheduled plan has a schedule: %+v", mobility.Schedule)
		}

		var plans PlansOut
		callOK(t, cs, "list_plans", nil, &plans)
		var got PlanOut
		callOK(t, cs, "get_plan", map[string]any{"plan_id": full.ID}, &got)
		if got.Plan.Schedule == nil || got.Plan.Schedule.TimesPerWeek != 2 || len(plans.Plans) != 4 {
			t.Fatalf("get_plan schedule = %+v (plans %d)", got.Plan.Schedule, len(plans.Plans))
		}

		// Monday: Upper A done. Last week's Wednesday: Full Body done. Today:
		// an Upper A workout started with no sets yet.
		callOK(t, cs, "log_sets", map[string]any{"date": "2026-03-09", "plan_id": upper.ID,
			"sets": []any{map[string]any{"exercise": "upper a lift", "weight_kg": 50, "reps": 8}}}, nil)
		callOK(t, cs, "log_sets", map[string]any{"date": "2026-03-04", "plan_id": full.ID,
			"sets": []any{map[string]any{"exercise": "full body lift", "reps": 10}}}, nil)
		callOK(t, cs, "start_session", map[string]any{"plan_id": upper.ID}, nil)

		var view ScheduleOut
		callOK(t, cs, "get_schedule", map[string]any{}, &view)
		v := view.Schedule
		if v.From != "2026-03-09" || v.To != "2026-03-15" || len(v.Days) != 7 || v.Today != "2026-03-10" || v.Timezone != "Asia/Jerusalem" {
			t.Fatalf("default range = %s..%s (%d days) today %s tz %s", v.From, v.To, len(v.Days), v.Today, v.Timezone)
		}
		mon, tue, thu := scheduleDay(t, v, "2026-03-09"), scheduleDay(t, v, "2026-03-10"), scheduleDay(t, v, "2026-03-12")
		if !tue.Today || tue.Weekday != "tue" {
			t.Fatalf("today flag = %+v", tue)
		}
		for _, c := range []struct {
			day  training.ScheduleDay
			plan int64
			want string
		}{
			{mon, upper.ID, "done"}, {mon, lower.ID, "missed"},
			{tue, upper.ID, "started"}, {tue, lower.ID, "due"},
			{thu, upper.ID, "upcoming"}, {thu, lower.ID, ""},
		} {
			if got := statusOf(c.day, c.plan); got != c.want {
				t.Errorf("%s plan %d status = %q, want %q", c.day.Date, c.plan, got, c.want)
			}
		}
		if mon.Workout == nil || mon.Workout.PlanID == nil || *mon.Workout.PlanID != upper.ID || mon.Workout.WorkingSets != 1 {
			t.Fatalf("monday workout ref = %+v", mon.Workout)
		}
		if thu.Workout != nil {
			t.Fatalf("thursday has a workout: %+v", thu.Workout)
		}
		if len(v.Unscheduled) != 1 || v.Unscheduled[0].PlanID != mobility.ID {
			t.Fatalf("unscheduled = %+v", v.Unscheduled)
		}
		if len(v.Weekly) != 1 || v.Weekly[0].PlanID != full.ID || v.Weekly[0].Target != 2 || v.Weekly[0].Completed != 0 || v.Weekly[0].Remaining != 2 {
			t.Fatalf("this week's target = %+v", v.Weekly)
		}

		callOK(t, cs, "get_schedule", map[string]any{"from": "2026-03-02", "to": "2026-03-15"}, &view)
		if w := view.Schedule.Weekly; len(w) != 2 || w[0].WeekStart != "2026-03-02" || w[0].Completed != 1 || w[0].Remaining != 1 ||
			!reflect.DeepEqual(w[0].Dates, []string{"2026-03-04"}) || w[1].Completed != 0 {
			t.Fatalf("weekly tally = %+v", view.Schedule.Weekly)
		}
		if w := getWorkout(t, cs, "2026-03-12"); w.Exists {
			t.Fatal("get_schedule created a workout")
		}
		msg := callErr(t, cs, "get_schedule", map[string]any{"to": "2026-03-15"})
		mustContain(t, msg, "from", "is required when to is given")

		// set_plan_schedule: set, stale revision, invalid, clear.
		var set PlanOut
		callOK(t, cs, "set_plan_schedule", map[string]any{"plan_id": mobility.ID, "schedule": map[string]any{"type": "times_per_week", "times_per_week": 3}, "expected_revision": mobility.Revision}, &set)
		if set.Plan.Schedule == nil || set.Plan.Schedule.TimesPerWeek != 3 || set.Plan.Revision <= mobility.Revision {
			t.Fatalf("set_plan_schedule = %+v", set.Plan)
		}
		msg = callErr(t, cs, "set_plan_schedule", map[string]any{"plan_id": mobility.ID, "schedule": nil, "expected_revision": mobility.Revision})
		mustContain(t, msg, "conflict", "re-read")
		msg = callErr(t, cs, "set_plan_schedule", map[string]any{"plan_id": mobility.ID, "schedule": map[string]any{"type": "weekdays", "days": []any{"mon", "funday", "monday"}}})
		mustContain(t, msg, "validation error", "schedule.days[1]", "funday", "schedule.days[2]", "listed twice")
		msg = callErr(t, cs, "set_plan_schedule", map[string]any{"plan_id": mobility.ID, "schedule": map[string]any{"type": "times_per_week", "times_per_week": 9}})
		mustContain(t, msg, "schedule.times_per_week", "between 1 and 7")
		var cleared PlanOut
		callOK(t, cs, "set_plan_schedule", map[string]any{"plan_id": mobility.ID, "schedule": nil, "expected_revision": set.Plan.Revision}, &cleared)
		if cleared.Plan.Schedule != nil {
			t.Fatalf("null did not clear: %+v", cleared.Plan.Schedule)
		}
		callOK(t, cs, "set_plan_schedule", map[string]any{"plan_id": full.ID}, &cleared)
		if cleared.Plan.Schedule != nil {
			t.Fatalf("omitted schedule did not clear: %+v", cleared.Plan.Schedule)
		}
	})
}

func TestPlanDocumentScheduleRoundTrip(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		doc := map[string]any{
			"format": "workout-plan", "version": 1, "name": "Push Day",
			"schedule": map[string]any{"type": "weekdays", "days": []any{"mon", "thu"}},
			"items":    []any{map[string]any{"exercise": "bench press", "sets": 3}},
		}
		var dry ImportPlanOut
		callOK(t, cs, "import_plan", map[string]any{"plan": doc, "dry_run": true}, &dry)
		if s := dry.Result.Plan.Schedule; !dry.Result.DryRun || s == nil || !reflect.DeepEqual(s.Days, []string{"mon", "thu"}) {
			t.Fatalf("dry run schedule = %+v", dry.Result)
		}
		var imp ImportPlanOut
		callOK(t, cs, "import_plan", map[string]any{"plan": doc}, &imp)
		var exp ExportPlanOut
		callOK(t, cs, "export_plan", map[string]any{"plan_id": imp.Result.Plan.ID}, &exp)
		gotJSON, _ := json.Marshal(exp.Plan)
		wantJSON, _ := json.Marshal(doc)
		var got, want map[string]any
		_ = json.Unmarshal(gotJSON, &got)
		_ = json.Unmarshal(wantJSON, &want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("export round trip:\n got %s\nwant %s", gotJSON, wantJSON)
		}

		bad := map[string]any{"format": "workout-plan", "version": 1, "name": "Bad",
			"schedule": map[string]any{"type": "sometimes"},
			"items":    []any{map[string]any{"exercise": "row", "sets": 3}}}
		msg := callErr(t, cs, "import_plan", map[string]any{"plan": bad, "dry_run": true})
		mustContain(t, msg, "validation error", "schedule.type")

		// Replacing with a document without schedule clears it.
		delete(doc, "schedule")
		var rep ImportPlanOut
		callOK(t, cs, "import_plan", map[string]any{"plan": doc, "on_conflict": "replace"}, &rep)
		if !rep.Result.Replaced || rep.Result.Plan.Schedule != nil {
			t.Fatalf("replace kept the schedule: %+v", rep.Result.Plan)
		}
	})
}
