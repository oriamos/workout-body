package e2e

import (
	"strconv"
	"testing"
)

// Plans carry a fixed schedule; both adapters read and write it, and the
// schedule view reflects what was actually done.
func TestPlanScheduleThroughRealBinary(t *testing.T) {
	srv := start(t, "")
	cs := srv.mcpClient()
	today := srv.do("GET", "/api/v1/config", nil).Map(t)["today"].(string)

	// Import with a schedule over HTTP; read it back over MCP.
	doc := []byte(`{"format":"workout-plan","version":1,"name":"Upper A",
		"schedule":{"type":"weekdays","days":["Thursday","mon"]},
		"items":[{"exercise":"bench press","sets":3,"rep_min":5,"rep_max":8}]}`)
	imp := srv.do("POST", "/api/v1/plans/import", doc)
	if imp.Status != 201 {
		t.Fatalf("import: %d %s", imp.Status, imp.Body)
	}
	planID := int(num(get(imp.Map(t), "plan", "id")))
	sched, _ := get(tool(t, cs, "get_plan", map[string]any{"plan_id": planID}), "plan", "schedule").(map[string]any)
	if sched["type"] != "weekdays" || len(list(sched["days"])) != 2 || list(sched["days"])[0] != "mon" || list(sched["days"])[1] != "thu" {
		t.Fatalf("schedule must be stored canonically (mon, thu): %v", sched)
	}

	// A second plan with a weekly frequency, created over MCP.
	freq := tool(t, cs, "import_plan", map[string]any{"plan": map[string]any{
		"format": "workout-plan", "version": 1, "name": "Lower B",
		"schedule": map[string]any{"type": "times_per_week", "times_per_week": 2},
		"items":    []any{map[string]any{"exercise": "squat", "sets": 3}},
	}})
	freqID := int(num(get(freq, "result", "plan", "id")))
	if freqID == 0 {
		freqID = int(num(get(freq, "plan", "id")))
	}

	// Invalid schedules are rejected with paths and change nothing.
	bad := srv.do("POST", "/api/v1/plans/import", []byte(`{"format":"workout-plan","version":1,"name":"Bad","schedule":{"type":"weekdays","days":["mon","funday"]},"items":[{"exercise":"a","sets":3}]}`))
	if bad.Status != 400 || !containsField(bad.Map(t), "schedule.days[1]") {
		t.Fatalf("invalid schedule: %d %s", bad.Status, bad.Body)
	}

	// The view over HTTP and over MCP agree.
	view := srv.do("GET", "/api/v1/schedule", nil).Map(t)
	if view["today"] != today || len(list(view["days"])) != 7 {
		t.Fatalf("schedule view: %v", view)
	}
	mview := tool(t, cs, "get_schedule", map[string]any{})
	if get(mview, "schedule", "today") != today && mview["today"] != today {
		t.Fatalf("mcp schedule view: %v", mview)
	}
	for _, d := range list(view["days"]) {
		day := d.(map[string]any)
		sc := list(day["scheduled"])
		if (day["weekday"] == "mon" || day["weekday"] == "thu") != (len(sc) == 1) {
			t.Fatalf("Upper A must appear on exactly Monday and Thursday: %v", day)
		}
		if len(sc) == 1 && sc[0].(map[string]any)["status"] == "done" {
			t.Fatalf("nothing is done yet: %v", day)
		}
	}
	if wt := list(view["weekly_targets"]); len(wt) != 1 || num(wt[0].(map[string]any)["completed"]) != 0 || num(wt[0].(map[string]any)["remaining"]) != 2 {
		t.Fatalf("weekly target before training: %v", wt)
	}

	// Train under the frequency plan today: the weekly tally moves.
	tool(t, cs, "log_sets", map[string]any{"plan_id": freqID, "sets": []any{map[string]any{"exercise": "squat", "weight_kg": 100, "reps": 5}}})
	view = srv.do("GET", "/api/v1/schedule", nil).Map(t)
	wt := list(view["weekly_targets"])[0].(map[string]any)
	if num(wt["completed"]) != 1 || num(wt["remaining"]) != 1 || list(wt["dates"])[0] != today {
		t.Fatalf("weekly tally after one workout: %v", wt)
	}

	// Changing the schedule over HTTP needs If-Match; MCP sees the new value.
	path := "/api/v1/plans/" + strconv.Itoa(planID) + "/schedule"
	etag := srv.do("GET", "/api/v1/plans/"+strconv.Itoa(planID), nil).Header.Get("ETag")
	body := map[string]any{"schedule": map[string]any{"type": "weekdays", "days": []any{"fri"}}}
	if r := srv.do("PUT", path, body, "If-Match", "-"); r.Status != 428 {
		t.Fatalf("PUT schedule without If-Match must be 428, got %d %s", r.Status, r.Body)
	}
	if r := srv.do("PUT", path, body, "If-Match", `"999"`); r.Status != 412 {
		t.Fatalf("stale If-Match must be 412, got %d", r.Status)
	}
	if r := srv.do("PUT", path, body, "If-Match", etag); r.Status != 200 {
		t.Fatalf("set schedule: %d %s", r.Status, r.Body)
	}
	got, _ := get(tool(t, cs, "get_plan", map[string]any{"plan_id": planID}), "plan", "schedule").(map[string]any)
	if d := list(got["days"]); len(d) != 1 || d[0] != "fri" {
		t.Fatalf("mcp must read the schedule changed over HTTP: %v", got)
	}
	// Clear it over MCP; the HTTP view then lists the plan as unscheduled.
	tool(t, cs, "set_plan_schedule", map[string]any{"plan_id": planID})
	view = srv.do("GET", "/api/v1/schedule", nil).Map(t)
	found := false
	for _, u := range list(view["unscheduled_plans"]) {
		if int(num(u.(map[string]any)["plan_id"])) == planID {
			found = true
		}
	}
	if !found {
		t.Fatalf("a cleared plan must be listed as unscheduled: %v", view["unscheduled_plans"])
	}

	// Everything survives a restart.
	srv.stop()
	srv2 := start(t, srv.db)
	wt2 := list(srv2.do("GET", "/api/v1/schedule", nil).Map(t)["weekly_targets"])
	if len(wt2) != 1 || num(wt2[0].(map[string]any)["completed"]) != 1 {
		t.Fatalf("after restart: %v", wt2)
	}
}

func containsField(errBody map[string]any, field string) bool {
	for _, i := range list(get(errBody, "error", "issues")) {
		if i.(map[string]any)["field"] == field {
			return true
		}
	}
	return false
}
