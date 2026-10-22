package mcpsrv

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Synthetic meals. The fixed test clock makes today 2026-03-10 (Asia/Jerusalem).

func addEntry(t *testing.T, cs *mcp.ClientSession, args map[string]any) NutritionEntryOut {
	t.Helper()
	var out NutritionEntryOut
	callOK(t, cs, "add_nutrition_entry", args, &out)
	return out
}

func nutritionDay(t *testing.T, cs *mcp.ClientSession, date string) NutritionDayOut {
	t.Helper()
	args := map[string]any{}
	if date != "" {
		args["date"] = date
	}
	var out NutritionDayOut
	callOK(t, cs, "get_nutrition_day", args, &out)
	if out.Day == nil {
		t.Fatalf("get_nutrition_day returned no day: %+v", out)
	}
	return out
}

func TestNutritionEntries(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		oats := addEntry(t, cs, map[string]any{"name": " Oatmeal  with berries ", "calories_kcal": 350, "protein_g": 12.5, "carbs_g": 58, "fat_g": 7.25, "idempotency_key": "meal-1"})
		if oats.Replayed || oats.Entry.ID == 0 || oats.Entry.Name != "Oatmeal with berries" || oats.Entry.Date != "2026-03-10" || oats.Entry.Revision != 1 || oats.Entry.CreatedAt == "" {
			t.Fatalf("add = %+v", oats)
		}
		// Unknown fat is omitted; zero carbs is an explicit zero.
		chicken := addEntry(t, cs, map[string]any{"name": "Chicken breast", "calories_kcal": 300, "protein_g": 55, "carbs_g": 0})
		if chicken.Entry.FatG != nil || chicken.Entry.CarbsG == nil || *chicken.Entry.CarbsG != 0 {
			t.Fatalf("zero vs missing = %+v", chicken.Entry.Nutrients)
		}

		d := nutritionDay(t, cs, "")
		day := d.Day
		if d.Today != "2026-03-10" || d.Timezone != "Asia/Jerusalem" || day.Date != "2026-03-10" || !day.Today || day.EntryCount != 2 {
			t.Fatalf("day header = %+v", d)
		}
		if day.Calories.Consumed != 650 || day.Protein.Consumed != 67.5 || day.Carbs.Consumed != 58 || day.Fat.Consumed != 7.25 ||
			day.Fat.MissingEntries != 1 || day.Carbs.MissingEntries != 0 || day.Calories.Target != nil || day.Calories.Remaining != nil {
			t.Fatalf("totals = %+v", day)
		}
		if w := getWorkout(t, cs, ""); w.Exists {
			t.Fatal("logging food created a workout")
		}

		// Retry replays; the same meal again without a key is a real duplicate.
		replay := addEntry(t, cs, map[string]any{"name": " Oatmeal  with berries ", "calories_kcal": 350, "protein_g": 12.5, "carbs_g": 58, "fat_g": 7.25, "idempotency_key": "meal-1"})
		if !replay.Replayed || replay.Entry.ID != oats.Entry.ID {
			t.Fatalf("replay = %+v", replay)
		}
		msg := callErr(t, cs, "add_nutrition_entry", map[string]any{"name": "Oatmeal", "calories_kcal": 400, "idempotency_key": "meal-1"})
		mustContain(t, msg, "conflict", "Nothing was written")
		dup := addEntry(t, cs, map[string]any{"name": "Oatmeal with berries", "calories_kcal": 350})
		if dup.Entry.ID == oats.Entry.ID || dup.Replayed {
			t.Fatalf("intentional duplicate = %+v", dup)
		}
		if n := nutritionDay(t, cs, "").Day.EntryCount; n != 3 {
			t.Fatalf("entry count = %d, want 3", n)
		}

		// Partial update keeps omitted values; stale revision conflicts.
		var upd NutritionEntryOut
		callOK(t, cs, "update_nutrition_entry", map[string]any{"entry_id": chicken.Entry.ID, "protein_g": 30, "expected_revision": chicken.Entry.Revision}, &upd)
		if *upd.Entry.ProteinG != 30 || *upd.Entry.CaloriesKcal != 300 || upd.Entry.Revision != 2 || upd.Entry.Name != "Chicken breast" {
			t.Fatalf("partial update = %+v", upd.Entry)
		}
		msg = callErr(t, cs, "update_nutrition_entry", map[string]any{"entry_id": chicken.Entry.ID, "fat_g": 5, "expected_revision": chicken.Entry.Revision})
		mustContain(t, msg, "conflict", "re-read")
		callOK(t, cs, "update_nutrition_entry", map[string]any{"entry_id": chicken.Entry.ID, "clear": []any{"calories_kcal"}}, &upd)
		if upd.Entry.CaloriesKcal != nil || *upd.Entry.ProteinG != 30 {
			t.Fatalf("clear = %+v", upd.Entry)
		}
		msg = callErr(t, cs, "update_nutrition_entry", map[string]any{"entry_id": chicken.Entry.ID, "fat_g": 5, "clear": []any{"fat_g"}})
		mustContain(t, msg, "fat_g", "cannot both set a value and clear it")

		// Invalid values and dates are reported per field.
		msg = callErr(t, cs, "add_nutrition_entry", map[string]any{"name": "Bad", "calories_kcal": -5, "protein_g": 5000})
		mustContain(t, msg, "validation error", "calories_kcal: must not be negative", "protein_g: must be at most 2000")
		msg = callErr(t, cs, "add_nutrition_entry", map[string]any{"name": "Tomorrow's lunch", "date": "2026-03-11", "calories_kcal": 500})
		mustContain(t, msg, "date", "future", "2026-03-10")

		// Copy yesterday's dinner to today: independent of the original.
		dinner := addEntry(t, cs, map[string]any{"date": "2026-03-09", "name": "Salmon and potatoes", "calories_kcal": 700, "protein_g": 40, "carbs_g": 60, "fat_g": 30})
		var found NutritionEntriesOut
		callOK(t, cs, "list_nutrition_entries", map[string]any{"query": "salmon"}, &found)
		if len(found.Entries) != 1 || found.Entries[0].ID != dinner.Entry.ID {
			t.Fatalf("list by query = %+v", found.Entries)
		}
		callOK(t, cs, "list_nutrition_entries", map[string]any{"date": "2026-03-09"}, &found)
		if len(found.Entries) != 1 {
			t.Fatalf("list by date = %+v", found.Entries)
		}
		callOK(t, cs, "list_nutrition_entries", map[string]any{}, &found)
		if len(found.Entries) != 4 {
			t.Fatalf("list latest = %d entries", len(found.Entries))
		}
		msg = callErr(t, cs, "list_nutrition_entries", map[string]any{"date": "2026-03-09", "from": "2026-03-01"})
		mustContain(t, msg, "date", "not both")

		var cp NutritionEntryOut
		callOK(t, cs, "copy_nutrition_entry", map[string]any{"entry_id": dinner.Entry.ID, "idempotency_key": "copy-1"}, &cp)
		if cp.Entry.ID == dinner.Entry.ID || cp.Entry.Date != "2026-03-10" || cp.Entry.CopiedFrom == nil || *cp.Entry.CopiedFrom != dinner.Entry.ID || *cp.Entry.CaloriesKcal != 700 {
			t.Fatalf("copy = %+v", cp.Entry)
		}
		var cpAgain NutritionEntryOut
		callOK(t, cs, "copy_nutrition_entry", map[string]any{"entry_id": dinner.Entry.ID, "idempotency_key": "copy-1"}, &cpAgain)
		if !cpAgain.Replayed || cpAgain.Entry.ID != cp.Entry.ID {
			t.Fatalf("copy replay = %+v", cpAgain)
		}
		callOK(t, cs, "update_nutrition_entry", map[string]any{"entry_id": cp.Entry.ID, "calories_kcal": 500}, &upd)
		var orig NutritionEntryOut
		callOK(t, cs, "get_nutrition_entry", map[string]any{"entry_id": dinner.Entry.ID}, &orig)
		if *orig.Entry.CaloriesKcal != 700 || orig.Entry.Revision != 1 {
			t.Fatalf("editing the copy changed the original: %+v", orig.Entry)
		}

		// Delete with revision guard.
		msg = callErr(t, cs, "delete_nutrition_entry", map[string]any{"entry_id": cp.Entry.ID, "expected_revision": cp.Entry.Revision})
		mustContain(t, msg, "conflict")
		var del DeleteNutritionOut
		callOK(t, cs, "delete_nutrition_entry", map[string]any{"entry_id": cp.Entry.ID, "expected_revision": upd.Entry.Revision}, &del)
		if !del.Deleted {
			t.Fatalf("delete = %+v", del)
		}
		msg = callErr(t, cs, "get_nutrition_entry", map[string]any{"entry_id": cp.Entry.ID})
		mustContain(t, msg, "not found")

		var multi NutritionDayOut
		callOK(t, cs, "get_nutrition_day", map[string]any{"from": "2026-03-09", "to": "2026-03-10"}, &multi)
		if len(multi.Days) != 2 || multi.Days[0].Calories.Consumed != 700 || multi.Day != nil {
			t.Fatalf("multi-day = %+v", multi)
		}
	})
}

