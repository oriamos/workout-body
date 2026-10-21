package integration

import "testing"

func TestSmokeLogAndRead(t *testing.T) {
	e := newEnv(t)
	res, err := e.svc.LogSets(ctx(), batch("", set("bench press", 60, 8)))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || len(res.Sets) != 1 || res.Sets[0].ID == 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	w, err := e.svc.GetWorkout(ctx(), "")
	if err != nil {
		t.Fatal(err)
	}
	if w.Date != "2026-03-10" || len(w.Sets) != 1 || w.Sets[0].SetNumber != 1 {
		t.Fatalf("unexpected workout: %+v", w)
	}
}
