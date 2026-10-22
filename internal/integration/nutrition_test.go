package integration

import (
	"errors"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/fullfran/training-mcp/internal/training"
)

func meal(name string, kcal, p, c, fat float64) training.NutritionInput {
	return training.NutritionInput{Name: name, Nutrients: training.Nutrients{CaloriesKcal: f(kcal), ProteinG: f(p), CarbsG: f(c), FatG: f(fat)}}
}

func addMeal(t *testing.T, e *env, in training.NutritionInput) training.NutritionEntry {
	t.Helper()
	out, _, err := e.svc.AddNutritionEntry(ctx(), in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func day(t *testing.T, e *env, date string) training.NutritionDay {
	t.Helper()
	d, err := e.svc.NutritionDayOn(ctx(), date)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestNutritionEntryLifecycleAndTotalsAfterEveryMutation(t *testing.T) {
	e := newEnv(t)
	oats := addMeal(t, e, meal("  Oatmeal   with  berries ", 350, 12.5, 58, 7.25))
	if oats.ID == 0 || oats.Name != "Oatmeal with berries" || oats.Date != "2026-03-10" || oats.Revision != 1 ||
		oats.CreatedAt != "2026-03-10T12:00:00Z" || oats.UpdatedAt != oats.CreatedAt {
		t.Fatalf("created entry: %+v", oats)
	}
	if *oats.ProteinG != 12.5 || *oats.FatG != 7.25 {
		t.Fatalf("decimals must be preserved: %+v", oats.Nutrients)
	}
	chicken := addMeal(t, e, meal("Chicken and rice", 650, 48, 70, 14))
	d := day(t, e, "")
	if d.EntryCount != 2 || d.Calories.Consumed != 1000 || d.Protein.Consumed != 60.5 || d.Fat.Consumed != 21.25 {
		t.Fatalf("totals after two entries: %+v", d)
	}
	if d.Entries[0].ID != oats.ID || d.Entries[1].ID != chicken.ID {
		t.Fatal("entries must be listed in the order they were logged")
	}

	// Partial update keeps omitted fields.
	e.now = e.now.Add(time.Hour)
	up, err := e.svc.UpdateNutritionEntry(ctx(), chicken.ID, training.NutritionPatch{Nutrients: training.Nutrients{CaloriesKcal: f(700)}, ExpectedRevision: &chicken.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if *up.CaloriesKcal != 700 || *up.ProteinG != 48 || up.Name != "Chicken and rice" || up.Revision != 2 || up.UpdatedAt == up.CreatedAt {
		t.Fatalf("partial update: %+v", up)
	}
	if d = day(t, e, ""); d.Calories.Consumed != 1050 {
		t.Fatalf("totals must follow the edit: %v", d.Calories.Consumed)
	}

	// Delete, then the totals are recomputed from what is left.
	if err := e.svc.DeleteNutritionEntry(ctx(), oats.ID, &oats.Revision); err != nil {
		t.Fatal(err)
	}
	d = day(t, e, "")
	if d.EntryCount != 1 || d.Calories.Consumed != 700 || d.Protein.Consumed != 48 {
		t.Fatalf("totals after delete: %+v", d)
	}
	if _, err := e.svc.GetNutritionEntry(ctx(), oats.ID); !errors.Is(err, training.ErrNotFound) {
		t.Fatalf("deleted entry: %v", err)
	}
}

func TestNutritionZeroIsNotMissing(t *testing.T) {
	e := newEnv(t)
	coffee := addMeal(t, e, training.NutritionInput{Name: "Black coffee", Nutrients: training.Nutrients{CaloriesKcal: f(0), FatG: f(0)}})
	if coffee.CaloriesKcal == nil || *coffee.CaloriesKcal != 0 || coffee.ProteinG != nil || coffee.CarbsG != nil {
		t.Fatalf("explicit 0 kept, missing kept missing: %+v", coffee.Nutrients)
	}
	got, _ := e.svc.GetNutritionEntry(ctx(), coffee.ID)
	if got.CaloriesKcal == nil || got.ProteinG != nil {
		t.Fatalf("after a round trip: %+v", got.Nutrients)
	}
	addMeal(t, e, meal("Toast", 120, 4, 22, 1.5))
	d := day(t, e, "")
	if d.Calories.Consumed != 120 || d.Calories.MissingEntries != 0 || d.Protein.Consumed != 4 || d.Protein.MissingEntries != 1 {
		t.Fatalf("a missing value is reported, not counted as zero: cal %+v protein %+v", d.Calories, d.Protein)
	}
	// Clearing turns a value back into "not provided".
	up, err := e.svc.UpdateNutritionEntry(ctx(), coffee.ID, training.NutritionPatch{ClearFat: true})
	if err != nil || up.FatG != nil {
		t.Fatalf("clear: %+v %v", up, err)
	}
	// Calories are never derived from macros.
	odd := addMeal(t, e, meal("Label says", 999, 1, 1, 1))
	if *odd.CaloriesKcal != 999 {
		t.Fatal("supplied calories must be preserved as given")
	}
}

func TestNutritionRejectsInvalidNumbersAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	for name, tc := range map[string]struct {
		in    training.NutritionInput
		field string
	}{
		"negative":  {meal("x", -1, 0, 0, 0), "calories_kcal"},
		"NaN":       {training.NutritionInput{Name: "x", Nutrients: training.Nutrients{ProteinG: f(math.NaN())}}, "protein_g"},
		"infinite":  {training.NutritionInput{Name: "x", Nutrients: training.Nutrients{CarbsG: f(math.Inf(1))}}, "carbs_g"},
		"too large": {training.NutritionInput{Name: "x", Nutrients: training.Nutrients{FatG: f(5000)}}, "fat_g"},
		"no name":   {meal("   ", 1, 1, 1, 1), "name"},
		"future":    {training.NutritionInput{Name: "x", Date: "2026-03-11"}, "date"},
		"bad date":  {training.NutritionInput{Name: "x", Date: "10/03/2026"}, "date"},
	} {
		_, _, err := e.svc.AddNutritionEntry(ctx(), tc.in)
		requireFields(t, err, tc.field)
		_ = name
	}
	if list, _ := e.svc.ListNutritionEntries(ctx(), training.NutritionFilter{}); len(list) != 0 {
		t.Fatalf("invalid input must store nothing: %+v", list)
	}
	ok := addMeal(t, e, meal("Apple", 95, 0.5, 25, 0.3))
	_, err := e.svc.UpdateNutritionEntry(ctx(), ok.ID, training.NutritionPatch{Nutrients: training.Nutrients{ProteinG: f(-3)}, ClearCalories: true})
	requireFields(t, err, "protein_g")
	if got, _ := e.svc.GetNutritionEntry(ctx(), ok.ID); got.CaloriesKcal == nil || got.Revision != 1 {
		t.Fatal("a rejected update must change nothing")
	}
}

func TestNutritionRetryIsIdempotentButDuplicatesAreAllowed(t *testing.T) {
	e := newEnv(t)
	in := meal("Protein shake", 180, 30, 8, 3)
	in.IdempotencyKey = "shake-2026-03-10-1"
	first, replayed, err := e.svc.AddNutritionEntry(ctx(), in)
	if err != nil || replayed {
		t.Fatal(err)
	}
	e.now = e.now.Add(time.Minute) // a retry a minute later is still the same write
	again, replayed, err := e.svc.AddNutritionEntry(ctx(), in)
	if err != nil || !replayed || again.ID != first.ID {
		t.Fatalf("retry must replay: %+v replayed=%v %v", again, replayed, err)
	}
	// Ten concurrent retries still write once.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _, _ = e.svc.AddNutritionEntry(ctx(), in) }()
	}
	wg.Wait()
	if d := day(t, e, ""); d.EntryCount != 1 {
		t.Fatalf("retries duplicated the entry: %d", d.EntryCount)
	}
	// Same key, different payload: conflict.
	other := meal("Protein shake", 360, 60, 16, 6)
	other.IdempotencyKey = in.IdempotencyKey
	if _, _, err := e.svc.AddNutritionEntry(ctx(), other); !errors.Is(err, training.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	// Intentional duplicates (no key, or a new key) are separate entries.
	addMeal(t, e, meal("Protein shake", 180, 30, 8, 3))
	second := meal("Protein shake", 180, 30, 8, 3)
	second.IdempotencyKey = "shake-2026-03-10-2"
	addMeal(t, e, second)
	if d := day(t, e, ""); d.EntryCount != 3 || d.Protein.Consumed != 90 {
		t.Fatalf("intentional duplicates: %+v", d)
	}
}

func TestNutritionCopyCreatesAnIndependentEntry(t *testing.T) {
	e := newEnv(t)
	src := addMeal(t, e, training.NutritionInput{Date: "2026-03-08", Name: "Lentil soup", Nutrients: training.Nutrients{CaloriesKcal: f(320), ProteinG: f(18)}})
	cp, replayed, err := e.svc.CopyNutritionEntry(ctx(), src.ID, "", "copy-soup-1")
	if err != nil || replayed {
		t.Fatal(err)
	}
	if cp.ID == src.ID || cp.Date != "2026-03-10" || cp.Name != src.Name || *cp.CaloriesKcal != 320 || cp.CarbsG != nil || cp.CopiedFrom == nil || *cp.CopiedFrom != src.ID {
		t.Fatalf("copy: %+v", cp)
	}
	if again, replayed, _ := e.svc.CopyNutritionEntry(ctx(), src.ID, "", "copy-soup-1"); !replayed || again.ID != cp.ID {
		t.Fatal("a retried copy must not create a second copy")
	}
	// Editing the copy never touches the original, and vice versa.
	if _, err := e.svc.UpdateNutritionEntry(ctx(), cp.ID, training.NutritionPatch{Nutrients: training.Nutrients{CaloriesKcal: f(400)}}); err != nil {
		t.Fatal(err)
	}
	if orig, _ := e.svc.GetNutritionEntry(ctx(), src.ID); *orig.CaloriesKcal != 320 || orig.Revision != 1 {
		t.Fatalf("the original changed: %+v", orig)
	}
	// Deleting the original keeps the copy.
	if err := e.svc.DeleteNutritionEntry(ctx(), src.ID, nil); err != nil {
		t.Fatal(err)
	}
	kept, err := e.svc.GetNutritionEntry(ctx(), cp.ID)
	if err != nil || *kept.CaloriesKcal != 400 || kept.CopiedFrom != nil {
		t.Fatalf("copy after the original was deleted: %+v %v", kept, err)
	}
	if _, _, err := e.svc.CopyNutritionEntry(ctx(), 999, "", ""); !errors.Is(err, training.ErrNotFound) {
		t.Fatalf("copy of a missing entry: %v", err)
	}
	if _, _, err := e.svc.CopyNutritionEntry(ctx(), cp.ID, "2026-04-01", ""); !errors.Is(err, training.ErrValidation) {
		t.Fatalf("copy into the future: %v", err)
	}
}

func TestNutritionUsesTheLocalConsumptionDate(t *testing.T) {
	e := newEnv(t)
	// 22:30 UTC on 10 March is 00:30 on 11 March in Jerusalem.
	e.now = time.Date(2026, 3, 10, 22, 30, 0, 0, time.UTC)
	late := addMeal(t, e, meal("Midnight snack", 200, 5, 20, 10))
	if late.Date != "2026-03-11" || late.CreatedAt != "2026-03-10T22:30:00Z" {
		t.Fatalf("summaries use the local date, timestamps stay UTC: %+v", late)
	}
	// An explicit historical date wins over the creation time.
	old := addMeal(t, e, training.NutritionInput{Date: "2026-02-28", Name: "Pasta", Nutrients: training.Nutrients{CaloriesKcal: f(700)}})
	if day(t, e, "2026-02-28").Calories.Consumed != 700 || day(t, e, "2026-03-10").EntryCount != 0 {
		t.Fatal("the consumption date, not the creation timestamp, decides the day")
	}
	// Moving an entry to another day moves its totals.
	if _, err := e.svc.UpdateNutritionEntry(ctx(), old.ID, training.NutritionPatch{Date: ptrS("2026-03-01")}); err != nil {
		t.Fatal(err)
	}
	if day(t, e, "2026-02-28").EntryCount != 0 || day(t, e, "2026-03-01").Calories.Consumed != 700 {
		t.Fatal("moved entry")
	}
	if !day(t, e, "").Today || day(t, e, "").Date != "2026-03-11" {
		t.Fatal("today follows the configured timezone")
	}
}

func ptrS(v string) *string { return &v }

func TestNutritionWorksOnRestDaysWithoutCreatingAWorkout(t *testing.T) {
	e := newEnv(t)
	addMeal(t, e, meal("Rest-day breakfast", 400, 25, 40, 12))
	if _, err := e.svc.GetWorkout(ctx(), ""); !errors.Is(err, training.ErrNotFound) {
		t.Fatalf("logging food must not create a workout, got %v", err)
	}
	// And workouts are unaffected by nutrition.
	if _, err := e.svc.LogSets(ctx(), batch("", set("squat", 100, 5))); err != nil {
		t.Fatal(err)
	}
	w, _ := e.svc.GetWorkout(ctx(), "")
	if len(w.Sets) != 1 || day(t, e, "").EntryCount != 1 {
		t.Fatal("workout and nutrition stay separate")
	}
}

func TestNutritionTargetsAreEffectiveDated(t *testing.T) {
	e := newEnv(t)
	addMeal(t, e, training.NutritionInput{Date: "2026-03-01", Name: "Early", Nutrients: training.Nutrients{CaloriesKcal: f(1800), ProteinG: f(100)}})
	addMeal(t, e, meal("Today", 2600, 150, 300, 80))

	// No targets yet: everything unset, nothing invented.
	d := day(t, e, "")
	if d.Targets.Revision != 0 || d.Calories.Target != nil || d.Calories.Remaining != nil || d.Calories.Over != nil {
		t.Fatalf("unset targets: %+v", d)
	}
	t1, err := e.svc.SetNutritionTargets(ctx(), "2026-03-01", training.TargetPatch{Nutrients: training.Nutrients{CaloriesKcal: f(2000), ProteinG: f(140)}, ExpectedRevision: n(0)})
	if err != nil || t1.EffectiveFrom != "2026-03-01" || t1.Revision != 1 || t1.CarbsG != nil {
		t.Fatalf("set: %+v %v", t1, err)
	}
	d = day(t, e, "")
	if *d.Calories.Target != 2000 || *d.Calories.Over != 600 || *d.Calories.Remaining != 0 || *d.Protein.Remaining != 0 || *d.Protein.Over != 10 || d.Carbs.Target != nil {
		t.Fatalf("today vs targets: cal %+v protein %+v carbs %+v", d.Calories, d.Protein, d.Carbs)
	}
	early := day(t, e, "2026-03-01")
	if *early.Calories.Remaining != 200 || *early.Protein.Remaining != 40 {
		t.Fatalf("early day: %+v", early.Calories)
	}

	// A change from today forward (partial: only calories) keeps protein and
	// never changes what earlier days show.
	t2, err := e.svc.SetNutritionTargets(ctx(), "", training.TargetPatch{Nutrients: training.Nutrients{CaloriesKcal: f(2500)}, ExpectedRevision: &t1.Revision})
	if err != nil || t2.EffectiveFrom != "2026-03-10" || *t2.ProteinG != 140 || *t2.CaloriesKcal != 2500 {
		t.Fatalf("change from today: %+v %v", t2, err)
	}
	if got := day(t, e, "2026-03-01"); *got.Calories.Target != 2000 || got.Targets.EffectiveFrom != "2026-03-01" {
		t.Fatalf("editing current targets must not change earlier days: %+v", got.Targets)
	}
	if got := day(t, e, "2026-03-09"); *got.Calories.Target != 2000 {
		t.Fatal("the day before the change keeps the old target")
	}
	if got := day(t, e, ""); *got.Calories.Over != 100 {
		t.Fatalf("today uses the new target: %+v", got.Calories)
	}
	// A stale precondition is refused.
	if _, err := e.svc.SetNutritionTargets(ctx(), "", training.TargetPatch{Nutrients: training.Nutrients{FatG: f(70)}, ExpectedRevision: &t1.Revision}); !errors.Is(err, training.ErrStale) {
		t.Fatalf("stale target edit: %v", err)
	}
	// A future change applies only from its date.
	if _, err := e.svc.SetNutritionTargets(ctx(), "2026-04-01", training.TargetPatch{ClearCalories: true}); err != nil {
		t.Fatal(err)
	}
	future, _ := e.svc.NutritionTargetsOn(ctx(), "2026-04-02")
	if future.CaloriesKcal != nil || *future.ProteinG != 140 {
		t.Fatalf("future change: %+v", future)
	}
	if cur, _ := e.svc.NutritionTargetsOn(ctx(), ""); *cur.CaloriesKcal != 2500 {
		t.Fatal("a future change must not affect today")
	}
	// A change inserted between two others applies only until the next one.
	if _, err := e.svc.SetNutritionTargets(ctx(), "2026-03-05", training.TargetPatch{Nutrients: training.Nutrients{FatG: f(60)}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.svc.NutritionTargetsOn(ctx(), "2026-03-07"); got.FatG == nil || *got.FatG != 60 || *got.CaloriesKcal != 2000 {
		t.Fatalf("inserted change: %+v", got)
	}
	if got, _ := e.svc.NutritionTargetsOn(ctx(), "2026-03-10"); got.FatG != nil {
		t.Fatal("an inserted change must not leak past the next change")
	}
	hist, _ := e.svc.NutritionTargetHistory(ctx())
	if len(hist) != 4 {
		t.Fatalf("history: %+v", hist)
	}
	// Deleting a change restores the previous one from that date.
	if err := e.svc.DeleteNutritionTargetChange(ctx(), "2026-04-01", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.svc.NutritionTargetsOn(ctx(), "2026-04-02"); got.CaloriesKcal == nil || *got.CaloriesKcal != 2500 {
		t.Fatalf("after deleting the future change: %+v", got)
	}
	// Invalid targets.
	_, err = e.svc.SetNutritionTargets(ctx(), "", training.TargetPatch{Nutrients: training.Nutrients{ProteinG: f(-5)}})
	requireFields(t, err, "protein_g")
}

func TestNutritionEditsDoNotSilentlyOverwrite(t *testing.T) {
	e := newEnv(t)
	m := addMeal(t, e, meal("Salad", 250, 8, 20, 15))
	rev := m.Revision
	if _, err := e.svc.UpdateNutritionEntry(ctx(), m.ID, training.NutritionPatch{Nutrients: training.Nutrients{FatG: f(18)}, ExpectedRevision: &rev}); err != nil {
		t.Fatal(err)
	}
	// A second editor still holding the old revision loses, and changes nothing.
	_, err := e.svc.UpdateNutritionEntry(ctx(), m.ID, training.NutritionPatch{Nutrients: training.Nutrients{FatG: f(10)}, ExpectedRevision: &rev})
	if !errors.Is(err, training.ErrStale) {
		t.Fatalf("want stale, got %v", err)
	}
	if err := e.svc.DeleteNutritionEntry(ctx(), m.ID, &rev); !errors.Is(err, training.ErrStale) {
		t.Fatalf("stale delete: %v", err)
	}
	got, _ := e.svc.GetNutritionEntry(ctx(), m.ID)
	if *got.FatG != 18 {
		t.Fatalf("value overwritten: %v", *got.FatG)
	}
	// Racing editors with the same revision: exactly one wins.
	cur := got.Revision
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := e.svc.UpdateNutritionEntry(ctx(), m.ID, training.NutritionPatch{Name: ptrS("Salad " + strconv.Itoa(i)), ExpectedRevision: &cur})
			if err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("exactly one concurrent editor may win, got %d", wins)
	}
}

func TestNutritionSearchAndRanges(t *testing.T) {
	e := newEnv(t)
	addMeal(t, e, training.NutritionInput{Date: "2026-03-01", Name: "Greek yogurt", Nutrients: training.Nutrients{ProteinG: f(15)}})
	addMeal(t, e, training.NutritionInput{Date: "2026-03-05", Name: "Yogurt with honey", Nutrients: training.Nutrients{ProteinG: f(10)}})
	addMeal(t, e, training.NutritionInput{Date: "2026-03-05", Name: "שקשוקה", Nutrients: training.Nutrients{ProteinG: f(20)}})
	got, _ := e.svc.ListNutritionEntries(ctx(), training.NutritionFilter{Query: "YOGURT"})
	if len(got) != 2 || got[0].Date != "2026-03-05" {
		t.Fatalf("case-insensitive search, newest first: %+v", got)
	}
	he, _ := e.svc.ListNutritionEntries(ctx(), training.NutritionFilter{Query: "שקשוקה"})
	if len(he) != 1 {
		t.Fatal("Hebrew search")
	}
	r, _ := e.svc.ListNutritionEntries(ctx(), training.NutritionFilter{From: "2026-03-02", To: "2026-03-05"})
	if len(r) != 2 {
		t.Fatalf("range: %+v", r)
	}
	if _, err := e.svc.ListNutritionEntries(ctx(), training.NutritionFilter{From: "2026-03-05", To: "2026-03-01"}); !errors.Is(err, training.ErrValidation) {
		t.Fatal("inverted range")
	}
	days, err := e.svc.NutritionDays(ctx(), "2026-03-01", "2026-03-07")
	if err != nil || len(days) != 7 || days[0].Protein.Consumed != 15 || days[4].Protein.Consumed != 30 || days[2].EntryCount != 0 {
		t.Fatalf("day range includes empty days: %v", err)
	}
}

func TestNutritionSurvivesBackupAndRestore(t *testing.T) {
	e := newEnv(t)
	addMeal(t, e, meal("Backed up", 500, 30, 50, 20))
	if _, err := e.svc.SetNutritionTargets(ctx(), "", training.TargetPatch{Nutrients: training.Nutrients{CaloriesKcal: f(2200)}}); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/backup.db"
	if err := e.svc.Snapshot(ctx(), path); err != nil {
		t.Fatal(err)
	}
	restored := openEnv(t, path)
	d := day(t, restored, "")
	if d.EntryCount != 1 || d.Calories.Consumed != 500 || *d.Calories.Target != 2200 {
		t.Fatalf("restored nutrition: %+v", d)
	}
}
