package apisrv

import (
	"sync"
	"testing"
)

// Endpoints added for the web UI: open an empty workout, add a library
// exercise, edit a prescription's note.

func TestEnsureWorkoutIsIdempotentGetOrCreate(t *testing.T) {
	e := newEnv(t)
	if r := e.do("GET", "/api/v1/workouts/2026-03-09", nil, nil); r.status != 404 {
		t.Fatalf("no workout yet, got %d", r.status)
	}
	first := expect(t, e.do("PUT", "/api/v1/workouts/2026-03-09", nil, nil), 201)
	if first["date"] != "2026-03-09" || len(list(first, "sets")) != 0 {
		t.Fatalf("created workout: %v", first)
	}
	again := expect(t, e.do("PUT", "/api/v1/workouts/today", nil, nil), 201)
	if again["date"] != "2026-03-10" {
		t.Fatalf("today alias: %v", again)
	}
	r := e.do("PUT", "/api/v1/workouts/2026-03-09", nil, nil)
	if r.status != 200 || num(r.obj(t)["id"]) != num(first["id"]) || r.etag() == "" {
		t.Fatalf("second call must return the same workout with 200: %d %s", r.status, r.body)
	}
	if r := e.do("PUT", "/api/v1/workouts/not-a-date", nil, nil); r.status != 400 {
		t.Fatalf("bad date: %d", r.status)
	}
}

func TestEnsureWorkoutConcurrentCallsCreateOne(t *testing.T) {
	e := newEnv(t)
	e.validate = false // concurrent use of the shared validator is not the point here
	var wg sync.WaitGroup
	ids := make(chan int, 10)
	created := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := e.do("PUT", "/api/v1/workouts/2026-03-08", nil, nil)
			created <- r.status == 201
			ids <- num(r.obj(t)["id"])
		}()
	}
	wg.Wait()
	close(ids)
	close(created)
	first, n := 0, 0
	for id := range ids {
		if first == 0 {
			first = id
		}
		if id != first {
			t.Fatalf("all callers must get the same workout")
		}
	}
	for c := range created {
		if c {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("exactly one call creates the workout, got %d", n)
	}
}

func TestCreateExerciseNeverMergesSilently(t *testing.T) {
	e := newEnv(t)
	r := expect(t, e.do("POST", "/api/v1/exercises", map[string]any{"name": "  Bench   Press "}, nil), 201)
	ex := r["exercise"].(map[string]any)
	if ex["name"] != "bench press" || r["created"] != true || len(list(r, "similar")) != 0 {
		t.Fatalf("create: %v", r)
	}
	again := expect(t, e.do("POST", "/api/v1/exercises", map[string]any{"name": "bench press"}, nil), 200)
	if again["created"] != false || num(again["exercise"].(map[string]any)["id"]) != num(ex["id"]) {
		t.Fatalf("an existing name returns the same exercise: %v", again)
	}
	typo := expect(t, e.do("POST", "/api/v1/exercises", map[string]any{"name": "bench pres"}, nil), 201)
	if typo["created"] != true || len(list(typo, "similar")) != 1 || list(typo, "similar")[0] != "bench press" {
		t.Fatalf("a near miss is created separately and reported: %v", typo)
	}
	er := expectErr(t, e.do("POST", "/api/v1/exercises", map[string]any{"name": ""}, nil), 400, "validation_error")
	hasIssue(t, er, "name")
	er = expectErr(t, e.do("POST", "/api/v1/exercises", map[string]any{"name": "x", "muscle": "chest"}, nil), 400, "validation_error")
	hasIssue(t, er, "muscle")
}

func TestPrescriptionNoteCanBeSetAndCleared(t *testing.T) {
	e := newEnv(t)
	w := expect(t, e.do("POST", "/api/v1/workouts/today/sets", map[string]any{
		"prescriptions": []any{map[string]any{"exercise": "squat", "target_sets": 3}},
	}, nil), 201)["workout"].(map[string]any)
	etag := quoted(num(w["revision"]))
	r := e.do("PATCH", "/api/v1/workouts/today/prescriptions/squat", map[string]any{"notes": "belt on the top set"}, h("If-Match", etag, "Content-Type", "application/merge-patch+json"))
	got := expect(t, r, 200)
	p := list(got, "progress")[0].(map[string]any)
	if p["notes"] != "belt on the top set" {
		t.Fatalf("note: %v", p)
	}
	r = e.do("PATCH", "/api/v1/workouts/today/prescriptions/squat", map[string]any{"notes": nil}, h("If-Match", r.etag(), "Content-Type", "application/merge-patch+json"))
	p = list(expect(t, r, 200), "progress")[0].(map[string]any)
	if _, has := p["notes"]; has && p["notes"] != "" {
		t.Fatalf("null must clear the note: %v", p)
	}
}
