package e2e

import (
	"testing"
)

// weekAt returns the i-th week of a weight trend response.
func weekAt(t *testing.T, tr map[string]any, i int) map[string]any {
	t.Helper()
	weeks := list(tr["weeks"])
	if i >= len(weeks) {
		t.Fatalf("trend has %d weeks, want index %d: %v", len(weeks), i, tr)
	}
	return weeks[i].(map[string]any)
}

// The body-weight flow on the real binary, MCP writes and HTTP reads against
// one database: three weigh-ins over two Sunday-start weeks (the default week) via MCP -> trend
// over HTTP -> replace one via PUT with If-Match -> delete one -> the weekly
// averages and the week-over-week change recompute. Synthetic data on fixed
// past dates (weeks of Sun 2026-01-04 and Sun 2026-01-11), so the result does
// not depend on the real clock.
func TestWeightAcceptanceFlow(t *testing.T) {
	srv := start(t, "")
	cs := srv.mcpClient()

	// 1. Three weigh-ins through MCP; a retry of the same value is harmless.
	logW := func(date string, kg float64) map[string]any {
		r := tool(t, cs, "log_weigh_in", map[string]any{"date": date, "weight_kg": kg})
		w, _ := r["weigh_in"].(map[string]any)
		if w == nil || w["date"] != date || num(w["weight_kg"]) != kg || r["timezone"] == "" {
			t.Fatalf("log_weigh_in %s: %v", date, r)
		}
		return r
	}
	if r := logW("2026-01-06", 80.0); r["created"] != true {
		t.Fatalf("first weigh-in not created: %v", r)
	}
	logW("2026-01-08", 79.6)
	logW("2026-01-13", 79.2)
	if r := logW("2026-01-13", 79.2); r["created"] != false || num(get(r, "weigh_in", "revision")) != 1 {
		t.Fatalf("retry must not create or bump: %v", r)
	}

	// 2. The trend over HTTP.
	trendPath := "/api/v1/weight/trend?to=2026-01-17&weeks=2"
	tr := srv.do("GET", trendPath, nil).Map(t)
	w1, w2 := weekAt(t, tr, 0), weekAt(t, tr, 1)
	if tr["from"] != "2026-01-04" || w1["week_start"] != "2026-01-04" || num(w1["days_logged"]) != 2 || !approx(num(w1["average_kg"]), 79.8) {
		t.Fatalf("week 1: %v", w1)
	}
	if w2["week_start"] != "2026-01-11" || num(w2["days_logged"]) != 1 || !approx(num(w2["average_kg"]), 79.2) ||
		!approx(num(w2["change_kg"]), -0.6) || w2["trend"] != "down" {
		t.Fatalf("week 2: %v", w2)
	}
	if w1["change_kg"] != nil {
		t.Fatalf("week 1 has no previous data, its change must be null: %v", w1)
	}
	if l := list(srv.do("GET", "/api/v1/weight/entries?from=2026-01-01&to=2026-01-31", nil).Map(t)["entries"]); len(l) != 3 ||
		l[0].(map[string]any)["date"] != "2026-01-06" {
		t.Fatalf("entries over HTTP: %v", l)
	}

	// 3. Replace one via PUT with If-Match (the ETag from a GET).
	cur := srv.do("GET", "/api/v1/weight/entries/2026-01-13", nil)
	if cur.Status != 200 || cur.Header.Get("ETag") != `"1"` {
		t.Fatalf("GET weigh-in: %d %q %s", cur.Status, cur.Header.Get("ETag"), cur.Body)
	}
	up := srv.do("PUT", "/api/v1/weight/entries/2026-01-13", map[string]any{"weight_kg": 78.8, "note": "corrected"}, "If-Match", cur.Header.Get("ETag"))
	if up.Status != 200 || up.Header.Get("ETag") != `"2"` || num(up.Map(t)["weight_kg"]) != 78.8 {
		t.Fatalf("PUT replace: %d %q %s", up.Status, up.Header.Get("ETag"), up.Body)
	}
	if r := srv.do("PUT", "/api/v1/weight/entries/2026-01-13", map[string]any{"weight_kg": 70}, "If-Match", `"1"`); r.Status != 412 {
		t.Fatalf("stale If-Match: %d %s", r.Status, r.Body)
	}
	// MCP sees the HTTP edit.
	if g := tool(t, cs, "get_weigh_in", map[string]any{"date": "2026-01-13"}); g["found"] != true || num(get(g, "weigh_in", "weight_kg")) != 78.8 {
		t.Fatalf("MCP must see the HTTP edit: %v", g)
	}
	tr = srv.do("GET", trendPath, nil).Map(t)
	if w2 := weekAt(t, tr, 1); !approx(num(w2["average_kg"]), 78.8) || !approx(num(w2["change_kg"]), -1.0) {
		t.Fatalf("after replace: %v", w2)
	}

	// 4. Delete one (If-Match required).
	if r := srv.do("DELETE", "/api/v1/weight/entries/2026-01-08", nil); r.Status != 428 {
		t.Fatalf("DELETE without If-Match: %d", r.Status)
	}
	et := srv.do("GET", "/api/v1/weight/entries/2026-01-08", nil).Header.Get("ETag")
	if r := srv.do("DELETE", "/api/v1/weight/entries/2026-01-08", nil, "If-Match", et); r.Status != 204 {
		t.Fatalf("delete: %d %s", r.Status, r.Body)
	}
	if r := srv.do("GET", "/api/v1/weight/entries/2026-01-08", nil); r.Status != 404 {
		t.Fatalf("deleted weigh-in still readable: %d", r.Status)
	}

	// 5. Averages recompute, over HTTP and over MCP.
	tr = srv.do("GET", trendPath, nil).Map(t)
	w1, w2 = weekAt(t, tr, 0), weekAt(t, tr, 1)
	if num(w1["days_logged"]) != 1 || !approx(num(w1["average_kg"]), 80.0) || !approx(num(w2["change_kg"]), -1.2) || !approx(num(tr["overall_change_kg"]), -1.2) {
		t.Fatalf("after delete: %v / %v", w1, w2)
	}
	mt := tool(t, cs, "get_weight_trend", map[string]any{"to": "2026-01-17", "weeks": 2})
	if w := weekAt(t, mt, 1); !approx(num(w["average_kg"]), 78.8) || !approx(num(w["change_kg"]), -1.2) || mt["timezone"] == "" {
		t.Fatalf("MCP trend after delete: %v", mt)
	}

	// Weight never created a workout on those dates.
	if r := srv.do("GET", "/api/v1/workouts/2026-01-13", nil); r.Status != 404 {
		t.Fatalf("logging weight must not create a workout, got %d", r.Status)
	}
	// Everything survives a restart.
	srv.stop()
	srv2 := start(t, srv.db)
	if l := list(srv2.do("GET", "/api/v1/weight/entries", nil).Map(t)["entries"]); len(l) != 2 {
		t.Fatalf("after restart: %v", l)
	}
}

func TestWeightEndpointsRequireAuth(t *testing.T) {
	srv := start(t, "")
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/weight/entries"}, {"GET", "/api/v1/weight/entries/today"}, {"PUT", "/api/v1/weight/entries/today"},
		{"DELETE", "/api/v1/weight/entries/today"}, {"GET", "/api/v1/weight/trend"},
	} {
		if r := srv.do(c.method, c.path, []byte(`{}`), "Authorization", "-"); r.Status != 401 {
			t.Errorf("%s %s without a token: %d", c.method, c.path, r.Status)
		}
	}
}
