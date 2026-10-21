package apisrv

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

func TestBearerWiring(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct {
		name, auth string
		want       int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "Bearer nope", http.StatusUnauthorized},
		{"valid", "Bearer " + testToken, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := e.do("GET", "/api/v1/config", nil, h("Authorization", tc.auth))
			if r.status != tc.want {
				t.Fatalf("status %d, want %d", r.status, tc.want)
			}
		})
	}
}

func TestConfigAndSpec(t *testing.T) {
	e := newEnv(t)
	m := expect(t, e.do("GET", "/api/v1/config", nil, nil), 200)
	if m["timezone"] != "Asia/Jerusalem" || m["today"] != "2026-03-10" || m["api_version"] != "v1" {
		t.Fatalf("config = %v", m)
	}
	r := e.do("GET", "/api/v1/openapi.yaml", nil, nil)
	if r.status != 200 || !strings.HasPrefix(r.header.Get("Content-Type"), "application/yaml") || !strings.HasPrefix(string(r.body), "openapi: 3.1") {
		t.Fatalf("spec: %d %s", r.status, r.header.Get("Content-Type"))
	}
}

func TestUnknownPathAndMethod(t *testing.T) {
	e := newEnv(t)
	expectErr(t, e.do("GET", "/api/v1/nope", nil, nil), 404, "not_found")
	r := e.do("DELETE", "/api/v1/config", nil, nil)
	expectErr(t, r, 405, "method_not_allowed")
	if r.header.Get("Allow") != "GET" {
		t.Fatalf("Allow = %q", r.header.Get("Allow"))
	}
	r = e.do("PUT", "/api/v1/sets/1", map[string]any{}, nil)
	expectErr(t, r, 405, "method_not_allowed")
	if r.header.Get("Allow") != "GET, PATCH, DELETE" {
		t.Fatalf("Allow = %q", r.header.Get("Allow"))
	}
}

func TestWorkoutLogAndRead(t *testing.T) {
	e := newEnv(t)
	expectErr(t, e.do("GET", "/api/v1/workouts/today", nil, nil), 404, "not_found")

	body := map[string]any{
		"prescriptions": []any{map[string]any{"exercise": "Bench Press", "target_sets": 3, "rep_min": 6, "rep_max": 8, "target_rpe": 8}},
		"sets": []any{
			map[string]any{"exercise": "bench press", "weight_kg": 80, "reps": 8, "rpe": 7.5},
			map[string]any{"exercise": "Bench Press", "weight_kg": nil, "reps": 7},
			map[string]any{"exercise": "bench press", "weight_kg": 40, "reps": 10, "set_type": "warmup"},
		},
	}
	r := e.do("POST", "/api/v1/workouts/today/sets", body, nil)
	m := expect(t, r, 201)
	wk := m["workout"].(map[string]any)
	if m["created"] != true || m["replayed"] != false || wk["date"] != "2026-03-10" {
		t.Fatalf("result = %v", m)
	}
	if r.etag() != quoted(num(wk["revision"])) {
		t.Fatalf("ETag %q vs revision %v", r.etag(), wk["revision"])
	}
	sets := list(m, "sets")
	if len(sets) != 3 || sets[1].(map[string]any)["weight_kg"] != nil {
		t.Fatalf("sets = %v", sets)
	}
	if len(list(m, "new_exercises")) != 1 {
		t.Fatalf("new_exercises = %v", m["new_exercises"])
	}

	// Read back by explicit date: stable ids, set numbers and progress.
	r = e.do("GET", "/api/v1/workouts/2026-03-10", nil, nil)
	w := expect(t, r, 200)
	if r.etag() != quoted(num(w["revision"])) {
		t.Fatalf("ETag %q", r.etag())
	}
	ws := list(w, "sets")
	for i, s := range ws {
		if num(s.(map[string]any)["set_number"]) != i+1 {
			t.Fatalf("set %d set_number = %v", i, s.(map[string]any)["set_number"])
		}
		if num(s.(map[string]any)["id"]) != num(sets[i].(map[string]any)["id"]) {
			t.Fatal("ids not stable between write and read")
		}
	}
	prog := list(w, "progress")
	if len(prog) != 1 {
		t.Fatalf("progress = %v", prog)
	}
	p := prog[0].(map[string]any)
	if num(p["target_sets"]) != 3 || num(p["done_sets"]) != 2 || num(p["warmup_sets"]) != 1 {
		t.Fatalf("progress item = %v", p)
	}
	if w["plan_id"] != nil || w["plan_name"] != nil {
		t.Fatalf("plan fields should be null: %v", w)
	}

	// Summaries.
	l := expect(t, e.do("GET", "/api/v1/workouts?from=2026-03-01&to=today&limit=5", nil, nil), 200)
	if ws := list(l, "workouts"); len(ws) != 1 || num(ws[0].(map[string]any)["set_count"]) != 3 {
		t.Fatalf("list = %v", l)
	}
	l = expect(t, e.do("GET", "/api/v1/workouts?to=2026-03-09", nil, nil), 200)
	if len(list(l, "workouts")) != 0 {
		t.Fatalf("list = %v", l)
	}
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/workouts?limit=many", nil, nil), 400, "validation_error"), "limit")
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/workouts?from=03-01-2026", nil, nil), 400, "validation_error"), "from")
	expectErr(t, e.do("GET", "/api/v1/workouts?limit=1000", nil, nil), 400, "validation_error")
	expectErr(t, e.do("GET", "/api/v1/workouts?from=2026-03-10&to=2026-03-01", nil, nil), 400, "validation_error")
}

