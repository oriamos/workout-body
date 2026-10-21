package e2e

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"sync"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 0.051 }

func setByNumber(w map[string]any, exercise string, n int) map[string]any {
	for _, s := range list(w["sets"]) {
		m := s.(map[string]any)
		if m["exercise"] == exercise && int(num(m["set_number"])) == n {
			return m
		}
	}
	return nil
}

func progressFor(w map[string]any, exercise string) map[string]any {
	for _, p := range list(w["progress"]) {
		m := p.(map[string]any)
		if m["exercise"] == exercise {
			return m
		}
	}
	return nil
}

// TestAcceptanceFlow is the flow from the product brief:
// import a plan -> retrieve it -> log incrementally through MCP -> read the same
// workout through HTTP -> correct a set -> verify persisted history and statistics.
func TestAcceptanceFlow(t *testing.T) {
	srv := start(t, "")
	cs := srv.mcpClient()

	// --- 0. the configured timezone and the local "today"
	cfg := srv.do("GET", "/api/v1/config", nil).Map(t)
	if cfg["timezone"] != "Asia/Jerusalem" {
		t.Fatalf("config: %v", cfg)
	}
	today := cfg["today"].(string)

	// --- 1. import a plan from the documented example through HTTP
	doc, err := os.ReadFile("../../docs/examples/plan-upper-lower.json")
	if err != nil {
		t.Fatal(err)
	}
	dry := srv.do("POST", "/api/v1/plans/import?dry_run=true", doc)
	if dry.Status != 200 && dry.Status != 201 {
		t.Fatalf("dry run: %d %s", dry.Status, dry.Body)
	}
	if plans := srv.do("GET", "/api/v1/plans", nil).Map(t); len(list(plans["plans"])) != 0 {
		t.Fatalf("a dry run must not create a plan: %v", plans)
	}
	imp := srv.do("POST", "/api/v1/plans/import", doc)
	if imp.Status != 201 {
		t.Fatalf("import: %d %s", imp.Status, imp.Body)
	}
	planID := int(num(get(imp.Map(t), "plan", "id")))
	again := srv.do("POST", "/api/v1/plans/import", doc)
	if again.Status != 409 {
		t.Fatalf("re-importing the same name must conflict by default, got %d %s", again.Status, again.Body)
	}
	bad := srv.do("POST", "/api/v1/plans/import", []byte(`{"format":"workout-plan","version":1,"name":"Broken","items":[{"exercise":"a","sets":0},{"exercise":"a","sets":3,"target_rir":11}]}`))
	if bad.Status != 400 || len(list(get(bad.Map(t), "error", "issues"))) < 3 {
		t.Fatalf("an invalid plan must report every issue: %d %s", bad.Status, bad.Body)
	}
	if plans := srv.do("GET", "/api/v1/plans", nil).Map(t); len(list(plans["plans"])) != 1 {
		t.Fatalf("a failed import must leave nothing behind: %v", plans)
	}

	// --- 2. retrieve it, over HTTP and over MCP
	plan := srv.do("GET", "/api/v1/plans/"+strconv.Itoa(planID), nil)
	if plan.Status != 200 || plan.Header.Get("ETag") == "" {
		t.Fatalf("get plan: %d %v", plan.Status, plan.Header)
	}
	if items := list(plan.Map(t)["items"]); len(items) != 5 {
		t.Fatalf("plan items: %d", len(items))
	}
	viaMCP := tool(t, cs, "get_plan", map[string]any{"plan_id": planID})
	if get(viaMCP, "plan", "name") != "Upper A" {
		t.Fatalf("mcp get_plan: %v", viaMCP)
	}
	exported := tool(t, cs, "export_plan", map[string]any{"plan_id": planID})
	if get(exported, "plan", "format") != "workout-plan" || len(list(get(exported, "plan", "items"))) != 5 {
		t.Fatalf("export: %v", exported)
	}

	// --- 3. log incrementally through MCP: the four messages of the brief
	// (1) "Bench press, first set: 8 reps at 60 kg. Planning 3 sets."
	r1 := tool(t, cs, "log_sets", map[string]any{
		"plan_id":       planID,
		"sets":          []any{map[string]any{"exercise": "bench press", "weight_kg": 60, "reps": 8}},
		"prescriptions": []any{map[string]any{"exercise": "bench press", "target_sets": 3}},
	})
	if r1["created"] != true || get(r1, "workout", "date") != today {
		t.Fatalf("first write must create today's workout (%s): %v", today, r1["workout"])
	}
	// (2) "Second set, same." - the assistant resolves it to explicit values.
	r2 := tool(t, cs, "log_sets", map[string]any{"sets": []any{map[string]any{"exercise": "bench press", "weight_kg": 60, "reps": 8}}})
	if r2["created"] != false || get(r2, "workout", "id") != get(r1, "workout", "id") {
		t.Fatalf("second write must reuse the workout: %v", r2)
	}
	set2ID := int(num(list(r2["sets"])[0].(map[string]any)["id"]))
	// (3) "Third set, same. Starting squats: 130 kg, 4 reps, planning 3 sets."
	r3 := tool(t, cs, "log_sets", map[string]any{
		"sets":          []any{map[string]any{"exercise": "bench press", "weight_kg": 60, "reps": 8}},
		"prescriptions": []any{map[string]any{"exercise": "squat", "target_sets": 3, "rep_min": 4, "rep_max": 4, "target_weight_kg": 130}},
	})
	if len(list(r3["sets"])) != 1 {
		t.Fatalf("only the performed set is created, never the planned squats: %v", r3["sets"])
	}
	// (4) "Actually, bench set two was 7 reps." - corrected by id.
	upd := tool(t, cs, "update_set", map[string]any{"set_id": set2ID, "reps": 7})
	if num(get(upd, "set", "reps")) != 7 && num(upd["reps"]) != 7 {
		t.Fatalf("update_set result: %v", upd)
	}

	// --- 4. read the same workout through HTTP
	wk := srv.do("GET", "/api/v1/workouts/today", nil)
	if wk.Status != 200 {
		t.Fatalf("workout: %d %s", wk.Status, wk.Body)
	}
	w := wk.Map(t)
	if w["date"] != today {
		t.Fatalf("date: %v want %s", w["date"], today)
	}
	for n, wantReps := range map[int]float64{1: 8, 2: 7, 3: 8} {
		s := setByNumber(w, "bench press", n)
		if s == nil || num(s["reps"]) != wantReps || num(s["weight_kg"]) != 60 {
			t.Fatalf("bench set %d: %v", n, s)
		}
	}
	if bp := progressFor(w, "bench press"); bp == nil || num(bp["done_sets"]) != 3 || num(bp["target_sets"]) != 3 {
		t.Fatalf("bench progress: %v", bp)
	}
	if bp := progressFor(w, "bench press"); num(bp["target_weight_kg"]) != 70 {
		t.Fatalf("the plan's target weight must survive a 'sets only' prescription: %v", bp)
	}
	sq := progressFor(w, "squat")
	if sq == nil || num(sq["target_sets"]) != 3 || num(sq["rep_min"]) != 4 || num(sq["target_weight_kg"]) != 130 || num(sq["done_sets"]) != 0 {
		t.Fatalf("squats must be PLANNED 3x4@130 with zero completed sets: %v", sq)
	}
	if setByNumber(w, "squat", 1) != nil {
		t.Fatal("a planned squat set must not exist as a performed set")
	}
	if w["plan_name"] != "Upper A" {
		t.Fatalf("plan name: %v", w["plan_name"])
	}

	// --- 5. correct a set through HTTP, with optimistic concurrency
	set3 := setByNumber(w, "bench press", 3)
	set3ID := int(num(set3["id"]))
	path := "/api/v1/sets/" + strconv.Itoa(set3ID)
	cur := srv.do("GET", path, nil)
	etag := cur.Header.Get("ETag")
	if etag == "" {
		t.Fatalf("a set must expose an ETag: %v", cur.Header)
	}
	if r := srv.do("PATCH", path, map[string]any{"weight_kg": 62.5}, "If-Match", "-"); r.Status != 428 {
		t.Fatalf("PATCH without If-Match must be 428, got %d %s", r.Status, r.Body)
	}
	if r := srv.do("PATCH", path, map[string]any{"weight_kg": 62.5}, "If-Match", `"999"`); r.Status != 412 {
		t.Fatalf("stale If-Match must be 412, got %d %s", r.Status, r.Body)
	}
	ok := srv.do("PATCH", path, map[string]any{"weight_kg": 62.5}, "If-Match", etag)
	if ok.Status != 200 || num(ok.Map(t)["weight_kg"]) != 62.5 {
		t.Fatalf("patch: %d %s", ok.Status, ok.Body)
	}
	if again := srv.do("PATCH", path, map[string]any{"weight_kg": 70}, "If-Match", etag); again.Status != 412 {
		t.Fatalf("reusing the old ETag must now fail, got %d", again.Status)
	}
	// the conversational side sees the UI's correction
	mw := tool(t, cs, "get_workout", map[string]any{})
	found := false
	for _, s := range list(get(mw, "workout", "sets")) {
		if m := s.(map[string]any); int(num(m["id"])) == set3ID && num(m["weight_kg"]) == 62.5 {
			found = true
		}
	}
	if !found {
		t.Fatalf("MCP must read the value corrected over HTTP: %v", mw)
	}
	// and an MCP edit with a stale revision is refused, not silently applied
	msg := toolErr(t, cs, "update_set", map[string]any{"set_id": set3ID, "reps": 5, "expected_revision": 1})
	if msg == "" {
		t.Fatal("expected a conflict message")
	}

	// --- 6. persisted history and statistics (HTTP and MCP agree)
	hist := srv.do("GET", "/api/v1/exercises/bench%20press/history", nil)
	if hist.Status != 200 {
		t.Fatalf("history: %d %s", hist.Status, hist.Body)
	}
	if n := len(list(hist.Map(t)["sets"])); n != 3 {
		t.Fatalf("history sets: %d", n)
	}
	st := srv.do("GET", "/api/v1/exercises/bench%20press/stats", nil).Map(t)
	// volume = 60*8 + 60*7 + 62.5*8 = 1400 ; best e1RM (Epley) = 62.5*(1+8/30) = 79.2
	if num(st["working_sets"]) != 3 || !approx(num(st["volume_kg"]), 1400) {
		t.Fatalf("stats: %v", st)
	}
	if !approx(num(get(st, "best_e1rm", "value")), 79.2) || num(get(st, "max_weight_kg", "value")) != 62.5 {
		t.Fatalf("bests: %v", st)
	}
	sum := srv.do("GET", "/api/v1/stats/summary", nil).Map(t)
	if num(sum["working_sets"]) != 3 || !approx(num(sum["volume_kg"]), 1400) || num(sum["workouts"]) != 1 {
		t.Fatalf("summary (planned squats must not count): %v", sum)
	}
	mst, _ := tool(t, cs, "exercise_stats", map[string]any{"exercise": "bench press"})["stats"].(map[string]any)
	if !approx(num(mst["volume_kg"]), 1400) || num(mst["working_sets"]) != 3 {
		t.Fatalf("mcp exercise_stats must match HTTP: %v", mst)
	}

	// --- 7. everything survives a restart on the same database file
	srv.stop()
	srv2 := start(t, srv.db)
	w2 := srv2.do("GET", "/api/v1/workouts/"+today, nil).Map(t)
	if len(list(w2["sets"])) != 3 || num(setByNumber(w2, "bench press", 3)["weight_kg"]) != 62.5 || setByNumber(w2, "bench press", 2)["reps"].(float64) != 7 {
		t.Fatalf("data lost across restart: %v", w2["sets"])
	}
	if sq := progressFor(w2, "squat"); sq == nil || num(sq["target_sets"]) != 3 {
		t.Fatalf("prescription lost across restart: %v", w2["progress"])
	}
}

