package apisrv

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/training-mcp/internal/httpsrv"
	"github.com/fullfran/training-mcp/internal/training"
)

// Synthetic weigh-ins. The fixed test clock makes today Tuesday 2026-03-10
// (Asia/Jerusalem); weeks start on Monday 2026-03-02 and 2026-03-09.

func TestWeighInLifecycle(t *testing.T) {
	e := newEnv(t)
	path := "/api/v1/weight/entries/2026-03-10"
	expectErr(t, e.do("GET", "/api/v1/weight/entries/today", nil, nil), 404, "not_found")

	// First PUT creates (201, ETag, Location); "today" resolves.
	r := e.do("PUT", "/api/v1/weight/entries/today", map[string]any{"weight_kg": 82.456, "note": " morning "}, nil)
	w := expect(t, r, 201)
	if w["date"] != "2026-03-10" || w["weight_kg"] != 82.46 || w["note"] != "morning" || num(w["revision"]) != 1 {
		t.Fatalf("created = %v", w)
	}
	if r.etag() != `"1"` || r.header.Get("Location") != path {
		t.Fatalf("ETag %q Location %q", r.etag(), r.header.Get("Location"))
	}
	r = e.do("GET", path, nil, nil)
	if g := expect(t, r, 200); g["weight_kg"] != 82.46 || r.etag() != `"1"` {
		t.Fatalf("get = %v %q", g, r.etag())
	}

	// The same values again (a retry): 200, no revision bump.
	r = e.do("PUT", path, map[string]any{"weight_kg": 82.46, "note": "morning"}, nil)
	if g := expect(t, r, 200); num(g["revision"]) != 1 || r.etag() != `"1"` {
		t.Fatalf("retry = %v", g)
	}
	// Different values replace the day's weigh-in (no If-Match needed).
	r = e.do("PUT", path, map[string]any{"weight_kg": 82.1}, nil)
	if g := expect(t, r, 200); g["weight_kg"] != 82.1 || g["note"] != nil || num(g["revision"]) != 2 || r.etag() != `"2"` {
		t.Fatalf("replace = %v", g)
	}
	if l := list(expect(t, e.do("GET", "/api/v1/weight/entries", nil, nil), 200), "entries"); len(l) != 1 {
		t.Fatalf("one weigh-in per day, got %v", l)
	}

	// If-Match on PUT: stale -> 412, current -> applied, "0" = must not exist, "*" = must exist.
	expectErr(t, e.do("PUT", path, map[string]any{"weight_kg": 81.9}, h("If-Match", `"1"`)), 412, "precondition_failed")
	expectErr(t, e.do("PUT", path, map[string]any{"weight_kg": 81.9}, h("If-Match", `"0"`)), 412, "precondition_failed")
	r = e.do("PUT", path, map[string]any{"weight_kg": 81.9}, h("If-Match", `"2"`))
	if g := expect(t, r, 200); g["weight_kg"] != 81.9 || r.etag() != `"3"` {
		t.Fatalf("conditional replace = %v", g)
	}
	expect(t, e.do("PUT", path, map[string]any{"weight_kg": 81.8}, h("If-Match", "*")), 200)
	expectErr(t, e.do("PUT", "/api/v1/weight/entries/2026-03-09", map[string]any{"weight_kg": 82}, h("If-Match", "*")), 412, "precondition_failed")
	expect(t, e.do("PUT", "/api/v1/weight/entries/2026-03-09", map[string]any{"weight_kg": 82}, h("If-Match", `"0"`)), 201)
	expectErr(t, e.do("PUT", path, map[string]any{"weight_kg": 81.8}, h("If-Match", "zero")), 400, "bad_request")

	// DELETE requires If-Match.
	cur := e.do("GET", path, nil, nil)
	expectErr(t, e.do("DELETE", path, nil, nil), 428, "precondition_required")
	expectErr(t, e.do("DELETE", path, nil, h("If-Match", `"1"`)), 412, "precondition_failed")
	if r := e.do("DELETE", path, nil, h("If-Match", cur.etag())); r.status != 204 {
		t.Fatalf("delete: %d %s", r.status, r.body)
	}
	expectErr(t, e.do("GET", path, nil, nil), 404, "not_found")
	expectErr(t, e.do("DELETE", path, nil, h("If-Match", "*")), 404, "not_found")
	// Weight never creates a workout.
	expectErr(t, e.do("GET", "/api/v1/workouts/today", nil, nil), 404, "not_found")
}

func TestWeighInValidation(t *testing.T) {
	e := newEnv(t)
	put := func(date, body string, hdr map[string]string) resp {
		return e.do("PUT", "/api/v1/weight/entries/"+date, body, hdr)
	}
	for _, tc := range []struct{ date, body, field string }{
		{"today", `{}`, "weight_kg"},
		{"today", `{"note":"x"}`, "weight_kg"},
		{"today", `{"weight_kg":19.9}`, "weight_kg"},
		{"today", `{"weight_kg":400.5}`, "weight_kg"},
		{"today", `{"weight_kg":0}`, "weight_kg"},
		{"today", `{"weight_kg":"80"}`, "weight_kg"},
		{"today", `{"weight_kg":80,"unit":"lb"}`, "unit"},
		{"today", `{"weight_kg":80,"note":"` + strings.Repeat("n", 201) + `"}`, "note"},
		{"2026-03-11", `{"weight_kg":80}`, "date"},
		{"2026-02-30", `{"weight_kg":80}`, "date"},
		{"yesterday", `{"weight_kg":80}`, "date"},
	} {
		hasIssue(t, expectErr(t, put(tc.date, tc.body, nil), 400, "validation_error"), tc.field)
	}
	expectErr(t, put("today", `{"weight_kg":80`, nil), 400, "bad_request")
	expectErr(t, put("today", `{"weight_kg":80}`, h("Content-Type", "text/plain")), 415, "unsupported_media_type")
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/weight/entries/03-10", nil, nil), 400, "validation_error"), "date")
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/weight/entries?from=x", nil, nil), 400, "validation_error"), "from")
	expectErr(t, e.do("GET", "/api/v1/weight/entries?from=2026-03-10&to=2026-03-01", nil, nil), 400, "validation_error")
	for _, q := range []string{"weeks=0", "weeks=105", "weeks=x", "to=tomorrow"} {
		expectErr(t, e.do("GET", "/api/v1/weight/trend?"+q, nil, nil), 400, "validation_error")
	}
	if l := list(expect(t, e.do("GET", "/api/v1/weight/entries", nil, nil), 200), "entries"); len(l) != 0 {
		t.Fatalf("nothing should be stored: %v", l)
	}
}

