package apisrv

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/training-mcp/internal/httpsrv"
	"github.com/fullfran/training-mcp/internal/training"
)

func postEntry(t *testing.T, e *env, body any, hdr map[string]string, status int) (map[string]any, resp) {
	t.Helper()
	r := e.do("POST", "/api/v1/nutrition/entries", body, hdr)
	m := expect(t, r, status)
	return m, r
}

func entryOf(m map[string]any) map[string]any { return m["entry"].(map[string]any) }

func TestNutritionEntryLifecycle(t *testing.T) {
	e := newEnv(t)
	m, r := postEntry(t, e, map[string]any{"name": "  Greek   yogurt ", "calories_kcal": 150, "protein_g": 15.5, "carbs_g": nil}, nil, 201)
	en := entryOf(m)
	id := num(en["id"])
	path := fmt.Sprintf("/api/v1/nutrition/entries/%d", id)
	if m["replayed"] != false || en["name"] != "Greek yogurt" || en["date"] != "2026-03-10" || en["carbs_g"] != nil || en["fat_g"] != nil ||
		en["calories_kcal"] != 150.0 || en["copied_from"] != nil {
		t.Fatalf("entry = %v", en)
	}
	if r.header.Get("Location") != path || r.etag() != quoted(num(en["revision"])) {
		t.Fatalf("Location %q ETag %q", r.header.Get("Location"), r.etag())
	}
	r = e.do("GET", path, nil, nil)
	if g := expect(t, r, 200); g["name"] != "Greek yogurt" || r.etag() == "" {
		t.Fatalf("get = %v", g)
	}
	tag := r.etag()

	// PATCH: If-Match required; partial changes keep the rest; null clears.
	expectErr(t, e.do("PATCH", path, map[string]any{"protein_g": 17}, nil), 428, "precondition_required")
	expectErr(t, e.do("PATCH", path, map[string]any{"protein_g": 17}, h("If-Match", `"99"`)), 412, "precondition_failed")
	r = e.do("PATCH", path, map[string]any{"protein_g": 17}, h("If-Match", tag))
	p := expect(t, r, 200)
	if p["protein_g"] != 17.0 || p["calories_kcal"] != 150.0 || p["name"] != "Greek yogurt" || p["carbs_g"] != nil || r.etag() == tag {
		t.Fatalf("partial patch = %v", p)
	}
	expectErr(t, e.do("PATCH", path, map[string]any{"protein_g": 18}, h("If-Match", tag)), 412, "precondition_failed")
	r = e.do("PATCH", path, `{"calories_kcal":null,"fat_g":0,"date":"2026-03-09","name":"Skyr"}`, h("If-Match", r.etag(), "Content-Type", "application/merge-patch+json"))
	p = expect(t, r, 200)
	if p["calories_kcal"] != nil || p["fat_g"] != 0.0 || p["date"] != "2026-03-09" || p["name"] != "Skyr" || p["protein_g"] != 17.0 {
		t.Fatalf("clear/zero patch = %v", p)
	}

	star := h("If-Match", "*")
	for _, tc := range []struct{ body, field string }{
		{`{"name":null}`, "name"},
		{`{"date":null}`, "date"},
		{`{"name":"  "}`, "name"},
		{`{"calories_kcal":"lots"}`, "calories_kcal"},
		{`{"protein_g":-1}`, "protein_g"},
		{`{"carbs_g":2001}`, "carbs_g"},
		{`{"calories_kcal":20001}`, "calories_kcal"},
		{`{"date":"2026-03-11"}`, "date"},
		{`{"date":"2026-02-30"}`, "date"},
		{`{"sugar_g":3}`, "sugar_g"},
	} {
		hasIssue(t, expectErr(t, e.do("PATCH", path, tc.body, star), 400, "validation_error"), tc.field)
	}
	expectErr(t, e.do("PATCH", path, `{}`, star), 400, "validation_error")
	expectErr(t, e.do("PATCH", "/api/v1/nutrition/entries/999", `{"name":"x"}`, star), 404, "not_found")

	// DELETE.
	cur := e.do("GET", path, nil, nil)
	expectErr(t, e.do("DELETE", path, nil, nil), 428, "precondition_required")
	expectErr(t, e.do("DELETE", path, nil, h("If-Match", `"1"`)), 412, "precondition_failed")
	if r := e.do("DELETE", path, nil, h("If-Match", cur.etag())); r.status != 204 {
		t.Fatalf("delete: %d %s", r.status, r.body)
	}
	expectErr(t, e.do("GET", path, nil, nil), 404, "not_found")
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/nutrition/entries/abc", nil, nil), 400, "validation_error"), "id")
}

