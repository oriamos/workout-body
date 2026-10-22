package integration

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fullfran/training-mcp/internal/training"
)

func weigh(t *testing.T, e *env, date string, kg float64) training.WeighIn {
	t.Helper()
	w, _, err := e.svc.LogWeighIn(ctx(), date, kg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestWeighInOnePerDayReplacesAndRetriesAreHarmless(t *testing.T) {
	e := newEnv(t)
	w, created, err := e.svc.LogWeighIn(ctx(), "", 82.35, " morning ", nil)
	if err != nil || !created || w.Date != "2026-03-10" || w.WeightKG != 82.35 || w.Note != "morning" || w.Revision != 1 {
		t.Fatalf("first weigh-in: %+v created=%v err=%v", w, created, err)
	}
	// Same values again (a retry): no new row, no revision bump.
	again, created, err := e.svc.LogWeighIn(ctx(), "2026-03-10", 82.35, "morning", nil)
	if err != nil || created || again.Revision != 1 || again.ID != w.ID {
		t.Fatalf("retry: %+v created=%v err=%v", again, created, err)
	}
	// A different value replaces the day's weigh-in.
	up, created, err := e.svc.LogWeighIn(ctx(), "2026-03-10", 82.1, "", nil)
	if err != nil || created || up.WeightKG != 82.1 || up.Revision != 2 || up.ID != w.ID {
		t.Fatalf("replace: %+v created=%v err=%v", up, created, err)
	}
	list, _ := e.svc.ListWeighIns(ctx(), "", "")
	if len(list) != 1 {
		t.Fatalf("one weigh-in per day, got %d", len(list))
	}
	// Concurrent retries of the same weigh-in for a new day produce one row.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _, _ = e.svc.LogWeighIn(ctx(), "2026-03-09", 82.6, "", nil) }()
	}
	wg.Wait()
	list, _ = e.svc.ListWeighIns(ctx(), "", "")
	if len(list) != 2 || list[0].Date != "2026-03-09" || list[0].Revision != 1 {
		t.Fatalf("after concurrent retries: %+v", list)
	}
}

func TestWeighInValidationRevisionsAndDelete(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		date  string
		kg    float64
		field string
	}{{"", 0, "weight_kg"}, {"", 401, "weight_kg"}, {"", 19.9, "weight_kg"}, {"2026-03-11", 80, "date"}, {"2026-02-30", 80, "date"}} {
		_, _, err := e.svc.LogWeighIn(ctx(), c.date, c.kg, "", nil)
		iss := training.IssuesOf(err)
		if len(iss) == 0 || iss[0].Field != c.field {
			t.Fatalf("%+v: want issue on %s, got %v", c, c.field, err)
		}
	}
	zero := 0
	w, _, err := e.svc.LogWeighIn(ctx(), "", 80, "", &zero)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.LogWeighIn(ctx(), "", 81, "", &zero); !errors.Is(err, training.ErrStale) {
		t.Fatalf("expected_revision 0 on an existing day must be stale, got %v", err)
	}
	if _, _, err := e.svc.LogWeighIn(ctx(), "", 81, "", &w.Revision); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteWeighIn(ctx(), "", &w.Revision); !errors.Is(err, training.ErrStale) {
		t.Fatalf("stale delete: %v", err)
	}
	if err := e.svc.DeleteWeighIn(ctx(), "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GetWeighIn(ctx(), ""); !errors.Is(err, training.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if err := e.svc.DeleteWeighIn(ctx(), "", nil); !errors.Is(err, training.ErrNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestWeightTrendWeeklyAveragesAndWeekOverWeek(t *testing.T) {
	e := newEnv(t) // today is Tuesday 2026-03-10; weeks start Monday
	// Week of 2026-02-16: nothing (a gap).
	// Week of 2026-02-23: 84, 83 -> 83.5
	weigh(t, e, "2026-02-23", 84)
	weigh(t, e, "2026-02-26", 83)
	// Week of 2026-03-02: 83, 82.5, 82 -> 82.5
	weigh(t, e, "2026-03-02", 83)
	weigh(t, e, "2026-03-04", 82.5)
	weigh(t, e, "2026-03-08", 82) // Sunday belongs to the same week
	// Week of 2026-03-09 (current, partial): 82.6
	weigh(t, e, "2026-03-09", 82.6)

	tr, err := e.svc.WeightTrendFor(ctx(), "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if tr.From != "2026-02-16" || tr.To != "2026-03-10" || len(tr.Weeks) != 4 || len(tr.Entries) != 6 {
		t.Fatalf("range: %+v", tr)
	}
	gap, w2, w3, w4 := tr.Weeks[0], tr.Weeks[1], tr.Weeks[2], tr.Weeks[3]
	if gap.DaysLogged != 0 || gap.AverageKG != nil || gap.ChangeKG != nil || gap.Trend != "" {
		t.Fatalf("empty week must have null stats: %+v", gap)
	}
	if *w2.AverageKG != 83.5 || w2.DaysLogged != 2 || w2.ChangeKG != nil {
		t.Fatalf("no comparison after an empty week: %+v", w2)
	}
	if *w3.AverageKG != 82.5 || w3.DaysLogged != 3 || *w3.MinKG != 82 || *w3.MaxKG != 83 || w3.WeekEnd != "2026-03-08" {
		t.Fatalf("week 3: %+v", w3)
	}
	if *w3.ChangeKG != -1 || *w3.ChangePct != -1.2 || w3.Trend != "down" {
		t.Fatalf("week 3 change: %v %v %s", *w3.ChangeKG, *w3.ChangePct, w3.Trend)
	}
	if *w4.AverageKG != 82.6 || *w4.ChangeKG != 0.1 || w4.Trend != "up" {
		t.Fatalf("week 4: %+v change=%v", w4, *w4.ChangeKG)
	}
	if tr.Latest == nil || tr.Latest.Date != "2026-03-09" || tr.OverallChangeKG == nil || *tr.OverallChangeKG != -0.9 {
		t.Fatalf("latest/overall: %+v %v", tr.Latest, tr.OverallChangeKG)
	}

	// The first week in range compares with the week before it.
	tr, _ = e.svc.WeightTrendFor(ctx(), "2026-03-08", 1)
	if len(tr.Weeks) != 1 || *tr.Weeks[0].ChangeKG != -1 {
		t.Fatalf("one-week view must still compare with the prior week: %+v", tr.Weeks)
	}
	// Flat when the change is under 0.1 kg.
	weigh(t, e, "2026-03-10", 82.6)
	tr, _ = e.svc.WeightTrendFor(ctx(), "", 1)
	if tr.Weeks[0].Trend != "up" { // avg 82.6 vs 82.5
		t.Fatalf("trend: %+v", tr.Weeks[0])
	}
	weigh(t, e, "2026-03-10", 82.44) // avg (82.6+82.44)/2 = 82.52 -> +0.02 flat
	tr, _ = e.svc.WeightTrendFor(ctx(), "", 1)
	if tr.Weeks[0].Trend != "flat" || *tr.Weeks[0].ChangeKG != 0.02 {
		t.Fatalf("flat: %+v", tr.Weeks[0])
	}
	if _, err := e.svc.WeightTrendFor(ctx(), "", 105); training.IssuesOf(err) == nil {
		t.Fatal("weeks over the max must be rejected")
	}
	// No data at all: empty weeks, nothing invented.
	empty := newEnv(t)
	tr, err = empty.svc.WeightTrendFor(ctx(), "", 0)
	if err != nil || len(tr.Weeks) != 8 || tr.Latest != nil || tr.OverallChangeKG != nil {
		t.Fatalf("empty trend: %+v %v", tr, err)
	}
}

func TestWeighInUsesLocalDate(t *testing.T) {
	e := newEnv(t)
	e.now = e.now.Add(10*time.Hour + 30*time.Minute) // 22:30 UTC = 00:30 next day in Jerusalem
	w := weigh(t, e, "", 80)
	if w.Date != "2026-03-11" {
		t.Fatalf("local date: %s", w.Date)
	}
}