func TestAuthenticationOnEveryExposedSurface(t *testing.T) {
	srv := start(t, "")
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/config"}, {"GET", "/api/v1/workouts/today"}, {"GET", "/api/v1/plans"},
		{"POST", "/api/v1/workouts/today/sets"}, {"GET", "/api/v1/stats/summary"}, {"POST", "/mcp"},
	} {
		for name, hdr := range map[string]string{"missing": "-", "wrong": "Bearer nope", "wrong scheme": "Basic " + token} {
			r := srv.do(c.method, c.path, []byte(`{}`), "Authorization", hdr)
			if r.Status != 401 {
				t.Errorf("%s %s with %s credentials: status %d, want 401", c.method, c.path, name, r.Status)
			}
		}
	}
	if r := srv.do("GET", "/health", nil, "Authorization", "-"); r.Status != 200 {
		t.Errorf("/health must stay open, got %d", r.Status)
	}
}

// Writes arriving at the same moment through both adapters must neither
// duplicate the workout nor lose sets.
func TestConcurrentFirstWritesFromMCPAndHTTP(t *testing.T) {
	srv := start(t, "")
	const date = "2026-01-05"
	const each = 8
	var wg sync.WaitGroup
	errs := make(chan string, 4*each)
	for i := 0; i < each; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			cs := srv.mcpClient()
			res := tool(t, cs, "log_sets", map[string]any{"date": date, "sets": []any{map[string]any{"exercise": "row", "weight_kg": 50 + i, "reps": 10}}})
			_ = res
		}(i)
		go func(i int) {
			defer wg.Done()
			r := srv.do("POST", "/api/v1/workouts/"+date+"/sets", map[string]any{"sets": []any{map[string]any{"exercise": "row", "weight_kg": 70 + i, "reps": 6}}})
			if r.Status != 201 {
				errs <- fmt.Sprintf("http %d: %s", r.Status, r.Body)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	ws := list(srv.do("GET", "/api/v1/workouts?from="+date+"&to="+date, nil).Map(t)["workouts"])
	if len(ws) != 1 {
		t.Fatalf("exactly one workout per date, got %d", len(ws))
	}
	w := srv.do("GET", "/api/v1/workouts/"+date, nil).Map(t)
	if n := len(list(w["sets"])); n != 2*each {
		t.Fatalf("expected %d sets, got %d", 2*each, n)
	}
	seen := map[float64]bool{}
	for _, s := range list(w["sets"]) {
		id := num(s.(map[string]any)["id"])
		if seen[id] {
			t.Fatalf("duplicate set id %v", id)
		}
		seen[id] = true
	}
}

// Two editors holding the same revision: exactly one wins.
func TestConcurrentEditSameRevisionExactlyOneWins(t *testing.T) {
	srv := start(t, "")
	cs := srv.mcpClient()
	r := tool(t, cs, "log_sets", map[string]any{"date": "2026-02-02", "sets": []any{map[string]any{"exercise": "deadlift", "weight_kg": 100, "reps": 5}}})
	s := list(r["sets"])[0].(map[string]any)
	id, rev := int(num(s["id"])), int(num(s["revision"]))
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	wg.Add(2)
	go func() {
		defer wg.Done()
		res := srv.do("PATCH", "/api/v1/sets/"+strconv.Itoa(id), map[string]any{"reps": 6}, "If-Match", strconv.Itoa(rev))
		mu.Lock()
		defer mu.Unlock()
		if res.Status == 200 {
			wins++
		} else if res.Status != 412 {
			t.Errorf("loser must see 412, got %d", res.Status)
		}
	}()
	go func() {
		defer wg.Done()
		res, err := cs.CallTool(t.Context(), toolParams("update_set", map[string]any{"set_id": id, "reps": 7, "expected_revision": rev}))
		mu.Lock()
		defer mu.Unlock()
		if err == nil && !res.IsError {
			wins++
		}
	}()
	wg.Wait()
	if wins != 1 {
		t.Fatalf("exactly one concurrent editor may win, got %d", wins)
	}
}

// The same idempotency key through the two adapters is one logical write.
func TestIdempotencyKeyAcrossAdapters(t *testing.T) {
	srv := start(t, "")
	cs := srv.mcpClient()
	body := map[string]any{"sets": []any{map[string]any{"exercise": "bench press", "weight_kg": 60, "reps": 8}}, "idempotency_key": "retry-me-1"}
	first := tool(t, cs, "log_sets", map[string]any{"date": "2026-02-03", "sets": body["sets"], "idempotency_key": "retry-me-1"})
	if first["replayed"] == true {
		t.Fatal("first call must not be a replay")
	}
	r := srv.do("POST", "/api/v1/workouts/2026-02-03/sets", map[string]any{"sets": body["sets"]}, "Idempotency-Key", "retry-me-1")
	if r.Status != 200 || r.Map(t)["replayed"] != true {
		t.Fatalf("the same key and payload over HTTP must replay: %d %s", r.Status, r.Body)
	}
	if n := len(list(srv.do("GET", "/api/v1/workouts/2026-02-03", nil).Map(t)["sets"])); n != 1 {
		t.Fatalf("a retry must not duplicate sets, got %d", n)
	}
	// a deliberate identical second set (no key) is stored
	srv.do("POST", "/api/v1/workouts/2026-02-03/sets", map[string]any{"sets": body["sets"]})
	if n := len(list(srv.do("GET", "/api/v1/workouts/2026-02-03", nil).Map(t)["sets"])); n != 2 {
		t.Fatalf("intentional identical sets must remain possible, got %d", n)
	}
	diff := srv.do("POST", "/api/v1/workouts/2026-02-03/sets", map[string]any{"sets": []any{map[string]any{"exercise": "bench press", "weight_kg": 99, "reps": 1}}}, "Idempotency-Key", "retry-me-1")
	if diff.Status != 409 {
		t.Fatalf("same key, different payload must be 409, got %d %s", diff.Status, diff.Body)
	}
}