func TestNutritionCreateValidation(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct {
		body  string
		field string
	}{
		{`{"calories_kcal":100}`, "name"},
		{`{"name":"rice","calories_kcal":-5}`, "calories_kcal"},
		{`{"name":"rice","protein_g":5000}`, "protein_g"},
		{`{"name":"rice","fat_g":"7"}`, "fat_g"},
		{`{"name":"rice","carbs_g":true}`, "carbs_g"},
		{`{"name":"rice","date":"2026-03-11"}`, "date"},
		{`{"name":"rice","date":"tomorrow"}`, "date"},
		{`{"name":"rice","sodium_mg":3}`, "sodium_mg"},
		{`{"name":"` + strings.Repeat("r", 201) + `"}`, "name"},
	} {
		hasIssue(t, expectErr(t, e.do("POST", "/api/v1/nutrition/entries", tc.body, nil), 400, "validation_error"), tc.field)
	}
	expectErr(t, e.do("POST", "/api/v1/nutrition/entries", `{"name":"rice","calories_kcal":1e999}`, nil), 400, "validation_error")
	expectErr(t, e.do("POST", "/api/v1/nutrition/entries", `{"name":"rice"`, nil), 400, "bad_request")
	expectErr(t, e.do("POST", "/api/v1/nutrition/entries", `{"name":"rice"}`, h("Content-Type", "text/plain")), 415, "unsupported_media_type")
	hasIssue(t, expectErr(t, e.do("POST", "/api/v1/nutrition/entries", `{"name":"rice"}`, h("Idempotency-Key", strings.Repeat("k", 129))), 400, "validation_error"), "idempotency_key")
	// Explicit zero is kept as zero; "today" and past dates are accepted.
	m, _ := postEntry(t, e, map[string]any{"name": "water", "calories_kcal": 0, "date": "today"}, nil, 201)
	if entryOf(m)["calories_kcal"] != 0.0 || entryOf(m)["date"] != "2026-03-10" {
		t.Fatalf("zero entry = %v", m)
	}
	m, _ = postEntry(t, e, map[string]any{"name": "rice", "date": "2025-12-31"}, nil, 201)
	if entryOf(m)["date"] != "2025-12-31" {
		t.Fatalf("past entry = %v", m)
	}
}

func TestNutritionIdempotencyAndDuplicates(t *testing.T) {
	e := newEnv(t)
	body := map[string]any{"name": "banana", "calories_kcal": 105, "carbs_g": 27}
	first, r1 := postEntry(t, e, body, h("Idempotency-Key", "meal-1"), 201)
	again, r2 := postEntry(t, e, body, h("Idempotency-Key", "meal-1"), 200)
	if again["replayed"] != true || num(entryOf(again)["id"]) != num(entryOf(first)["id"]) || r2.etag() != r1.etag() {
		t.Fatalf("replay = %v", again)
	}
	expectErr(t, e.do("POST", "/api/v1/nutrition/entries", map[string]any{"name": "banana", "calories_kcal": 110}, h("Idempotency-Key", "meal-1")), 409, "conflict")
	// Without a key, the same body twice is two portions.
	postEntry(t, e, body, nil, 201)
	postEntry(t, e, body, nil, 201)
	l := expect(t, e.do("GET", "/api/v1/nutrition/entries?date=today", nil, nil), 200)
	if len(list(l, "entries")) != 3 {
		t.Fatalf("entries = %v", l)
	}
	if d := expect(t, e.do("GET", "/api/v1/nutrition/days/today", nil, nil), 200); d["calories_kcal"].(map[string]any)["consumed"] != 315.0 || num(d["entry_count"]) != 3 {
		t.Fatalf("day = %v", d)
	}
}

