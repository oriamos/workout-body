package integration

import (
	"errors"
	"sync"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

func TestOptimisticConcurrency(t *testing.T) {
	e := newEnv(t)
	res := e.log(batch("", set("bench press", 60, 8), set("bench press", 60, 8)))
	id := res.Sets[0].ID
	stale := 0

	t.Run("UpdateSet", func(t *testing.T) {
		_, _, err := e.svc.UpdateSet(ctx(), id, training.SetPatch{Reps: n(5), ExpectedRevision: &stale})
		wantErr(t, err, training.ErrConflict)
		if got := e.set(id); *got.Reps != 8 || got.Revision != 1 {
			t.Fatalf("stale update applied: %+v", got)
		}
		one := 1
		upd, _, err := e.svc.UpdateSet(ctx(), id, training.SetPatch{Reps: n(6), ExpectedRevision: &one})
		if err != nil || upd.Revision != 2 || *upd.Reps != 6 {
			t.Fatalf("matching revision: %v %+v", err, upd)
		}
		// The old revision is now stale.
		_, _, err = e.svc.UpdateSet(ctx(), id, training.SetPatch{Reps: n(7), ExpectedRevision: &one})
		wantErr(t, err, training.ErrConflict)
		if *e.set(id).Reps != 6 {
			t.Fatal("second stale update applied")
		}
	})

	t.Run("DeleteSet", func(t *testing.T) {
		other := res.Sets[1].ID
		_, _, _, err := e.svc.DeleteSet(ctx(), other, &stale)
		wantErr(t, err, training.ErrConflict)
		if len(e.workout("").Sets) != 2 {
			t.Fatal("stale delete removed the set")
		}
		rev := e.set(other).Revision
		if _, _, _, err := e.svc.DeleteSet(ctx(), other, &rev); err != nil {
			t.Fatal(err)
		}
		if len(e.workout("").Sets) != 1 {
			t.Fatal("matching delete did not remove the set")
		}
	})

	t.Run("LogSets", func(t *testing.T) {
		w := e.workout("")
		old := w.Revision - 1
		in := batch("", set("bench press", 60, 8))
		in.ExpectedRevision = &old
		_, err := e.svc.LogSets(ctx(), in)
		wantErr(t, err, training.ErrConflict)
		after := e.workout("")
		if after.Revision != w.Revision || len(after.Sets) != len(w.Sets) {
			t.Fatal("stale batch changed the workout")
		}
		in.ExpectedRevision = &w.Revision
		r := e.log(in)
		if r.Workout.Revision != w.Revision+1 || len(r.Workout.Sets) != len(w.Sets)+1 {
			t.Fatalf("matching batch: rev %d sets %d", r.Workout.Revision, len(r.Workout.Sets))
		}
	})

	t.Run("ReplacePlan", func(t *testing.T) {
		p, err := e.svc.CreatePlan(ctx(), training.Plan{Name: "P", Items: []training.PlanItem{{Exercise: "squat", TargetSets: 3}}})
		if err != nil {
			t.Fatal(err)
		}
		doc := []byte(`{"format":"workout-plan","version":1,"name":"P2","items":[{"exercise":"lunge","sets":2}]}`)
		old := p.Revision + 5
		_, err = e.svc.ReplacePlan(ctx(), p.ID, doc, &old)
		wantErr(t, err, training.ErrConflict)
		if got, _ := e.svc.GetPlan(ctx(), p.ID); got.Name != "P" || got.Revision != p.Revision || got.Items[0].Exercise != "squat" {
			t.Fatalf("stale replace applied: %+v", got)
		}
		got, err := e.svc.ReplacePlan(ctx(), p.ID, doc, &p.Revision)
		if err != nil || got.Name != "P2" || got.Revision != p.Revision+1 || got.Items[0].Exercise != "lunge" {
			t.Fatalf("matching replace: %v %+v", err, got)
		}
	})
}

func TestConcurrentUpdatersExactlyOneWins(t *testing.T) {
	e := newEnv(t)
	res := e.log(batch("", set("bench press", 60, 8)))
	id := res.Sets[0].ID
	const racers = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, conflicts := 0, 0
	var winner int
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rev := 1
			<-start
			_, _, err := e.svc.UpdateSet(ctx(), id, training.SetPatch{Reps: n(10 + i), ExpectedRevision: &rev})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
				winner = 10 + i
			case errors.Is(err, training.ErrConflict):
				conflicts++
			default:
				t.Errorf("racer %d: %v", i, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if wins != 1 || conflicts != racers-1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
	got := e.set(id)
	if got.Revision != 2 || *got.Reps != winner {
		t.Fatalf("final set %+v, winner wrote %d", got, winner)
	}
}
