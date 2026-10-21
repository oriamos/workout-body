package apisrv

import (
	"encoding/json"
	"fmt"
	"testing"
)

func planWith(name string, schedule any) map[string]any {
	p := samplePlan(name)
	if schedule != nil {
		p["schedule"] = schedule
	}
	return p
}

func schedOf(t *testing.T, plan map[string]any) string {
	t.Helper()
	b, _ := json.Marshal(plan["schedule"])
	return string(b)
}

func TestPlanScheduleRoundTripAndPut(t *testing.T) {
	e := newEnv(t)
	res := expect(t, e.do("POST", "/api/v1/plans", planWith("Upper", map[string]any{"type": "weekdays", "days": []any{"Thursday", "mon"}}), nil), 201)
	plan := res["plan"].(map[string]any)
	id := num(plan["id"])
	path := fmt.Sprintf("/api/v1/plans/%d", id)
	if got := schedOf(t, plan); got != `{"days":["mon","thu"],"type":"weekdays"}` {
		t.Fatalf("schedule = %s", got)
	}
	if l := list(expect(t, e.do("GET", "/api/v1/plans", nil, nil), 200), "plans"); schedOf(t, l[0].(map[string]any)) != `{"days":["mon","thu"],"type":"weekdays"}` {
		t.Fatalf("list = %v", l)
	}

	// Export carries it; replacing with a document without schedule clears it.
	doc := expect(t, e.do("GET", path+"/export", nil, nil), 200)
	if schedOf(t, doc) != `{"days":["mon","thu"],"type":"weekdays"}` {
		t.Fatalf("export = %v", doc)
	}
	delete(doc, "schedule")
	r := e.do("POST", "/api/v1/plans/import?on_conflict=replace", doc, nil)
	if p := expect(t, r, 200)["plan"].(map[string]any); p["schedule"] != nil {
		t.Fatalf("replace without schedule kept it: %v", p)
	}
	// PUT /plans/{id} with a schedule in the document.
	r = e.do("PUT", path, planWith("Upper", map[string]any{"type": "times_per_week", "times_per_week": 3}), h("If-Match", r.etag()))
	if p := expect(t, r, 200); schedOf(t, p) != `{"times_per_week":3,"type":"times_per_week"}` {
		t.Fatalf("put doc = %v", p)
	}
	tag := r.etag()

	// PUT /plans/{id}/schedule.
	sp := path + "/schedule"
	body := map[string]any{"schedule": map[string]any{"type": "weekdays", "days": []any{"sat"}}}
	expectErr(t, e.do("PUT", sp, body, nil), 428, "precondition_required")
	expectErr(t, e.do("PUT", sp, body, h("If-Match", `"1"`)), 412, "precondition_failed")
	r = e.do("PUT", sp, body, h("If-Match", tag))
	p := expect(t, r, 200)
	if schedOf(t, p) != `{"days":["sat"],"type":"weekdays"}` || r.etag() == tag || r.etag() != quoted(num(p["revision"])) {
		t.Fatalf("set = %v etag %q", p, r.etag())
	}
	expectErr(t, e.do("PUT", sp, body, h("If-Match", tag)), 412, "precondition_failed")
	r = e.do("PUT", sp, `{"schedule":null}`, h("If-Match", r.etag()))
	if p := expect(t, r, 200); p["schedule"] != nil {
		t.Fatalf("clear = %v", p)
	}
	expectErr(t, e.do("PUT", "/api/v1/plans/999/schedule", body, h("If-Match", "*")), 404, "not_found")

	star := h("If-Match", "*")
	for _, tc := range []struct{ body, field string }{
		{`{"schedule":{"type":"weekdays","days":["mon","funday"]}}`, "schedule.days[1]"},
		{`{"schedule":{"type":"weekdays","days":["mon","Monday"]}}`, "schedule.days[1]"},
		{`{"schedule":{"type":"weekdays","days":[]}}`, "schedule.days"},
		{`{"schedule":{"type":"times_per_week","times_per_week":0}}`, "schedule.times_per_week"},
		{`{"schedule":{"type":"times_per_week","times_per_week":8}}`, "schedule.times_per_week"},
		{`{"schedule":{"type":"weekdays","days":["mon"],"times_per_week":2}}`, "schedule.times_per_week"},
		{`{"schedule":{"type":"times_per_week","times_per_week":2,"days":["mon"]}}`, "schedule.days"},
		{`{"schedule":{"type":"daily"}}`, "schedule.type"},
		{`{"schedule":{"type":"weekdays","days":["mon"],"colour":"red"}}`, "schedule.colour"},
		{`{"schedule":{"type":"weekdays","days":"mon"}}`, "schedule.days"},
		{`{"schedule":{"type":"times_per_week","times_per_week":"3"}}`, "schedule.times_per_week"},
		{`{"schedule":"weekly"}`, "schedule"},
		{`{"schedule":null,"extra":1}`, "extra"},
		{`{}`, "schedule"},
	} {
		hasIssue(t, expectErr(t, e.do("PUT", sp, tc.body, star), 400, "validation_error"), tc.field)
	}
	expectErr(t, e.do("PUT", sp, `{"schedule":null} 1`, star), 400, "bad_request")

	// The same rules inside a plan document.
	for _, tc := range []struct {
		sched any
		field string
	}{
		{map[string]any{"type": "weekdays", "days": []any{"xyz"}}, "schedule.days[0]"},
		{map[string]any{"type": "weekdays", "days": []any{"mon"}, "x": 1}, "schedule.x"},
		{map[string]any{"type": "times_per_week", "times_per_week": 9}, "schedule.times_per_week"},
	} {
		hasIssue(t, expectErr(t, e.do("POST", "/api/v1/plans", planWith("Bad", tc.sched), nil), 400, "validation_error"), tc.field)
	}
	nullDoc := samplePlan("Bad")
	nullDoc["schedule"] = nil
	hasIssue(t, expectErr(t, e.do("POST", "/api/v1/plans", nullDoc, nil), 400, "validation_error"), "schedule")
}