func TestNutritionCopy(t *testing.T) {
	e := newEnv(t)
	src, _ := postEntry(t, e, map[string]any{"name": "oats", "calories_kcal": 380, "protein_g": 13, "date": "2026-03-08"}, nil, 201)
	srcID := num(entryOf(src)["id"])
	copyPath := fmt.Sprintf("/api/v1/nutrition/entries/%d/copy", srcID)

	r := e.do("POST", copyPath, nil, nil) // body omitted: today
	cp := entryOf(expect(t, r, 201))
	cpID := num(cp["id"])
	if cpID == srcID || num(cp["copied_from"]) != srcID || cp["date"] != "2026-03-10" || cp["calories_kcal"] != 380.0 || cp["name"] != "oats" {
		t.Fatalf("copy = %v", cp)
	}
	if r.header.Get("Location") != fmt.Sprintf("/api/v1/nutrition/entries/%d", cpID) {
		t.Fatalf("Location = %q", r.header.Get("Location"))
	}
	// Independence: editing the copy leaves the source; deleting the source keeps the copy.
	expect(t, e.do("PATCH", fmt.Sprintf("/api/v1/nutrition/entries/%d", cpID), map[string]any{"calories_kcal": 400}, h("If-Match", "*")), 200)
	if s := expect(t, e.do("GET", fmt.Sprintf("/api/v1/nutrition/entries/%d", srcID), nil, nil), 200); s["calories_kcal"] != 380.0 {
		t.Fatalf("source changed: %v", s)
	}
	if r := e.do("DELETE", fmt.Sprintf("/api/v1/nutrition/entries/%d", srcID), nil, h("If-Match", "*")); r.status != 204 {
		t.Fatalf("delete source: %d", r.status)
	}
	if c := expect(t, e.do("GET", fmt.Sprintf("/api/v1/nutrition/entries/%d", cpID), nil, nil), 200); c["calories_kcal"] != 400.0 {
		t.Fatalf("copy after source delete = %v", c)
	}

	path2 := fmt.Sprintf("/api/v1/nutrition/entries/%d/copy", cpID)
	m := expect(t, e.do("POST", path2, map[string]any{"date": "2026-03-09"}, h("Idempotency-Key", "cp-1")), 201)
	if entryOf(m)["date"] != "2026-03-09" {
		t.Fatalf("dated copy = %v", m)
	}
	if again := expect(t, e.do("POST", path2, map[string]any{"date": "2026-03-09"}, h("Idempotency-Key", "cp-1")), 200); again["replayed"] != true || num(entryOf(again)["id"]) != num(entryOf(m)["id"]) {
		t.Fatalf("copy replay = %v", again)
	}
	hasIssue(t, expectErr(t, e.do("POST", path2, map[string]any{"date": "2026-03-11"}, nil), 400, "validation_error"), "date")
	hasIssue(t, expectErr(t, e.do("POST", path2, map[string]any{"when": "x"}, nil), 400, "validation_error"), "when")
	expectErr(t, e.do("POST", "/api/v1/nutrition/entries/999/copy", nil, nil), 404, "not_found")
}

func summary(d map[string]any, nutrient string) map[string]any { return d[nutrient].(map[string]any) }