func TestLogSetsValidation(t *testing.T) {
	e := newEnv(t)
	post := func(date string, body any, hdr map[string]string) resp {
		return e.do("POST", "/api/v1/workouts/"+date+"/sets", body, hdr)
	}
	// Every problem is reported with its path, nothing is written.
	er := expectErr(t, post("today", map[string]any{"sets": []any{
		map[string]any{"exercise": "squat", "reps": 0},
		map[string]any{"exercise": "squat", "rpe": 11, "set_type": "mega"},
		map[string]any{"weight_kg": 10},
	}}, nil), 400, "validation_error")
	for _, f := range []string{"sets[0].reps", "sets[1].rpe", "sets[1].set_type", "sets[2].exercise"} {
		hasIssue(t, er, f)
	}
	expectErr(t, e.do("GET", "/api/v1/workouts/today", nil, nil), 404, "not_found")

	hasIssue(t, expectErr(t, post("2026-03-11", map[string]any{"sets": []any{map[string]any{"exercise": "squat"}}}, nil), 400, "validation_error"), "date")
	hasIssue(t, expectErr(t, post("2026-02-30", map[string]any{"sets": []any{map[string]any{"exercise": "squat"}}}, nil), 400, "validation_error"), "date")
	hasIssue(t, expectErr(t, post("today", map[string]any{}, nil), 400, "validation_error"), "sets")

	// Strict decoding.
	hasIssue(t, expectErr(t, post("today", `{"sets":[{"exercise":"squat","weight":80}]}`, nil), 400, "validation_error"), "weight")
	hasIssue(t, expectErr(t, post("today", `{"sets":[],"extra":1}`, nil), 400, "validation_error"), "extra")
	er = expectErr(t, post("today", `{"sets":[{"exercise":"squat","reps":"ten"}]}`, nil), 400, "validation_error")
	if fs := issueFields(er); len(fs) != 1 || !strings.HasSuffix(fs[0], "reps") {
		t.Fatalf("type issue fields = %v", fs)
	}
	expectErr(t, post("today", `{"sets":[{"exercise":"squat","reps":8.5}]}`, nil), 400, "validation_error")
	expectErr(t, post("today", `{"sets":[`, nil), 400, "bad_request")
	expectErr(t, post("today", `{"sets":[]} {"sets":[]}`, nil), 400, "bad_request")
	expectErr(t, post("today", `[]`, nil), 400, "bad_request")
	expectErr(t, post("today", "", h("Content-Type", "application/json")), 400, "bad_request")
	expectErr(t, post("today", `{"sets":[]}`, h("Content-Type", "")), 415, "unsupported_media_type")
	expectErr(t, post("today", `{"sets":[]}`, h("Content-Type", "text/plain")), 415, "unsupported_media_type")
	big := `{"sets":[{"exercise":"squat","notes":"` + strings.Repeat("x", maxBodyBytes) + `"}]}`
	expectErr(t, post("today", big, nil), 413, "payload_too_large")
	expectErr(t, post("today", map[string]any{"sets": []any{map[string]any{"exercise": "squat"}}}, h("If-Match", "banana")), 400, "bad_request")
	hasIssue(t, expectErr(t, post("today", map[string]any{"sets": []any{map[string]any{"exercise": "squat"}}}, h("Idempotency-Key", strings.Repeat("k", 129))), 400, "validation_error"), "idempotency_key")
	expectErr(t, post("today", map[string]any{"plan_id": 99, "sets": []any{map[string]any{"exercise": "squat"}}}, nil), 404, "not_found")
}