func TestNutritionTargets(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		var tg TargetsOut
		callOK(t, cs, "get_nutrition_targets", nil, &tg)
		if tg.Date != "2026-03-10" || tg.Targets.Revision != 0 || tg.Targets.CaloriesKcal != nil || len(tg.History) != 0 {
			t.Fatalf("initial targets = %+v", tg)
		}
		callOK(t, cs, "set_nutrition_targets", map[string]any{"effective_from": "2026-03-01", "calories_kcal": 2500, "protein_g": 160, "expected_revision": 0}, &tg)
		first := tg.Targets
		if first.EffectiveFrom != "2026-03-01" || *first.CaloriesKcal != 2500 || first.Revision < 1 {
			t.Fatalf("first targets = %+v", first)
		}
		msg := callErr(t, cs, "set_nutrition_targets", map[string]any{"calories_kcal": 2400, "expected_revision": 0})
		mustContain(t, msg, "conflict", "re-read")

		// A later change keeps protein, lowers calories, and leaves earlier dates alone.
		callOK(t, cs, "set_nutrition_targets", map[string]any{"effective_from": "2026-03-08", "calories_kcal": 2200, "clear": []any{"protein_g"}, "expected_revision": first.Revision}, &tg)
		if len(tg.History) != 2 {
			t.Fatalf("history = %+v", tg.History)
		}
		callOK(t, cs, "get_nutrition_targets", map[string]any{"date": "2026-03-05"}, &tg)
		if *tg.Targets.CaloriesKcal != 2500 || tg.Targets.ProteinG == nil || *tg.Targets.ProteinG != 160 {
			t.Fatalf("earlier date changed: %+v", tg.Targets)
		}
		callOK(t, cs, "set_nutrition_targets", map[string]any{"effective_from": "2026-03-09", "fat_g": 70}, &tg)
		callOK(t, cs, "get_nutrition_targets", map[string]any{}, &tg)
		if *tg.Targets.CaloriesKcal != 2200 || tg.Targets.ProteinG != nil || *tg.Targets.FatG != 70 {
			t.Fatalf("today's targets = %+v", tg.Targets)
		}

		addEntry(t, cs, map[string]any{"name": "Pasta", "calories_kcal": 2500, "fat_g": 20})
		day := nutritionDay(t, cs, "").Day
		if *day.Calories.Target != 2200 || *day.Calories.Over != 300 || *day.Calories.Remaining != 0 || *day.Fat.Remaining != 50 || day.Protein.Target != nil {
			t.Fatalf("day vs targets = %+v / %+v / %+v", day.Calories, day.Fat, day.Protein)
		}
		msg = callErr(t, cs, "set_nutrition_targets", map[string]any{"calories_kcal": -1})
		mustContain(t, msg, "calories_kcal: must not be negative")
		msg = callErr(t, cs, "set_nutrition_targets", map[string]any{})
		mustContain(t, msg, "provide at least one target")
		if w := getWorkout(t, cs, ""); w.Exists {
			t.Fatal("nutrition created a workout")
		}
	})
}