func TestNutritionDaysAndTargets(t *testing.T) {
	e := newEnv(t)
	// No targets yet: nulls, not zeros.
	d := expect(t, e.do("GET", "/api/v1/nutrition/days/today", nil, nil), 200)
	cal := summary(d, "calories_kcal")
	if d["today"] != true || num(d["entry_count"]) != 0 || cal["consumed"] != 0.0 || cal["target"] != nil || cal["remaining"] != nil || cal["over"] != nil {
		t.Fatalf("empty day = %v", d)
	}
	r := e.do("GET", "/api/v1/nutrition/targets", nil, nil)
	tg := expect(t, r, 200)
	if r.etag() != `"0"` || num(tg["targets"].(map[string]any)["revision"]) != 0 || len(list(tg, "history")) != 0 {
		t.Fatalf("no targets = %v etag %q", tg, r.etag())
	}

	// First targets, effective 2026-03-01, need If-Match "0".
	body := map[string]any{"calories_kcal": 2000, "protein_g": 150}
	expectErr(t, e.do("PUT", "/api/v1/nutrition/targets/2026-03-01", body, nil), 428, "precondition_required")
	expectErr(t, e.do("PUT", "/api/v1/nutrition/targets/2026-03-01", body, h("If-Match", `"5"`)), 412, "precondition_failed")
	r = e.do("PUT", "/api/v1/nutrition/targets/2026-03-01", body, h("If-Match", `"0"`))
	first := expect(t, r, 200)
	if first["effective_from"] != "2026-03-01" || first["calories_kcal"] != 2000.0 || first["carbs_g"] != nil || r.etag() != quoted(num(first["revision"])) {
		t.Fatalf("first targets = %v", first)
	}
	expectErr(t, e.do("PUT", "/api/v1/nutrition/targets/2026-03-01", body, h("If-Match", `"0"`)), 412, "precondition_failed")

	// Entries today, one without protein.
	a, _ := postEntry(t, e, map[string]any{"name": "lunch", "calories_kcal": 800, "protein_g": 50}, nil, 201)
	postEntry(t, e, map[string]any{"name": "pizza", "calories_kcal": 1500}, nil, 201)
	d = expect(t, e.do("GET", "/api/v1/nutrition/days/2026-03-10", nil, nil), 200)
	cal, pro, carb := summary(d, "calories_kcal"), summary(d, "protein_g"), summary(d, "carbs_g")
	if cal["consumed"] != 2300.0 || cal["target"] != 2000.0 || cal["remaining"] != 0.0 || cal["over"] != 300.0 {
		t.Fatalf("calories = %v", cal)
	}
	if pro["consumed"] != 50.0 || num(pro["entries_missing_value"]) != 1 || pro["remaining"] != 100.0 || pro["over"] != 0.0 {
		t.Fatalf("protein = %v", pro)
	}
	if carb["target"] != nil || num(carb["entries_missing_value"]) != 2 {
		t.Fatalf("carbs = %v", carb)
	}

	// Mutations are reflected in the totals.
	aPath := fmt.Sprintf("/api/v1/nutrition/entries/%d", num(entryOf(a)["id"]))
	expect(t, e.do("PATCH", aPath, map[string]any{"calories_kcal": 300}, h("If-Match", "*")), 200)
	if c := summary(expect(t, e.do("GET", "/api/v1/nutrition/days/today", nil, nil), 200), "calories_kcal"); c["consumed"] != 1800.0 || c["remaining"] != 200.0 || c["over"] != 0.0 {
		t.Fatalf("after patch = %v", c)
	}
	if r := e.do("DELETE", aPath, nil, h("If-Match", "*")); r.status != 204 {
		t.Fatal("delete")
	}
	if c := summary(expect(t, e.do("GET", "/api/v1/nutrition/days/today", nil, nil), 200), "calories_kcal"); c["consumed"] != 1500.0 {
		t.Fatalf("after delete = %v", c)
	}

	// A change from today: protein cleared, calories raised; earlier days keep theirs.
	r = e.do("GET", "/api/v1/nutrition/targets?date=today", nil, nil)
	expect(t, r, 200)
	r = e.do("PUT", "/api/v1/nutrition/targets/today", `{"calories_kcal":2500,"protein_g":null}`, h("If-Match", r.etag(), "Content-Type", "application/merge-patch+json"))
	second := expect(t, r, 200)
	if second["effective_from"] != "2026-03-10" || second["calories_kcal"] != 2500.0 || second["protein_g"] != nil {
		t.Fatalf("second targets = %v", second)
	}
	secondTag := r.etag()
	if old := expect(t, e.do("GET", "/api/v1/nutrition/targets?date=2026-03-05", nil, nil), 200)["targets"].(map[string]any); old["calories_kcal"] != 2000.0 || old["protein_g"] != 150.0 {
		t.Fatalf("history rewritten: %v", old)
	}
	days := list(expect(t, e.do("GET", "/api/v1/nutrition/days?from=2026-03-08&to=today", nil, nil), 200), "days")
	if len(days) != 3 || summary(days[0].(map[string]any), "calories_kcal")["target"] != 2000.0 || summary(days[2].(map[string]any), "calories_kcal")["target"] != 2500.0 ||
		summary(days[2].(map[string]any), "protein_g")["target"] != nil {
		t.Fatalf("days = %v", days)
	}
	if h := list(expect(t, e.do("GET", "/api/v1/nutrition/targets", nil, nil), 200), "history"); len(h) != 2 {
		t.Fatalf("history = %v", h)
	}

	// Validation.
	star := h("If-Match", secondTag)
	hasIssue(t, expectErr(t, e.do("PUT", "/api/v1/nutrition/targets/today", `{"calories_kcal":-1}`, star), 400, "validation_error"), "calories_kcal")
	hasIssue(t, expectErr(t, e.do("PUT", "/api/v1/nutrition/targets/today", `{"fat_g":"x"}`, star), 400, "validation_error"), "fat_g")
	hasIssue(t, expectErr(t, e.do("PUT", "/api/v1/nutrition/targets/today", `{"fiber_g":30}`, star), 400, "validation_error"), "fiber_g")
	expectErr(t, e.do("PUT", "/api/v1/nutrition/targets/today", `{}`, star), 400, "validation_error")
	hasIssue(t, expectErr(t, e.do("PUT", "/api/v1/nutrition/targets/2026-13-01", `{"fat_g":1}`, star), 400, "validation_error"), "date")
	expectErr(t, e.do("PUT", "/api/v1/nutrition/targets/today", `{"fat_g":1}`, h("If-Match", "zero")), 400, "bad_request")

	// Undo today's change: the March 1 targets apply again.
	expectErr(t, e.do("DELETE", "/api/v1/nutrition/targets/today", nil, nil), 428, "precondition_required")
	expectErr(t, e.do("DELETE", "/api/v1/nutrition/targets/today", nil, h("If-Match", quoted(num(first["revision"])))), 412, "precondition_failed")
	if r := e.do("DELETE", "/api/v1/nutrition/targets/today", nil, h("If-Match", secondTag)); r.status != 204 {
		t.Fatalf("delete targets: %d %s", r.status, r.body)
	}
	if now := expect(t, e.do("GET", "/api/v1/nutrition/targets", nil, nil), 200)["targets"].(map[string]any); now["calories_kcal"] != 2000.0 || now["effective_from"] != "2026-03-01" {
		t.Fatalf("after undo = %v", now)
	}
	expectErr(t, e.do("DELETE", "/api/v1/nutrition/targets/2026-03-05", nil, h("If-Match", "*")), 404, "not_found")

	// Range limits; nutrition never creates workouts.
	expect(t, e.do("GET", "/api/v1/nutrition/days?from=2025-03-10&to=2026-03-10", nil, nil), 200)
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/nutrition/days?from=2025-03-09&to=2026-03-10", nil, nil), 400, "validation_error"), "to")
	expectErr(t, e.do("GET", "/api/v1/nutrition/days?from=2026-03-10&to=2026-03-01", nil, nil), 400, "validation_error")
	expectErr(t, e.do("GET", "/api/v1/workouts/today", nil, nil), 404, "not_found")
}