func TestWeightListAndTrend(t *testing.T) {
	e := newEnv(t)
	// Empty: weeks with null statistics, no latest, no overall change.
	tr := expect(t, e.do("GET", "/api/v1/weight/trend?weeks=2", nil, nil), 200)
	weeks := list(tr, "weeks")
	if tr["from"] != "2026-03-02" || tr["to"] != "2026-03-10" || tr["today"] != "2026-03-10" || len(weeks) != 2 || tr["latest"] != nil || tr["overall_change_kg"] != nil {
		t.Fatalf("empty trend = %v", tr)
	}
	if w0 := weeks[0].(map[string]any); w0["average_kg"] != nil || w0["change_kg"] != nil || num(w0["days_logged"]) != 0 {
		t.Fatalf("empty week = %v", w0)
	}

	for date, kg := range map[string]float64{"2026-02-24": 84, "2026-03-03": 83.4, "2026-03-05": 83, "2026-03-09": 82.5, "2026-03-10": 82.3} {
		expect(t, e.do("PUT", "/api/v1/weight/entries/"+date, map[string]any{"weight_kg": kg}, nil), 201)
	}
	l := list(expect(t, e.do("GET", "/api/v1/weight/entries?from=2026-03-01&to=today", nil, nil), 200), "entries")
	if len(l) != 4 || l[0].(map[string]any)["date"] != "2026-03-03" || l[3].(map[string]any)["date"] != "2026-03-10" {
		t.Fatalf("list oldest first = %v", l)
	}

	tr = expect(t, e.do("GET", "/api/v1/weight/trend?weeks=2&to=today", nil, nil), 200)
	weeks = list(tr, "weeks")
	w0, w1 := weeks[0].(map[string]any), weeks[1].(map[string]any)
	// Week of Mar 2: avg(83.4, 83) = 83.2, vs Feb 23 week (84) -> -0.8.
	if w0["week_start"] != "2026-03-02" || w0["week_end"] != "2026-03-08" || num(w0["days_logged"]) != 2 || w0["average_kg"] != 83.2 ||
		w0["min_kg"] != 83.0 || w0["max_kg"] != 83.4 || w0["change_kg"] != -0.8 || w0["trend"] != "down" {
		t.Fatalf("week 1 = %v", w0)
	}
	// Week of Mar 9: avg(82.5, 82.3) = 82.4 -> -0.8 vs 83.2.
	if w1["average_kg"] != 82.4 || w1["change_kg"] != -0.8 || w1["change_pct"] != -0.96 || w1["trend"] != "down" {
		t.Fatalf("week 2 = %v", w1)
	}
	if tr["overall_change_kg"] != -0.8 || len(list(tr, "entries")) != 4 || tr["latest"].(map[string]any)["date"] != "2026-03-10" {
		t.Fatalf("trend = %v", tr)
	}
	// A week without weigh-ins breaks the comparison instead of inventing one.
	tr = expect(t, e.do("GET", "/api/v1/weight/trend?weeks=3&to=2026-03-01", nil, nil), 200)
	weeks = list(tr, "weeks")
	if len(weeks) != 3 || weeks[2].(map[string]any)["average_kg"] != 84.0 || weeks[2].(map[string]any)["change_kg"] != nil ||
		tr["latest"].(map[string]any)["date"] != "2026-02-24" {
		t.Fatalf("sparse trend = %v", tr)
	}
}

// noWeight exposes only the base store, hiding the weigh-in methods.
type noWeight struct{ training.Store }

func TestWeightDisabled(t *testing.T) {
	base := newEnv(t)
	svc := training.NewService(noWeight{base.st}, func() time.Time { return time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC) }).WithLocation(jerusalem)
	api := New(svc)
	api.logger = log.New(&bytes.Buffer{}, "", 0)
	srv := httptest.NewServer(httpsrv.RoutesWithAPI(http.NotFoundHandler(), api.Handler(), false, testToken, nil))
	t.Cleanup(srv.Close)
	e := &env{t: t, svc: svc, st: base.st, api: api, srv: srv, validate: true}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/weight/entries"},
		{"GET", "/api/v1/weight/entries/today"},
		{"GET", "/api/v1/weight/trend"},
		{"DELETE", "/api/v1/weight/entries/today"},
	} {
		expectErr(t, e.do(c.method, c.path, nil, h("If-Match", "*")), 501, "weight_disabled")
	}
	expectErr(t, e.do("PUT", "/api/v1/weight/entries/today", map[string]any{"weight_kg": 80}, nil), 501, "weight_disabled")
}
