package integration

import (
	"fmt"
	"sync"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

func TestOneWorkoutPerLocalDate(t *testing.T) {
	e := newEnv(t)

	first := e.log(batch("2026-03-10", set("bench press", 60, 8)))
	if !first.Created {
		t.Fatal("first write must create the workout")
	}
	// A later "conversation" logging for the same date, by explicit date and
	// by the implicit today, must land in the same workout.
	second := e.log(batch("2026-03-10", set("squat", 100, 5)))
	third := e.log(batch("", set("bench press", 60, 8)))
	ensured, created, err := e.svc.EnsureWorkout(ctx(), "2026-03-10")
	if err != nil {
		t.Fatal(err)
	}
	started, err := e.svc.StartSession(ctx(), "2026-03-10")
	if err != nil {
		t.Fatal(err)
	}
	if second.Created || third.Created || created {
		t.Fatal("later writes must reuse the workout, not create one")
	}
	for _, id := range []int64{second.Workout.ID, third.Workout.ID, ensured.ID, started.ID} {
		if id != first.Workout.ID {
			t.Fatalf("workout id %d differs from %d", id, first.Workout.ID)
		}
	}

	w := e.workout("2026-03-10")
	if len(w.Sets) != 3 {
		t.Fatalf("want 3 sets in the single workout, got %d", len(w.Sets))
	}
	for i, st := range w.Sets {
		if st.Position != i+1 {
			t.Fatalf("set %d has position %d", i, st.Position)
		}
	}
	if got := e.count(`SELECT COUNT(*) FROM sessions WHERE date='2026-03-10'`); got != 1 {
		t.Fatalf("want 1 workout row, got %d", got)
	}

	other := e.log(batch("2026-03-08", set("deadlift", 140, 3)))
	if !other.Created || other.Workout.ID == first.Workout.ID {
		t.Fatal("a different date must get its own workout")
	}
	if got := e.count(`SELECT COUNT(*) FROM sessions`); got != 2 {
		t.Fatalf("want 2 workouts, got %d", got)
	}
	if len(e.workout("2026-03-08").Sets) != 1 || len(e.workout("2026-03-10").Sets) != 3 {
		t.Fatal("sets leaked between dates")
	}
	list, err := e.svc.ListSessions(ctx(), training.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Date != "2026-03-10" || list[0].SetCount != 3 {
		t.Fatalf("unexpected session list: %+v", list)
	}
}

func TestConcurrentFirstWritesCreateOneWorkout(t *testing.T) {
	e := newEnv(t)
	const writers = 24
	const date = "2026-03-09"
	var wg sync.WaitGroup
	errs := make(chan error, writers*2)
	ids := make(chan int64, writers*2)
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			<-start
			res, err := e.svc.LogSets(ctx(), batch(date, set("bench press", 60, i+1)))
			if err != nil {
				errs <- fmt.Errorf("writer %d: %w", i, err)
				return
			}
			ids <- res.Workout.ID
		}(i)
		go func(i int) {
			defer wg.Done()
			<-start
			w, _, err := e.svc.EnsureWorkout(ctx(), date)
			if err != nil {
				errs <- fmt.Errorf("ensure %d: %w", i, err)
				return
			}
			ids <- w.ID
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Error(err)
	}
	var id int64
	for got := range ids {
		if id == 0 {
			id = got
		}
		if got != id {
			t.Fatalf("callers saw different workouts: %d vs %d", got, id)
		}
	}
	if got := e.count(`SELECT COUNT(*) FROM sessions WHERE date=?`, date); got != 1 {
		t.Fatalf("want exactly one workout row, got %d", got)
	}
	w := e.workout(date)
	if len(w.Sets) != writers {
		t.Fatalf("want %d sets, got %d", writers, len(w.Sets))
	}
	reps := map[int]bool{}
	for i, st := range w.Sets {
		if st.Position != i+1 {
			t.Fatalf("positions not dense: set %d at %d", i, st.Position)
		}
		reps[*st.Reps] = true
	}
	if len(reps) != writers {
		t.Fatalf("lost or duplicated sets: %d distinct", len(reps))
	}
}

func TestDateBoundariesInJerusalem(t *testing.T) {
	cases := []struct {
		name, utc, want string
	}{
		{"winter evening before local midnight", "2026-03-10T21:59:59Z", "2026-03-10"},
		{"winter: 21:30Z is already tomorrow (UTC+2)", "2026-03-10T22:30:00Z", "2026-03-11"},
		{"winter local midnight exactly", "2026-03-10T22:00:00Z", "2026-03-11"},
		{"day before DST starts, last second", "2026-03-26T21:59:59Z", "2026-03-26"},
		{"DST start day begins at 22:00Z", "2026-03-26T22:00:00Z", "2026-03-27"},
		{"DST day just before midnight (UTC+3)", "2026-03-27T20:59:59Z", "2026-03-27"},
		{"after DST, 21:00Z is already the next day", "2026-03-27T21:00:00Z", "2026-03-28"},
		{"summer: 21:30Z next day", "2026-03-28T21:30:00Z", "2026-03-29"},
		{"DST end night, midnight is still UTC+3", "2026-10-24T21:00:00Z", "2026-10-25"},
		{"after DST ends, 21:30Z still same day (UTC+2)", "2026-10-25T21:30:00Z", "2026-10-25"},
		{"after DST ends, 22:00Z next day", "2026-10-25T22:00:00Z", "2026-10-26"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.at(tc.utc)
			if got := e.svc.Today(); got != tc.want {
				t.Fatalf("Today() = %s, want %s", got, tc.want)
			}
			res := e.log(batch("", set("bench press", 60, 8)))
			if res.Workout.Date != tc.want {
				t.Fatalf("unset date resolved to %s, want %s", res.Workout.Date, tc.want)
			}
			// Logging explicitly for the local date is "today", never "future",
			// even though the UTC date may still be the day before.
			if _, err := e.svc.LogSets(ctx(), batch(tc.want, set("bench press", 60, 8))); err != nil {
				t.Fatalf("explicit local date rejected: %v", err)
			}
			w := e.workout("")
			if w.Date != tc.want || len(w.Sets) != 2 {
				t.Fatalf("workout %s with %d sets", w.Date, len(w.Sets))
			}
		})
	}

	t.Run("a workout started before local midnight stays on its date", func(t *testing.T) {
		e := newEnv(t)
		e.at("2026-03-10T21:50:00Z") // 23:50 local
		before := e.log(batch("", set("bench press", 60, 8)))
		e.at("2026-03-10T22:10:00Z") // 00:10 local, next day
		after := e.log(batch("", set("bench press", 60, 8)))
		if before.Workout.Date != "2026-03-10" || after.Workout.Date != "2026-03-11" || before.Workout.ID == after.Workout.ID {
			t.Fatalf("before=%s after=%s", before.Workout.Date, after.Workout.Date)
		}
		// The past date can still be appended to explicitly.
		e.log(batch("2026-03-10", set("squat", 100, 5)))
		if len(e.workout("2026-03-10").Sets) != 2 {
			t.Fatal("explicit append to yesterday failed")
		}
	})
}