func TestNutritionListAndSearch(t *testing.T) {
	e := newEnv(t)
	for _, en := range []map[string]any{
		{"name": "Greek Yogurt", "date": "2026-03-08", "calories_kcal": 120},
		{"name": "chicken rice", "date": "2026-03-09"},
		{"name": "yogurt bowl", "date": "2026-03-10"},
		{"name": "apple", "date": "2026-03-10"},
	} {
		postEntry(t, e, en, nil, 201)
	}
	names := func(path string) []string {
		var out []string
		for _, x := range list(expect(t, e.do("GET", path, nil, nil), 200), "entries") {
			out = append(out, x.(map[string]any)["name"].(string))
		}
		return out
	}
	if got := fmt.Sprint(names("/api/v1/nutrition/entries")); got != "[yogurt bowl apple chicken rice Greek Yogurt]" {
		t.Fatalf("all = %s", got)
	}
	if got := fmt.Sprint(names("/api/v1/nutrition/entries?q=YOGURT")); got != "[yogurt bowl Greek Yogurt]" {
		t.Fatalf("search = %s", got)
	}
	if got := fmt.Sprint(names("/api/v1/nutrition/entries?date=2026-03-09")); got != "[chicken rice]" {
		t.Fatalf("date = %s", got)
	}
	if got := fmt.Sprint(names("/api/v1/nutrition/entries?from=2026-03-08&to=2026-03-09&q=yog")); got != "[Greek Yogurt]" {
		t.Fatalf("range+q = %s", got)
	}
	if got := names("/api/v1/nutrition/entries?limit=1"); len(got) != 1 {
		t.Fatalf("limit = %v", got)
	}
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/nutrition/entries?limit=501", nil, nil), 400, "validation_error"), "limit")
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/nutrition/entries?limit=x", nil, nil), 400, "validation_error"), "limit")
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/nutrition/entries?date=today&from=2026-03-01", nil, nil), 400, "validation_error"), "date")
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/nutrition/entries?date=03/10", nil, nil), 400, "validation_error"), "date")
	expectErr(t, e.do("GET", "/api/v1/nutrition/entries?from=2026-03-10&to=2026-03-01", nil, nil), 400, "validation_error")
}

