package integration

import (
	"errors"
	"slices"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

func item(name string, grams, kcal, p, c, fat *float64) training.NutritionItem {
	return training.NutritionItem{Name: name, AmountG: grams, Nutrients: training.Nutrients{CaloriesKcal: kcal, ProteinG: p, CarbsG: c, FatG: fat}}
}

func TestMealMadeOfIngredientsSumsTheirValues(t *testing.T) {
	e := newEnv(t)
	bowl := training.NutritionInput{Name: "Yogurt bowl", Items: []training.NutritionItem{
		item(" Greek  yogurt ", f(200), f(194), f(20), f(7.6), f(10)),
		item("Granola", f(40), f(180), f(4), f(26), f(6.5)),
		item("Blueberries", f(80), f(46), f(0.6), f(11.6), nil), // fat unknown
	}}
	m, _, err := e.svc.AddNutritionEntry(ctx(), bowl)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Items) != 3 || m.Items[0].Name != "Greek yogurt" || *m.Items[1].AmountG != 40 {
		t.Fatalf("items: %+v", m.Items)
	}
	if *m.CaloriesKcal != 420 || *m.ProteinG != 24.6 || *m.CarbsG != 45.2 || *m.FatG != 16.5 {
		t.Fatalf("sums: %+v", m.Nutrients)
	}
	if !slices.Equal(m.IncompleteValues, []string{"fat_g"}) {
		t.Fatalf("fat is known for only some items: %v", m.IncompleteValues)
	}
	// The day counts the known part and flags the meal as missing fat.
	d := day(t, e, "")
	if d.Calories.Consumed != 420 || d.Calories.MissingEntries != 0 || d.Fat.Consumed != 16.5 || d.Fat.MissingEntries != 1 {
		t.Fatalf("day: %+v %+v", d.Calories, d.Fat)
	}
	// A value no item provides stays missing, never 0.
	tea, _, err := e.svc.AddNutritionEntry(ctx(), training.NutritionInput{Name: "Tea", Items: []training.NutritionItem{item("Black tea", f(250), nil, nil, nil, nil)}})
	if err != nil || tea.CaloriesKcal != nil || len(tea.IncompleteValues) != 0 {
		t.Fatalf("tea: %+v %v", tea, err)
	}
	// Entries without items keep working and read back with an empty list.
	plain := addMeal(t, e, meal("Apple", 95, 0.5, 25, 0.3))
	if plain.Items == nil || len(plain.Items) != 0 {
		t.Fatalf("plain entry items must be an empty list: %#v", plain.Items)
	}
}

