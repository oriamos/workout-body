package e2e

import (
	"strconv"
	"testing"
)

func nutr(m map[string]any, nutrient, field string) any { return get(m, nutrient, field) }

// The nutrition acceptance flow on the real binary, with MCP writes and HTTP
// reads against one database:
// set targets -> two meals via MCP -> read via HTTP -> edit -> copy a previous
// meal -> delete and check the recalculated totals.
func TestNutritionAcceptanceFlow(t *testing.T) {
	srv := start(t, "")
	cs := srv.mcpClient()
	today := srv.do("GET", "/api/v1/config", nil).Map(t)["today"].(string)

	// 1. Targets (over MCP), effective from today.
	tg := tool(t, cs, "set_nutrition_targets", map[string]any{"calories_kcal": 2200, "protein_g": 150, "expected_revision": 0})
	if raw, _ := tg["targets"].(map[string]any); raw != nil {
		tg = raw
	}
	if tg["effective_from"] != today || num(tg["calories_kcal"]) != 2200 || tg["carbs_g"] != nil {
		t.Fatalf("targets: %v", tg)
	}

	// 2. Two meals through MCP; the second has no fat value (missing, not 0).
	add := func(args map[string]any) map[string]any {
		r := tool(t, cs, "add_nutrition_entry", args)
		if e, ok := r["entry"].(map[string]any); ok {
			return e
		}
		return r
	}
	oats := add(map[string]any{"name": "Oatmeal with berries", "calories_kcal": 350, "protein_g": 12.5, "carbs_g": 58, "fat_g": 7, "idempotency_key": "e2e-oats"})
	add(map[string]any{"name": "Oatmeal with berries", "calories_kcal": 350, "protein_g": 12.5, "carbs_g": 58, "fat_g": 7, "idempotency_key": "e2e-oats"}) // retry
	chicken := add(map[string]any{"name": "Chicken and rice", "calories_kcal": 650, "protein_g": 48, "carbs_g": 70})
	if oats["date"] != today || chicken["fat_g"] != nil {
		t.Fatalf("meals: %v / %v", oats, chicken)
	}

	// 3. The same entries and totals through HTTP.
	d := srv.do("GET", "/api/v1/nutrition/days/today", nil).Map(t)
	if num(d["entry_count"]) != 2 {
		t.Fatalf("a retried add must not duplicate; got %v entries", d["entry_count"])
	}
	if num(nutr(d, "calories_kcal", "consumed")) != 1000 || num(nutr(d, "protein_g", "consumed")) != 60.5 ||
		num(nutr(d, "calories_kcal", "remaining")) != 1200 || num(nutr(d, "protein_g", "remaining")) != 89.5 ||
		nutr(d, "carbs_g", "target") != nil || nutr(d, "carbs_g", "remaining") != nil ||
		num(nutr(d, "fat_g", "consumed")) != 7 || num(nutr(d, "fat_g", "entries_missing_value")) != 1 {
		t.Fatalf("day over HTTP: %v", d)
	}
	list := srv.do("GET", "/api/v1/nutrition/entries?date="+today, nil).Map(t)
	if n := len(list_(list["entries"])); n != 2 {
		t.Fatalf("entries over HTTP: %d", n)
	}

	// 4. Edit one entry over HTTP (If-Match), visible over MCP.
	id := strconv.Itoa(int(num(chicken["id"])))
	cur := srv.do("GET", "/api/v1/nutrition/entries/"+id, nil)
	if r := srv.do("PATCH", "/api/v1/nutrition/entries/"+id, map[string]any{"calories_kcal": 700}, "Content-Type", "application/merge-patch+json", "If-Match", "-"); r.Status != 428 {
		t.Fatalf("PATCH without If-Match: %d", r.Status)
	}
	up := srv.do("PATCH", "/api/v1/nutrition/entries/"+id, map[string]any{"calories_kcal": 700}, "Content-Type", "application/merge-patch+json", "If-Match", cur.Header.Get("ETag"))
	if up.Status != 200 || num(up.Map(t)["protein_g"]) != 48 {
		t.Fatalf("partial edit: %d %s", up.Status, up.Body)
	}
	mget := tool(t, cs, "get_nutrition_entry", map[string]any{"entry_id": num(chicken["id"])})
	if e, ok := mget["entry"].(map[string]any); ok {
		mget = e
	}
	if num(mget["calories_kcal"]) != 700 {
		t.Fatalf("MCP must see the HTTP edit: %v", mget)
	}
	// An MCP edit holding the old revision is refused, not applied.
	if msg := toolErr(t, cs, "update_nutrition_entry", map[string]any{"entry_id": num(chicken["id"]), "calories_kcal": 1, "expected_revision": 1}); msg == "" {
		t.Fatal("expected a conflict")
	}

	// 5. Copy a previous meal (logged on an earlier date) to today.
	past := add(map[string]any{"name": "Lentil soup", "calories_kcal": 320, "protein_g": 18, "date": "2026-01-15"})
	cp := tool(t, cs, "copy_nutrition_entry", map[string]any{"entry_id": num(past["id"]), "idempotency_key": "e2e-copy"})
	if e, ok := cp["entry"].(map[string]any); ok {
		cp = e
	}
	if cp["date"] != today || num(cp["id"]) == num(past["id"]) || num(cp["calories_kcal"]) != 320 {
		t.Fatalf("copy: %v", cp)
	}
	d = srv.do("GET", "/api/v1/nutrition/days/today", nil).Map(t)
	if num(d["entry_count"]) != 3 || num(nutr(d, "calories_kcal", "consumed")) != 1370 || num(nutr(d, "calories_kcal", "over")) != 0 {
		t.Fatalf("after copy: %v", d)
	}

	// 6. Delete an entry; totals are recalculated.
	oid := strconv.Itoa(int(num(oats["id"])))
	et := srv.do("GET", "/api/v1/nutrition/entries/"+oid, nil).Header.Get("ETag")
	if r := srv.do("DELETE", "/api/v1/nutrition/entries/"+oid, nil, "If-Match", et); r.Status != 204 {
		t.Fatalf("delete: %d %s", r.Status, r.Body)
	}
	md := tool(t, cs, "get_nutrition_day", map[string]any{})
	if raw, ok := md["day"].(map[string]any); ok {
		md = raw
	}
	if num(md["entry_count"]) != 2 || num(nutr(md, "calories_kcal", "consumed")) != 1020 || num(nutr(md, "protein_g", "consumed")) != 66 {
		t.Fatalf("after delete (via MCP): %v", md)
	}

	// The past day keeps its entry and shows no target (targets started today).
	pd := srv.do("GET", "/api/v1/nutrition/days/2026-01-15", nil).Map(t)
	if num(pd["entry_count"]) != 1 || nutr(pd, "calories_kcal", "target") != nil {
		t.Fatalf("historical day: %v", pd)
	}
	// Nutrition never created a workout.
	if r := srv.do("GET", "/api/v1/workouts/today", nil); r.Status != 404 {
		t.Fatalf("logging food must not create a workout, got %d", r.Status)
	}
	// Everything survives a restart.
	srv.stop()
	srv2 := start(t, srv.db)
	if d := srv2.do("GET", "/api/v1/nutrition/days/today", nil).Map(t); num(nutr(d, "calories_kcal", "consumed")) != 1020 || num(nutr(d, "calories_kcal", "target")) != 2200 {
		t.Fatalf("after restart: %v", d)
	}
}

func TestNutritionEndpointsRequireAuth(t *testing.T) {
	srv := start(t, "")
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/nutrition/days/today"}, {"GET", "/api/v1/nutrition/entries"}, {"POST", "/api/v1/nutrition/entries"},
		{"GET", "/api/v1/nutrition/targets"}, {"PUT", "/api/v1/nutrition/targets/today"},
	} {
		if r := srv.do(c.method, c.path, []byte(`{}`), "Authorization", "-"); r.Status != 401 {
			t.Errorf("%s %s without a token: %d", c.method, c.path, r.Status)
		}
	}
}

func list_(v any) []any { return list(v) }
