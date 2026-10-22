package integration

import (
	"strings"
	"testing"
	"time"
)

// With the week configured to start on Sunday (the server default), weekly
// views run Sunday to Saturday: the schedule's default range, frequency
// tallies and weight trends.
func TestSundayWeekStart(t *testing.T) {
	e := newEnv(t)
	e.svc.WithWeekStart(time.Sunday)
	e.now = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC) // Tuesday

	importPlanDoc(t, e, planDoc(`{"type":"weekdays","days":["sun","wed"]}`), "")
	lower := importPlanDoc(t, e, []byte(strings.Replace(string(planDoc(`{"type":"times_per_week","times_per_week":2}`)), "Upper A", "Lower B", 1)), "").Plan
	v, err := e.svc.GetSchedule(ctx(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if v.From != "2026-03-08" || v.To != "2026-03-14" || v.Days[0].Weekday != "sun" || v.Days[6].Weekday != "sat" {
		t.Fatalf("default week must be Sunday..Saturday: %s..%s first=%s", v.From, v.To, v.Days[0].Weekday)
	}
	if len(v.Days[0].Scheduled) != 1 || v.Days[0].Scheduled[0].Status != "missed" || len(v.Days[3].Scheduled) != 1 {
		t.Fatalf("sunday plan: %+v / wednesday: %+v", v.Days[0], v.Days[3])
	}

	// Saturday and the next Sunday fall in different weeks for the tally.
	e.now = time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC) // Saturday
	if _, err := e.svc.StartPlannedSession(ctx(), "", lower.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.LogSets(ctx(), batch("", set("squat", 100, 5))); err != nil {
		t.Fatal(err)
	}
	e.now = time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC) // Sunday
	v, _ = e.svc.GetSchedule(ctx(), "", "")
	if v.From != "2026-03-15" || len(v.Weekly) != 1 || v.Weekly[0].Completed != 0 || v.Weekly[0].WeekStart != "2026-03-15" {
		t.Fatalf("new week on Sunday: %+v", v.Weekly)
	}
	v, _ = e.svc.GetSchedule(ctx(), "2026-03-08", "2026-03-14")
	if len(v.Weekly) != 1 || v.Weekly[0].Completed != 1 {
		t.Fatalf("Saturday counts in the week that started Sunday the 8th: %+v", v.Weekly)
	}

	// Weight trend weeks.
	weigh(t, e, "2026-03-07", 81) // Saturday: previous week
	weigh(t, e, "2026-03-08", 80) // Sunday: starts the week
	weigh(t, e, "2026-03-14", 79) // Saturday: same week
	tr, err := e.svc.WeightTrendFor(ctx(), "2026-03-14", 1)
	if err != nil {
		t.Fatal(err)
	}
	w := tr.Weeks[0]
	if w.WeekStart != "2026-03-08" || w.WeekEnd != "2026-03-14" || w.DaysLogged != 2 || *w.AverageKG != 79.5 || *w.ChangeKG != -1.5 {
		t.Fatalf("sunday weeks: %+v", w)
	}
}