func TestExplicitHistoricalDates(t *testing.T) {
	e := newEnv(t) // today is 2026-03-10 in Jerusalem
	res := e.log(batch("2025-12-31", set("bench press", 70, 5), set("bench press", 70, 5)))
	if res.Workout.Date != "2025-12-31" {
		t.Fatalf("logged on %s", res.Workout.Date)
	}
	if _, _, err := e.svc.UpdateSet(ctx(), res.Sets[1].ID, training.SetPatch{Reps: n(4)}); err != nil {
		t.Fatal(err)
	}
	w := e.workout("2025-12-31")
	if *w.Sets[0].Reps != 5 || *w.Sets[1].Reps != 4 {
		t.Fatalf("historical correction not persisted: %+v", w.Sets)
	}
	if _, err := e.svc.GetWorkout(ctx(), ""); err == nil {
		t.Fatal("logging a past date must not create today's workout")
	}

	// Future dates: sets rejected (nothing created), prescriptions allowed.
	_, err := e.svc.LogSets(ctx(), batch("2026-03-11", set("bench press", 60, 8)))
	requireFields(t, err, "date")
	if got := e.count(`SELECT COUNT(*) FROM sessions WHERE date='2026-03-11'`); got != 0 {
		t.Fatal("rejected future write created a workout")
	}
	plan := e.log(training.BatchInput{Date: "2026-03-12", Prescriptions: []training.PlanItem{rx("squat", 3, 5, 5, f(120))}})
	if plan.Workout.Date != "2026-03-12" || len(plan.Sets) != 0 || len(plan.Prescriptions) != 1 {
		t.Fatalf("future prescription: %+v", plan)
	}

	for _, bad := range []string{"2026-02-30", "10/03/2026", "2026-3-1"} {
		_, err := e.svc.LogSets(ctx(), batch(bad, set("bench press", 60, 8)))
		requireFields(t, err, "date")
	}
}