func TestLogSetsIdempotentReplay(t *testing.T) {
	e := newEnv(t)
	body := map[string]any{"sets": []any{map[string]any{"exercise": "deadlift", "weight_kg": 140, "reps": 5}}}
	first := expect(t, e.do("POST", "/api/v1/workouts/today/sets", body, h("Idempotency-Key", "k-1")), 201)
	r := e.do("POST", "/api/v1/workouts/today/sets", body, h("Idempotency-Key", "k-1"))
	again := expect(t, r, 200)
	if again["replayed"] != true || first["replayed"] != false {
		t.Fatalf("replayed flags: first %v again %v", first["replayed"], again["replayed"])
	}
	if num(list(again, "sets")[0].(map[string]any)["id"]) != num(list(first, "sets")[0].(map[string]any)["id"]) {
		t.Fatal("replay returned a different set")
	}
	if r.etag() == "" {
		t.Fatal("replay without ETag")
	}
	w := expect(t, e.do("GET", "/api/v1/workouts/today", nil, nil), 200)
	if len(list(w, "sets")) != 1 {
		t.Fatalf("replay wrote again: %v", list(w, "sets"))
	}
	other := map[string]any{"sets": []any{map[string]any{"exercise": "deadlift", "weight_kg": 150, "reps": 5}}}
	expectErr(t, e.do("POST", "/api/v1/workouts/today/sets", other, h("Idempotency-Key", "k-1")), 409, "conflict")
}

func TestLogSetsIfMatch(t *testing.T) {
	e := newEnv(t)
	body := map[string]any{"sets": []any{map[string]any{"exercise": "row", "reps": 10}}}
	r := e.do("POST", "/api/v1/workouts/today/sets", body, nil)
	expect(t, r, 201)
	tag := r.etag()
	expectErr(t, e.do("POST", "/api/v1/workouts/today/sets", body, h("If-Match", `"999"`)), 412, "precondition_failed")
	r = e.do("POST", "/api/v1/workouts/today/sets", body, h("If-Match", tag))
	expect(t, r, 201)
	if r.etag() == tag {
		t.Fatal("ETag did not change after a write")
	}
	expectErr(t, e.do("POST", "/api/v1/workouts/today/sets", body, h("If-Match", tag)), 412, "precondition_failed")
}