// noNutrition hides the store's nutrition methods, as a store without
// nutrition support would.
type noNutrition struct{ training.Store }

func TestNutritionDisabled(t *testing.T) {
	base := newEnv(t)
	svc := training.NewService(noNutrition{base.st}, func() time.Time { return time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC) }).WithLocation(jerusalem)
	api := New(svc)
	api.logger = log.New(&bytes.Buffer{}, "", 0)
	srv := httptest.NewServer(httpsrv.RoutesWithAPI(http.NotFoundHandler(), api.Handler(), false, testToken, nil))
	t.Cleanup(srv.Close)
	e := &env{t: t, svc: svc, st: base.st, api: api, srv: srv, validate: true}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/nutrition/entries"},
		{"GET", "/api/v1/nutrition/entries/1"},
		{"GET", "/api/v1/nutrition/days/today"},
		{"GET", "/api/v1/nutrition/days"},
		{"GET", "/api/v1/nutrition/targets"},
		{"DELETE", "/api/v1/nutrition/entries/1"},
	} {
		expectErr(t, e.do(c.method, c.path, nil, h("If-Match", "*")), 501, "nutrition_disabled")
	}
	expectErr(t, e.do("POST", "/api/v1/nutrition/entries", map[string]any{"name": "x"}, nil), 501, "nutrition_disabled")
	expectErr(t, e.do("PUT", "/api/v1/nutrition/targets/today", map[string]any{"fat_g": 1}, h("If-Match", "0")), 501, "nutrition_disabled")
}