func dayOf(t *testing.T, v map[string]any, date string) map[string]any {
	t.Helper()
	for _, d := range list(v, "days") {
		if d.(map[string]any)["date"] == date {
			return d.(map[string]any)
		}
	}
	t.Fatalf("no day %s in %v", date, v["days"])
	return nil
}

func statusOn(t *testing.T, v map[string]any, date string) string {
	t.Helper()
	sc := list(dayOf(t, v, date), "scheduled")
	if len(sc) != 1 {
		t.Fatalf("%s scheduled = %v", date, sc)
	}
	return sc[0].(map[string]any)["status"].(string)
}

// Today is Tuesday 2026-03-10 in the test clock.
func TestScheduleView(t *testing.T) {
	e := newEnv(t)
	planID := func(m map[string]any) int { return num(m["plan"].(map[string]any)["id"]) }
	a := planID(expect(t, e.do("POST", "/api/v1/plans", planWith("A", map[string]any{"type": "weekdays", "days": []any{"mon", "tue", "thu"}}), nil), 201))
	b := planID(expect(t, e.do("POST", "/api/v1/plans", planWith("B", map[string]any{"type": "times_per_week", "times_per_week": 2}), nil), 201))
	expect(t, e.do("POST", "/api/v1/plans", planWith("C", nil), nil), 201)

	set := map[string]any{"sets": []any{map[string]any{"exercise": "bench press", "weight_kg": 80, "reps": 6}}}
	warmup := map[string]any{"sets": []any{map[string]any{"exercise": "bench press", "weight_kg": 40, "reps": 10, "set_type": "warmup"}}}
	// Monday: A done.
	expect(t, e.do("POST", "/api/v1/workouts/2026-03-09/plan", map[string]any{"plan_id": a}, nil), 200)
	expect(t, e.do("POST", "/api/v1/workouts/2026-03-09/sets", set, nil), 201)
	// Last week: B done once (Wednesday), nothing for A.
	bSets := map[string]any{"plan_id": b, "sets": set["sets"]}
	expect(t, e.do("POST", "/api/v1/workouts/2026-03-04/sets", bSets, nil), 201)

	v := expect(t, e.do("GET", "/api/v1/schedule", nil, nil), 200)
	if v["from"] != "2026-03-09" || v["to"] != "2026-03-15" || v["today"] != "2026-03-10" || v["timezone"] != "Asia/Jerusalem" || len(list(v, "days")) != 7 {
		t.Fatalf("default view = %v", v)
	}
	if statusOn(t, v, "2026-03-09") != "done" || statusOn(t, v, "2026-03-10") != "due" || statusOn(t, v, "2026-03-12") != "upcoming" {
		t.Fatalf("statuses: %v", v["days"])
	}
	if d := dayOf(t, v, "2026-03-10"); d["today"] != true || d["weekday"] != "tue" || d["workout"] != nil {
		t.Fatalf("today = %v", d)
	}
	mon := dayOf(t, v, "2026-03-09")["workout"].(map[string]any)
	if num(mon["plan_id"]) != a || num(mon["working_sets"]) != 1 {
		t.Fatalf("monday workout = %v", mon)
	}
	if len(list(dayOf(t, v, "2026-03-11"), "scheduled")) != 0 {
		t.Fatal("wednesday should have nothing scheduled")
	}
	un := list(v, "unscheduled_plans")
	if len(un) != 1 || un[0].(map[string]any)["name"] != "C" {
		t.Fatalf("unscheduled = %v", un)
	}
	wt := list(v, "weekly_targets")
	if len(wt) != 1 || num(wt[0].(map[string]any)["completed"]) != 0 || num(wt[0].(map[string]any)["remaining"]) != 2 {
		t.Fatalf("weekly = %v", wt)
	}

	// Today: following the plan starts it; a warm-up alone does not finish it.
	expect(t, e.do("POST", "/api/v1/workouts/today/plan", map[string]any{"plan_id": a}, nil), 200)
	expect(t, e.do("POST", "/api/v1/workouts/today/sets", warmup, nil), 201)
	if s := statusOn(t, expect(t, e.do("GET", "/api/v1/schedule", nil, nil), 200), "2026-03-10"); s != "started" {
		t.Fatalf("today after following = %s", s)
	}
	expect(t, e.do("POST", "/api/v1/workouts/today/sets", set, nil), 201)
	if s := statusOn(t, expect(t, e.do("GET", "/api/v1/schedule", nil, nil), 200), "2026-03-10"); s != "done" {
		t.Fatalf("today after a working set = %s", s)
	}

	// Explicit range including last week.
	v = expect(t, e.do("GET", "/api/v1/schedule?from=2026-03-02&to=today", nil, nil), 200)
	if len(list(v, "days")) != 9 {
		t.Fatalf("days = %d", len(list(v, "days")))
	}
	for _, d := range []string{"2026-03-02", "2026-03-03", "2026-03-05"} {
		if s := statusOn(t, v, d); s != "missed" {
			t.Fatalf("%s = %s", d, s)
		}
	}
	wt = list(v, "weekly_targets")
	if len(wt) != 2 {
		t.Fatalf("weekly = %v", wt)
	}
	first := wt[0].(map[string]any)
	if first["week_start"] != "2026-03-02" || num(first["completed"]) != 1 || num(first["remaining"]) != 1 || fmt.Sprint(first["dates"]) != "[2026-03-04]" {
		t.Fatalf("last week target = %v", first)
	}
	if v := expect(t, e.do("GET", "/api/v1/schedule?from=2026-03-16", nil, nil), 200); v["to"] != "2026-03-22" {
		t.Fatalf("from-only view = %v", v)
	}

	// Bad ranges.
	expectErr(t, e.do("GET", "/api/v1/schedule?from=2026-03-10&to=2026-03-01", nil, nil), 400, "validation_error")
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/schedule?from=2026-01-01&to=2026-03-04", nil, nil), 400, "validation_error"), "to")
	expect(t, e.do("GET", "/api/v1/schedule?from=2026-01-02&to=2026-03-04", nil, nil), 200)
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/schedule?to=2026-03-04", nil, nil), 400, "validation_error"), "from")
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/schedule?from=soon", nil, nil), 400, "validation_error"), "from")

	// Read-only: viewing future days never creates workouts.
	expectErr(t, e.do("GET", "/api/v1/workouts/2026-03-12", nil, nil), 404, "not_found")
	if l := expect(t, e.do("GET", "/api/v1/workouts?limit=100", nil, nil), 200); len(list(l, "workouts")) != 3 {
		t.Fatalf("workouts = %v", l)
	}
}