func TestFollowPlanAndPrescriptions(t *testing.T) {
	e := newEnv(t)
	r := e.do("POST", "/api/v1/plans", samplePlan("Upper A"), nil)
	created := expect(t, r, 201)
	planID := num(created["plan"].(map[string]any)["id"])
	if r.header.Get("Location") != fmt.Sprintf("/api/v1/plans/%d", planID) || r.etag() != `"1"` {
		t.Fatalf("Location %q ETag %q", r.header.Get("Location"), r.etag())
	}
	other := num(expect(t, e.do("POST", "/api/v1/plans", samplePlan("Lower A"), nil), 201)["plan"].(map[string]any)["id"])

	hasIssue(t, expectErr(t, e.do("POST", "/api/v1/workouts/today/plan", map[string]any{}, nil), 400, "validation_error"), "plan_id")
	expectErr(t, e.do("POST", "/api/v1/workouts/today/plan", map[string]any{"plan_id": 999}, nil), 404, "not_found")
	expectErr(t, e.do("GET", "/api/v1/workouts/today", nil, nil), 404, "not_found")

	r = e.do("POST", "/api/v1/workouts/today/plan", map[string]any{"plan_id": planID}, nil)
	w := expect(t, r, 200)
	if num(w["plan_id"]) != planID || w["plan_name"] != "Upper A" || len(list(w, "progress")) != 2 {
		t.Fatalf("followed = %v", w)
	}
	expect(t, e.do("POST", "/api/v1/workouts/today/plan", map[string]any{"plan_id": planID}, nil), 200)
	expectErr(t, e.do("POST", "/api/v1/workouts/today/plan", map[string]any{"plan_id": other}, nil), 409, "conflict")

	// Adjust a prescription.
	path := "/api/v1/workouts/today/prescriptions/bench%20press"
	tag := r.etag()
	expectErr(t, e.do("PATCH", path, map[string]any{"target_sets": 5}, h("If-Match", `"99"`)), 412, "precondition_failed")
	r = e.do("PATCH", path, map[string]any{"target_sets": 5, "target_weight_kg": 82.5, "skipped": false}, h("If-Match", tag))
	w = expect(t, r, 200)
	var bench map[string]any
	for _, p := range list(w, "progress") {
		if p.(map[string]any)["exercise"] == "bench press" {
			bench = p.(map[string]any)
		}
	}
	if num(bench["target_sets"]) != 5 || bench["target_weight_kg"] != 82.5 || num(bench["rep_min"]) != 6 {
		t.Fatalf("adjusted = %v", bench)
	}
	if r.etag() == tag {
		t.Fatal("ETag did not change")
	}
	// Merge-patch content type is accepted too.
	expect(t, e.do("PATCH", path, `{"skipped":true}`, h("Content-Type", "application/merge-patch+json")), 200)
	hasIssue(t, expectErr(t, e.do("PATCH", path, `{"target_rpe":null}`, nil), 400, "validation_error"), "target_rpe")
	hasIssue(t, expectErr(t, e.do("PATCH", path, `{"sets":3}`, nil), 400, "validation_error"), "sets")
	hasIssue(t, expectErr(t, e.do("PATCH", path, `{"target_sets":"x"}`, nil), 400, "validation_error"), "target_sets")
	expectErr(t, e.do("PATCH", path, `{}`, nil), 400, "validation_error")
	expectErr(t, e.do("PATCH", path, `{"target_sets":50}`, nil), 400, "validation_error")
	expectErr(t, e.do("PATCH", "/api/v1/workouts/today/prescriptions/curl", `{"target_sets":2}`, nil), 404, "not_found")
	expectErr(t, e.do("PATCH", "/api/v1/workouts/2026-01-01/prescriptions/curl", `{"target_sets":2}`, nil), 404, "not_found")

	// Remove it; logged sets would survive as off-plan work.
	w = expect(t, e.do("DELETE", path, nil, nil), 200)
	if len(list(w, "progress")) != 1 {
		t.Fatalf("after delete progress = %v", list(w, "progress"))
	}
	expectErr(t, e.do("DELETE", path, nil, nil), 404, "not_found")
	expectErr(t, e.do("DELETE", "/api/v1/workouts/today/prescriptions/cable%20row", nil, h("If-Match", `"1"`)), 412, "precondition_failed")
}

func TestSetReadPatchDelete(t *testing.T) {
	e := newEnv(t)
	m := expect(t, e.do("POST", "/api/v1/workouts/2026-03-09/sets", map[string]any{"sets": []any{
		map[string]any{"exercise": "press", "weight_kg": 50, "reps": 8, "rpe": 8, "rir": 2, "notes": "ok"},
		map[string]any{"exercise": "press", "weight_kg": 50, "reps": 7},
	}}, nil), 201)
	id := num(list(m, "sets")[0].(map[string]any)["id"])
	path := fmt.Sprintf("/api/v1/sets/%d", id)

	r := e.do("GET", path, nil, nil)
	s := expect(t, r, 200)
	if r.etag() != quoted(num(s["revision"])) || s["weight_kg"] != 50.0 {
		t.Fatalf("set %v etag %q", s, r.etag())
	}
	tag := r.etag()

	patch := map[string]any{"weight_kg": nil, "reps": 9, "set_type": "failure"}
	expectErr(t, e.do("PATCH", path, patch, nil), 428, "precondition_required")
	expectErr(t, e.do("PATCH", path, patch, h("If-Match", `"42"`)), 412, "precondition_failed")
	r = e.do("PATCH", path, patch, h("If-Match", tag))
	s = expect(t, r, 200)
	if s["weight_kg"] != nil || num(s["reps"]) != 9 || s["set_type"] != "failure" || s["rpe"] != 8.0 || s["notes"] != "ok" {
		t.Fatalf("patched = %v", s)
	}
	if r.etag() == tag || r.etag() != quoted(num(s["revision"])) {
		t.Fatalf("ETag after patch %q", r.etag())
	}
	tag = r.etag()
	// Weak and * forms.
	r = e.do("PATCH", path, `{"rpe":null,"rir":null}`, h("If-Match", "W/"+tag, "Content-Type", "application/merge-patch+json"))
	s = expect(t, r, 200)
	if s["rpe"] != nil || s["rir"] != nil || s["si"] != 0.0 {
		t.Fatalf("cleared = %v", s)
	}
	expect(t, e.do("PATCH", path, `{"exercise":"Overhead Press","notes":"strict"}`, h("If-Match", "*")), 200)

	er := expectErr(t, e.do("PATCH", path, `{"rpe":11,"reps":0,"notes":null,"colour":"red"}`, h("If-Match", "*")), 400, "validation_error")
	hasIssue(t, er, "colour")
	er = expectErr(t, e.do("PATCH", path, `{"rpe":11,"reps":0,"notes":null}`, h("If-Match", "*")), 400, "validation_error")
	hasIssue(t, er, "notes")
	er = expectErr(t, e.do("PATCH", path, `{"rpe":11,"reps":0}`, h("If-Match", "*")), 400, "validation_error")
	hasIssue(t, er, "rpe")
	hasIssue(t, er, "reps")
	hasIssue(t, expectErr(t, e.do("PATCH", path, `{"weight_kg":"heavy"}`, h("If-Match", "*")), 400, "validation_error"), "weight_kg")
	hasIssue(t, expectErr(t, e.do("PATCH", path, `{"set_type":"mega"}`, h("If-Match", "*")), 400, "validation_error"), "set_type")
	expectErr(t, e.do("PATCH", path, `{}`, h("If-Match", "*")), 400, "validation_error")
	expectErr(t, e.do("PATCH", path, `{"exercise_id":9999}`, h("If-Match", "*")), 404, "not_found")
	expectErr(t, e.do("PATCH", "/api/v1/sets/9999", `{"reps":1}`, h("If-Match", "*")), 404, "not_found")
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/sets/abc", nil, nil), 400, "validation_error"), "id")
	expectErr(t, e.do("GET", "/api/v1/sets/9999", nil, nil), 404, "not_found")

	// Delete.
	cur := e.do("GET", path, nil, nil)
	expectErr(t, e.do("DELETE", path, nil, nil), 428, "precondition_required")
	expectErr(t, e.do("DELETE", path, nil, h("If-Match", `"1"`)), 412, "precondition_failed")
	d := expect(t, e.do("DELETE", path, nil, h("If-Match", cur.etag())), 200)
	wk := d["workout"].(map[string]any)
	if num(d["deleted_set_id"]) != id || len(list(wk, "sets")) != 1 || num(list(wk, "sets")[0].(map[string]any)["position"]) != 1 {
		t.Fatalf("delete result = %v", d)
	}
	expectErr(t, e.do("GET", path, nil, nil), 404, "not_found")
	expectErr(t, e.do("DELETE", path, nil, h("If-Match", "*")), 404, "not_found")
}