func TestMealIngredientValidationAndEditing(t *testing.T) {
	e := newEnv(t)
	_, _, err := e.svc.AddNutritionEntry(ctx(), training.NutritionInput{Name: "Bad", Nutrients: training.Nutrients{CaloriesKcal: f(100)}, Items: []training.NutritionItem{
		item("", nil, f(-1), nil, nil, nil), item("Rice", f(0), nil, nil, nil, nil)}})
	got := map[string]bool{}
	for _, i := range training.IssuesOf(err) {
		got[i.Field] = true
	}
	for _, want := range []string{"items", "items[0].name", "items[0].calories_kcal", "items[1].amount_g"} {
		if !got[want] {
			t.Fatalf("missing issue %s in %v", want, err)
		}
	}

	m, _, err := e.svc.AddNutritionEntry(ctx(), training.NutritionInput{Name: "Chicken and rice", Items: []training.NutritionItem{
		item("Chicken breast", f(150), f(248), f(46), f(0), f(5.4)),
		item("Rice, cooked", f(200), f(260), f(5.4), f(56), f(0.6)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Values of a meal made of items cannot be set directly...
	if _, err := e.svc.UpdateNutritionEntry(ctx(), m.ID, training.NutritionPatch{Nutrients: training.Nutrients{CaloriesKcal: f(1)}}); training.IssuesOf(err) == nil {
		t.Fatalf("direct values on an ingredient meal must be rejected: %v", err)
	}
	// ...changing the items recomputes them.
	items := []training.NutritionItem{m.Items[0], item("Rice, cooked", f(100), f(130), f(2.7), f(28), f(0.3)), item("Olive oil", f(10), f(88), f(0), f(0), f(10))}
	up, err := e.svc.UpdateNutritionEntry(ctx(), m.ID, training.NutritionPatch{Items: &items, ExpectedRevision: &m.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if len(up.Items) != 3 || *up.CaloriesKcal != 466 || *up.FatG != 15.7 || up.Revision != 2 {
		t.Fatalf("after editing items: %+v", up)
	}
	// Removing the items keeps the sums as the entry's own, editable values.
	none := []training.NutritionItem{}
	flat, err := e.svc.UpdateNutritionEntry(ctx(), m.ID, training.NutritionPatch{Items: &none})
	if err != nil || len(flat.Items) != 0 || *flat.CaloriesKcal != 466 {
		t.Fatalf("remove items: %+v %v", flat, err)
	}
	if flat, err = e.svc.UpdateNutritionEntry(ctx(), m.ID, training.NutritionPatch{Nutrients: training.Nutrients{CaloriesKcal: f(450)}}); err != nil || *flat.CaloriesKcal != 450 {
		t.Fatalf("direct edit after removing items: %+v %v", flat, err)
	}
	// Turning a plain entry into an ingredient meal.
	plain := addMeal(t, e, meal("Sandwich", 400, 20, 40, 15))
	two := []training.NutritionItem{item("Bread", f(80), f(200), f(7), f(38), f(2)), item("Cheese", f(30), f(110), f(7), f(0), f(9))}
	conv, err := e.svc.UpdateNutritionEntry(ctx(), plain.ID, training.NutritionPatch{Items: &two})
	if err != nil || *conv.CaloriesKcal != 310 || len(conv.Items) != 2 {
		t.Fatalf("convert: %+v %v", conv, err)
	}
}

func TestCopyIdempotencyAndSuggestionsWithIngredients(t *testing.T) {
	e := newEnv(t)
	in := training.NutritionInput{Name: "Oats", IdempotencyKey: "k-oats", Items: []training.NutritionItem{
		item("Rolled oats", f(50), f(190), f(6.5), f(33), f(3.5)), item("Milk", f(200), f(100), f(6.8), f(9.6), f(3.6))}}
	a, _, err := e.svc.AddNutritionEntry(ctx(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.Items = []training.NutritionItem{item("Rolled oats", f(50), f(190), f(6.5), f(33), f(3.5)), item("Milk", f(200), f(100), f(6.8), f(9.6), f(3.6))}
	b, replayed, err := e.svc.AddNutritionEntry(ctx(), in)
	if err != nil || !replayed || b.ID != a.ID || len(b.Items) != 2 {
		t.Fatalf("replay: %+v %v %v", b, replayed, err)
	}
	in.Items[1].AmountG = f(250)
	if _, _, err := e.svc.AddNutritionEntry(ctx(), in); !errors.Is(err, training.ErrConflict) {
		t.Fatalf("same key with different items must conflict: %v", err)
	}
	// A copy carries independent items.
	e.now = e.now.AddDate(0, 0, 1)
	c, _, err := e.svc.CopyNutritionEntry(ctx(), a.ID, "", "")
	if err != nil || len(c.Items) != 2 || *c.CaloriesKcal != 290 {
		t.Fatalf("copy: %+v %v", c, err)
	}
	more := []training.NutritionItem{c.Items[0], item("Milk", f(300), f(150), f(10.2), f(14.4), f(5.4))}
	if _, err := e.svc.UpdateNutritionEntry(ctx(), c.ID, training.NutritionPatch{Items: &more}); err != nil {
		t.Fatal(err)
	}
	orig, _ := e.svc.GetNutritionEntry(ctx(), a.ID)
	if *orig.Items[1].AmountG != 200 || *orig.CaloriesKcal != 290 {
		t.Fatalf("editing the copy changed the original: %+v", orig)
	}
	// Suggestions: latest use of each ingredient, filterable.
	sug, err := e.svc.SuggestNutritionItems(ctx(), "", 0)
	if err != nil || len(sug) != 2 {
		t.Fatalf("suggestions: %+v %v", sug, err)
	}
	var milk training.NutritionItemSuggestion
	for _, s := range sug {
		if s.Name == "Milk" {
			milk = s
		}
	}
	if *milk.AmountG != 300 || milk.Uses != 2 || milk.LastUsed != "2026-03-11" {
		t.Fatalf("milk suggestion must be the latest use: %+v", milk)
	}
	if sug, _ = e.svc.SuggestNutritionItems(ctx(), "OAT", 0); len(sug) != 1 || sug[0].Name != "Rolled oats" {
		t.Fatalf("filtered: %+v", sug)
	}
	// Deleting a meal removes its items (and they stop being suggested once unused).
	_ = e.svc.DeleteNutritionEntry(ctx(), a.ID, nil)
	_ = e.svc.DeleteNutritionEntry(ctx(), c.ID, nil)
	if sug, _ = e.svc.SuggestNutritionItems(ctx(), "", 0); len(sug) != 0 {
		t.Fatalf("items of deleted meals: %+v", sug)
	}
}
