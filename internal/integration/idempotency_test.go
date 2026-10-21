package integration

import (
	"sync"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

func TestIdempotentReplay(t *testing.T) {
	e := newEnv(t)
	in := batch("", set("bench press", 60, 8), set("bench press", 60, 8))
	in.IdempotencyKey = "msg-42"
	first := e.log(in)
	if first.Replayed || len(first.Sets) != 2 {
		t.Fatalf("first: %+v", first)
	}
	// Another write in between does not disturb the replay.
	e.log(batch("", set("squat", 100, 5)))

	again := e.log(in)
	if !again.Replayed {
		t.Fatal("retry with the same key must be a replay")
	}
	if len(again.Sets) != 2 || again.Sets[0].ID != first.Sets[0].ID || again.Sets[1].ID != first.Sets[1].ID {
		t.Fatalf("replay returned different sets: %+v", again.Sets)
	}
	if again.Workout.ID != first.Workout.ID || len(again.Workout.Sets) != 3 {
		t.Fatalf("replay workout: %+v", again.Workout)
	}
	if got := e.count(`SELECT COUNT(*) FROM sets`); got != 3 {
		t.Fatalf("replay duplicated sets: %d rows", got)
	}
	// Normalization happens before hashing, so cosmetic differences replay.
	cosmetic := batch(e.svc.Today(), set("  Bench   PRESS ", 60, 8), set("bench press", 60, 8))
	cosmetic.IdempotencyKey = "msg-42"
	if r := e.log(cosmetic); !r.Replayed {
		t.Fatal("same normalized payload must replay")
	}

	// Same key, different payload: conflict, nothing written.
	diff := batch("", set("bench press", 60, 9))
	diff.IdempotencyKey = "msg-42"
	_, err := e.svc.LogSets(ctx(), diff)
	wantErr(t, err, training.ErrConflict)
	if got := e.count(`SELECT COUNT(*) FROM sets`); got != 3 {
		t.Fatalf("conflicting key wrote sets: %d rows", got)
	}
}

func TestIntentionalDuplicatesAreKept(t *testing.T) {
	e := newEnv(t)
	e.log(batch("", set("bench press", 60, 8)))
	e.log(batch("", set("bench press", 60, 8))) // no key: a genuine second set
	k1 := batch("", set("bench press", 60, 8))
	k1.IdempotencyKey = "a"
	k2 := k1
	k2.IdempotencyKey = "b"
	if e.log(k1).Replayed || e.log(k2).Replayed {
		t.Fatal("different keys are different writes")
	}
	if got := len(e.workout("").Sets); got != 4 {
		t.Fatalf("want 4 identical sets stored, got %d", got)
	}
	// LogSet with count is the same as a batch of copies.
	res, err := e.svc.LogSet(ctx(), "", set("bench press", 60, 8), 3, "")
	if err != nil || len(res.Sets) != 3 {
		t.Fatalf("LogSet count: %v %+v", err, res.Sets)
	}
	if got := len(e.workout("").Sets); got != 7 {
		t.Fatalf("want 7, got %d", got)
	}
}

func TestIdempotencyKeyAfterFailedAttempt(t *testing.T) {
	e := newEnv(t)
	bad := batch("2026-03-09", set("bench press", 60, 8), training.AddSetInput{Exercise: "bench press", Reps: n(-3)})
	bad.IdempotencyKey = "retry-1"
	_, err := e.svc.LogSets(ctx(), bad)
	requireFields(t, err, "sets[1].reps")

	// Failure inside the transaction must not consume the key either.
	inner := batch("2026-03-09", set("bench press", 60, 8), training.AddSetInput{ExerciseID: 777})
	inner.IdempotencyKey = "retry-1"
	_, err = e.svc.LogSets(ctx(), inner)
	wantErr(t, err, training.ErrNotFound)
	for _, table := range []string{"sessions", "sets", "idempotency_keys", "exercises"} {
		if got := e.count(`SELECT COUNT(*) FROM ` + table); got != 0 {
			t.Fatalf("failed attempts left %d rows in %s", got, table)
		}
	}

	good := batch("2026-03-09", set("bench press", 60, 8), set("bench press", 60, 7))
	good.IdempotencyKey = "retry-1"
	res := e.log(good)
	if res.Replayed || !res.Created || len(res.Sets) != 2 {
		t.Fatalf("corrected retry: %+v", res)
	}
	if r := e.log(good); !r.Replayed || r.Sets[1].ID != res.Sets[1].ID {
		t.Fatal("second retry of the corrected payload must replay")
	}
	if got := len(e.workout("2026-03-09").Sets); got != 2 {
		t.Fatalf("want 2 sets, got %d", got)
	}
}

func TestConcurrentRetriesWithOneKeyWriteOnce(t *testing.T) {
	e := newEnv(t)
	in := batch("2026-03-09", set("bench press", 60, 8), set("bench press", 60, 8))
	in.IdempotencyKey = "flaky-network"
	const retries = 10
	results := make(chan training.BatchResult, retries)
	errs := make(chan error, retries)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < retries; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, err := e.svc.LogSets(ctx(), in)
			if err != nil {
				errs <- err
				return
			}
			results <- r
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	fresh := 0
	var ids []int64
	for r := range results {
		if !r.Replayed {
			fresh++
		}
		if ids == nil {
			ids = []int64{r.Sets[0].ID, r.Sets[1].ID}
		} else if r.Sets[0].ID != ids[0] || r.Sets[1].ID != ids[1] {
			t.Fatal("retries returned different sets")
		}
	}
	if fresh != 1 {
		t.Fatalf("want exactly one real write, got %d", fresh)
	}
	if got := e.count(`SELECT COUNT(*) FROM sets`); got != 2 {
		t.Fatalf("want 2 sets, got %d", got)
	}
}