func TestPlans(t *testing.T) {
	e := newEnv(t)
	if l := expect(t, e.do("GET", "/api/v1/plans", nil, nil), 200); len(list(l, "plans")) != 0 {
		t.Fatalf("plans = %v", l)
	}
	res := expect(t, e.do("POST", "/api/v1/plans", samplePlan("Push / Pull Día 1"), nil), 201)
	if res["created"] != true || len(list(res, "new_exercises")) != 2 {
		t.Fatalf("create = %v", res)
	}
	id := num(res["plan"].(map[string]any)["id"])
	path := fmt.Sprintf("/api/v1/plans/%d", id)
	expectErr(t, e.do("POST", "/api/v1/plans", samplePlan("push / pull día 1"), nil), 409, "conflict")

	er := expectErr(t, e.do("POST", "/api/v1/plans", map[string]any{"format": "workout-plan", "version": 2, "name": "", "bogus": 1,
		"items": []any{map[string]any{"exercise": "x", "sets": 0, "reps": 3}}}, nil), 400, "validation_error")
	for _, f := range []string{"bogus", "version", "name", "items[0].sets", "items[0].reps"} {
		hasIssue(t, er, f)
	}

	r := e.do("GET", path, nil, nil)
	p := expect(t, r, 200)
	if r.etag() != `"1"` || num(p["total_sets"]) != 5 || len(list(p, "items")) != 2 {
		t.Fatalf("plan = %v etag %q", p, r.etag())
	}
	l := expect(t, e.do("GET", "/api/v1/plans", nil, nil), 200)
	if len(list(l, "plans")) != 1 {
		t.Fatalf("plans = %v", l)
	}

	// Export round-trips through import.
	r = e.do("GET", path+"/export", nil, nil)
	doc := expect(t, r, 200)
	if cd := r.header.Get("Content-Disposition"); cd != `attachment; filename="push-pull-d-a-1.json"` {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if doc["format"] != "workout-plan" || num(doc["version"]) != 1 || r.etag() != `"1"` {
		t.Fatalf("doc = %v", doc)
	}
	raw, _ := json.Marshal(doc)
	expectErr(t, e.do("POST", "/api/v1/plans/import", raw, nil), 409, "conflict")
	dry := expect(t, e.do("POST", "/api/v1/plans/import?on_conflict=replace&dry_run=true", raw, nil), 200)
	if dry["dry_run"] != true || dry["replaced"] != true {
		t.Fatalf("dry = %v", dry)
	}
	if r := e.do("GET", path, nil, nil); r.etag() != `"1"` {
		t.Fatal("dry run wrote")
	}
	r = e.do("POST", "/api/v1/plans/import?on_conflict=replace", raw, h("Idempotency-Key", "imp-1"))
	rep := expect(t, r, 200)
	if rep["replaced"] != true || num(rep["plan"].(map[string]any)["id"]) != id || r.etag() != `"2"` {
		t.Fatalf("replace = %v etag %q", rep, r.etag())
	}
	newDoc := samplePlan("Legs")
	r = e.do("POST", "/api/v1/plans/import?dry_run=true", newDoc, nil)
	if d := expect(t, r, 200); d["created"] != true || num(d["plan"].(map[string]any)["id"]) != 0 || r.etag() != "" {
		t.Fatalf("dry create = %v", d)
	}
	r = e.do("POST", "/api/v1/plans/import?dry_run=false&on_conflict=error", newDoc, nil)
	legs := expect(t, r, 201)
	if r.header.Get("Location") == "" {
		t.Fatal("no Location")
	}
	hasIssue(t, expectErr(t, e.do("POST", "/api/v1/plans/import?on_conflict=merge", raw, nil), 400, "validation_error"), "on_conflict")
	hasIssue(t, expectErr(t, e.do("POST", "/api/v1/plans/import?dry_run=maybe", raw, nil), 400, "validation_error"), "dry_run")
	expectErr(t, e.do("POST", "/api/v1/plans/import", "{", nil), 400, "validation_error")

	// Replace.
	upd := samplePlan("Push A")
	upd["items"] = []any{map[string]any{"exercise": "dips", "sets": 4}}
	expectErr(t, e.do("PUT", path, upd, nil), 428, "precondition_required")
	expectErr(t, e.do("PUT", path, upd, h("If-Match", `"1"`)), 412, "precondition_failed")
	r = e.do("PUT", path, upd, h("If-Match", `"2"`))
	p = expect(t, r, 200)
	if p["name"] != "Push A" || num(p["total_sets"]) != 4 || r.etag() != `"3"` {
		t.Fatalf("put = %v etag %q", p, r.etag())
	}
	expectErr(t, e.do("PUT", path, map[string]any{"name": "x"}, h("If-Match", `"3"`)), 400, "validation_error")
	expectErr(t, e.do("PUT", "/api/v1/plans/999", upd, h("If-Match", `"1"`)), 404, "not_found")

	// Delete.
	expectErr(t, e.do("DELETE", path, nil, nil), 428, "precondition_required")
	expectErr(t, e.do("DELETE", path, nil, h("If-Match", `"2"`)), 412, "precondition_failed")
	r = e.do("DELETE", path, nil, h("If-Match", `"3"`))
	if r.status != 204 || len(r.body) != 0 {
		t.Fatalf("delete: %d %s", r.status, r.body)
	}
	expectErr(t, e.do("GET", path, nil, nil), 404, "not_found")
	expectErr(t, e.do("GET", path+"/export", nil, nil), 404, "not_found")
	expectErr(t, e.do("DELETE", path, nil, h("If-Match", "*")), 404, "not_found")
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/plans/0", nil, nil), 400, "validation_error"), "id")
	_ = legs
}

func TestExercises(t *testing.T) {
	e := newEnv(t)
	expect(t, e.do("POST", "/api/v1/workouts/2026-03-02/sets", map[string]any{"sets": []any{
		map[string]any{"exercise": "Leg Press", "weight_kg": 200, "reps": 10},
		map[string]any{"exercise": "Leg Press", "weight_kg": 220, "reps": 8},
		map[string]any{"exercise": "Leg Press", "weight_kg": 100, "reps": 12, "set_type": "warmup"},
		map[string]any{"exercise": "calf raise", "reps": 15},
	}}, nil), 201)
	expect(t, e.do("POST", "/api/v1/workouts/2026-03-05/sets", map[string]any{"sets": []any{
		map[string]any{"exercise": "leg press", "weight_kg": 230, "reps": 6},
	}}, nil), 201)

	l := expect(t, e.do("GET", "/api/v1/exercises", nil, nil), 200)
	if len(list(l, "exercises")) != 2 {
		t.Fatalf("exercises = %v", l)
	}
	ex := expect(t, e.do("GET", "/api/v1/exercises/leg%20press", nil, nil), 200)
	id := num(ex["id"])
	if num(ex["set_count"]) != 4 {
		t.Fatalf("exercise = %v", ex)
	}
	if byID := expect(t, e.do("GET", fmt.Sprintf("/api/v1/exercises/%d", id), nil, nil), 200); byID["name"] != "leg press" {
		t.Fatalf("by id = %v", byID)
	}
	expectErr(t, e.do("GET", "/api/v1/exercises/hack%20squat", nil, nil), 404, "not_found")

	// Aliases.
	ex = expect(t, e.do("POST", fmt.Sprintf("/api/v1/exercises/%d/aliases", id), map[string]any{"alias": "Prensa"}, nil), 200)
	if fmt.Sprint(ex["aliases"]) != "[prensa]" {
		t.Fatalf("aliases = %v", ex["aliases"])
	}
	if byAlias := expect(t, e.do("GET", "/api/v1/exercises/PRENSA", nil, nil), 200); num(byAlias["id"]) != id {
		t.Fatalf("by alias = %v", byAlias)
	}
	expectErr(t, e.do("POST", "/api/v1/exercises/calf%20raise/aliases", map[string]any{"alias": "prensa"}, nil), 409, "conflict")
	expectErr(t, e.do("POST", "/api/v1/exercises/calf%20raise/aliases", map[string]any{"alias": "leg press"}, nil), 409, "conflict")
	hasIssue(t, expectErr(t, e.do("POST", "/api/v1/exercises/calf%20raise/aliases", map[string]any{}, nil), 400, "validation_error"), "alias")
	hasIssue(t, expectErr(t, e.do("POST", "/api/v1/exercises/calf%20raise/aliases", map[string]any{"alias": "  "}, nil), 400, "validation_error"), "alias")
	expectErr(t, e.do("DELETE", "/api/v1/exercises/calf%20raise/aliases/prensa", nil, nil), 404, "not_found")

	// History and stats by alias.
	hist := expect(t, e.do("GET", "/api/v1/exercises/prensa/history?limit=2", nil, nil), 200)
	if hist["exercise"] != "leg press" || len(list(hist, "sets")) != 2 || hist["best"] == nil {
		t.Fatalf("history = %v", hist)
	}
	expectErr(t, e.do("GET", "/api/v1/exercises/prensa/history?limit=x", nil, nil), 400, "validation_error")
	expectErr(t, e.do("GET", "/api/v1/exercises/nothing/history", nil, nil), 404, "not_found")
	st := expect(t, e.do("GET", "/api/v1/exercises/prensa/stats?from=2026-03-01&to=today", nil, nil), 200)
	if num(st["workouts"]) != 2 || num(st["working_sets"]) != 3 || num(st["warmup_sets"]) != 1 || st["volume_kg"] != 200*10+220*8+230*6.0 {
		t.Fatalf("stats = %v", st)
	}
	if mw := st["max_weight_kg"].(map[string]any); mw["value"] != 230.0 || mw["date"] != "2026-03-05" {
		t.Fatalf("max weight = %v", mw)
	}
	expectErr(t, e.do("GET", "/api/v1/exercises/prensa/stats?from=2026-03-09&to=2026-03-01", nil, nil), 400, "validation_error")

	// Rename.
	ex = expect(t, e.do("PATCH", "/api/v1/exercises/prensa", map[string]any{"name": "Leg Press 45"}, nil), 200)
	if ex["name"] != "leg press 45" || num(ex["id"]) != id {
		t.Fatalf("renamed = %v", ex)
	}
	expectErr(t, e.do("PATCH", "/api/v1/exercises/calf%20raise", map[string]any{"name": "leg press 45"}, nil), 409, "conflict")
	hasIssue(t, expectErr(t, e.do("PATCH", "/api/v1/exercises/calf%20raise", map[string]any{}, nil), 400, "validation_error"), "name")
	hasIssue(t, expectErr(t, e.do("PATCH", "/api/v1/exercises/calf%20raise", map[string]any{"name": "x", "alias": "y"}, nil), 400, "validation_error"), "alias")
	expectErr(t, e.do("PATCH", "/api/v1/exercises/999", map[string]any{"name": "x"}, nil), 404, "not_found")

	ex = expect(t, e.do("DELETE", fmt.Sprintf("/api/v1/exercises/%d/aliases/Prensa", id), nil, nil), 200)
	if len(ex["aliases"].([]any)) != 0 {
		t.Fatalf("after alias delete = %v", ex)
	}
	expectErr(t, e.do("DELETE", fmt.Sprintf("/api/v1/exercises/%d/aliases/prensa", id), nil, nil), 404, "not_found")
}

func TestStatsSummary(t *testing.T) {
	e := newEnv(t)
	expect(t, e.do("POST", "/api/v1/workouts/2026-03-03/sets", map[string]any{"sets": []any{
		map[string]any{"exercise": "squat", "weight_kg": 100, "reps": 5},
		map[string]any{"exercise": "squat", "reps": 5},
		map[string]any{"exercise": "squat", "weight_kg": 60, "reps": 5, "set_type": "warmup"},
	}}, nil), 201)
	s := expect(t, e.do("GET", "/api/v1/stats/summary", nil, nil), 200)
	if s["to"] != "2026-03-10" || s["from"] != "2026-02-11" || num(s["working_sets"]) != 2 || num(s["unrecorded_sets"]) != 1 || s["volume_kg"] != 500.0 {
		t.Fatalf("summary = %v", s)
	}
	s = expect(t, e.do("GET", "/api/v1/stats/summary?from=2026-03-04&to=today", nil, nil), 200)
	if num(s["workouts"]) != 0 {
		t.Fatalf("summary = %v", s)
	}
	expectErr(t, e.do("GET", "/api/v1/stats/summary?from=2026-03-09&to=2026-03-01", nil, nil), 400, "validation_error")
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/stats/summary?to=yesterday", nil, nil), 400, "validation_error"), "to")
}

// Concurrent first writes to a date that has no workout must converge on one
// workout holding every set.
func TestConcurrentFirstPostCreatesOneWorkout(t *testing.T) {
	e := newEnv(t)
	const n = 12
	var wg sync.WaitGroup
	statuses := make([]int, n)
	created := make([]bool, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := e.do("POST", "/api/v1/workouts/2026-03-01/sets", map[string]any{"sets": []any{
				map[string]any{"exercise": "pull up", "reps": i + 1},
			}}, h("Idempotency-Key", fmt.Sprintf("c-%d", i)))
			statuses[i] = r.status
			if r.status == 201 {
				created[i] = r.obj(t)["created"] == true
			}
		}()
	}
	wg.Wait()
	nCreated := 0
	for i := range n {
		if statuses[i] != 201 {
			t.Fatalf("request %d: status %d", i, statuses[i])
		}
		if created[i] {
			nCreated++
		}
	}
	if nCreated != 1 {
		t.Fatalf("%d requests reported creating the workout, want 1", nCreated)
	}
	l := expect(t, e.do("GET", "/api/v1/workouts?from=2026-03-01&to=2026-03-01", nil, nil), 200)
	ws := list(l, "workouts")
	if len(ws) != 1 || num(ws[0].(map[string]any)["set_count"]) != n {
		t.Fatalf("workouts = %v", ws)
	}
}

func TestInternalErrorsAreNotLeaked(t *testing.T) {
	e := newEnv(t)
	e.st.Close()
	er := expectErr(t, e.do("GET", "/api/v1/workouts/today", nil, nil), 500, "internal")
	if er["message"] != "internal server error" || er["issues"] != nil {
		t.Fatalf("500 body leaks: %v", er)
	}
	if !strings.Contains(e.logBuf.String(), "GET /api/v1/workouts/today") {
		t.Fatalf("internal error was not logged: %q", e.logBuf.String())
	}
}

// The adapter must not hide engine semantics: a domain conflict without
// If-Match stays a 409, and is a 412 with it.
func TestConflictMapping(t *testing.T) {
	e := newEnv(t)
	api := e.api
	for _, tc := range []struct {
		ifMatch string
		status  int
		code    string
	}{{"", 409, "conflict"}, {`"1"`, 412, "precondition_failed"}} {
		req, _ := http.NewRequest("GET", "/api/v1/x", nil)
		if tc.ifMatch != "" {
			req.Header.Set("If-Match", tc.ifMatch)
		}
		rec := httptest.NewRecorder()
		api.writeError(rec, req, fmt.Errorf("%w: stale", training.ErrConflict))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.code) {
			t.Fatalf("If-Match %q -> %d %s", tc.ifMatch, rec.Code, rec.Body.String())
		}
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{"Upper A": "upper-a", "  ¡Día!  ": "d-a", "***": "plan-7", "Push/Pull 2": "push-pull-2"} {
		if got := slug(in, 7); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}
