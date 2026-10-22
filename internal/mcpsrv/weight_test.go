package mcpsrv

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/fullfran/training-mcp/internal/sqlitestore"
	"github.com/fullfran/training-mcp/internal/training"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Synthetic weigh-ins. The fixed test clock makes today Tuesday 2026-03-10
// (Asia/Jerusalem); its week starts on Monday 2026-03-09.

func logWeighIn(t *testing.T, cs *mcp.ClientSession, args map[string]any) WeighInOut {
	t.Helper()
	var out WeighInOut
	callOK(t, cs, "log_weigh_in", args, &out)
	if out.WeighIn == nil {
		t.Fatalf("log_weigh_in returned no weigh_in: %+v", out)
	}
	return out
}

func TestWeighIns(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		var got GetWeighInOut
		callOK(t, cs, "get_weigh_in", nil, &got)
		if got.Found || got.WeighIn != nil || got.Date != "2026-03-10" || got.Today != "2026-03-10" || got.Timezone != "Asia/Jerusalem" {
			t.Fatalf("empty day = %+v", got)
		}

		first := logWeighIn(t, cs, map[string]any{"weight_kg": 82.35, "note": " morning "})
		if !first.Created || first.WeighIn.Date != "2026-03-10" || first.WeighIn.WeightKG != 82.35 || first.WeighIn.Note != "morning" ||
			first.WeighIn.Revision != 1 || first.Today != "2026-03-10" || first.Timezone != "Asia/Jerusalem" {
			t.Fatalf("first = %+v / %+v", first, first.WeighIn)
		}
		// A retry with the same values: not created, no revision bump.
		again := logWeighIn(t, cs, map[string]any{"weight_kg": 82.35, "note": "morning", "date": "2026-03-10"})
		if again.Created || again.WeighIn.Revision != 1 || again.WeighIn.ID != first.WeighIn.ID {
			t.Fatalf("retry = %+v", again.WeighIn)
		}
		// A new value replaces the day's weigh-in.
		up := logWeighIn(t, cs, map[string]any{"weight_kg": 82.1, "expected_revision": 1})
		if up.Created || up.WeighIn.WeightKG != 82.1 || up.WeighIn.Revision != 2 || up.WeighIn.Note != "" {
			t.Fatalf("replace = %+v", up.WeighIn)
		}
		msg := callErr(t, cs, "log_weigh_in", map[string]any{"weight_kg": 81, "expected_revision": 1})
		mustContain(t, msg, "conflict", "revision 2", "Nothing was written")
		msg = callErr(t, cs, "log_weigh_in", map[string]any{"weight_kg": 81, "expected_revision": 0})
		mustContain(t, msg, "conflict")
		callOK(t, cs, "get_weigh_in", map[string]any{"date": "2026-03-10"}, &got)
		if !got.Found || got.WeighIn.WeightKG != 82.1 || got.WeighIn.Revision != 2 {
			t.Fatalf("get = %+v", got)
		}

		// Validation: range, future date, note length; nothing written.
		msg = callErr(t, cs, "log_weigh_in", map[string]any{"weight_kg": 401})
		mustContain(t, msg, "validation error", "weight_kg: must be between 20 and 400 kg")
		msg = callErr(t, cs, "log_weigh_in", map[string]any{"weight_kg": 80, "date": "2026-03-11"})
		mustContain(t, msg, "date", "future", "weight can be logged")

		// Two weeks of data and the trend.
		logWeighIn(t, cs, map[string]any{"weight_kg": 83.4, "date": "2026-03-03"})
		logWeighIn(t, cs, map[string]any{"weight_kg": 83.0, "date": "2026-03-05"})
		logWeighIn(t, cs, map[string]any{"weight_kg": 82.5, "date": "2026-03-09"})
		var tr WeightTrendOut
		callOK(t, cs, "get_weight_trend", map[string]any{"weeks": 3}, &tr)
		if tr.Timezone != "Asia/Jerusalem" || tr.Today != "2026-03-10" || tr.From != "2026-02-23" || len(tr.Weeks) != 3 || len(tr.Entries) != 4 {
			t.Fatalf("trend header = %+v", tr)
		}
		empty, w1, w2 := tr.Weeks[0], tr.Weeks[1], tr.Weeks[2]
		if empty.DaysLogged != 0 || empty.AverageKG != nil || empty.ChangeKG != nil {
			t.Fatalf("empty week = %+v", empty)
		}
		// Week 1 has no previous week with data: average but no change (never invented).
		if w1.DaysLogged != 2 || *w1.AverageKG != 83.2 || w1.ChangeKG != nil || w1.Trend != "" {
			t.Fatalf("week 1 = %+v", w1)
		}
		if w2.WeekStart != "2026-03-09" || w2.DaysLogged != 2 || *w2.AverageKG != 82.3 || *w2.ChangeKG != -0.9 || w2.Trend != "down" {
			t.Fatalf("week 2 = %+v", w2)
		}
		if tr.Latest == nil || tr.Latest.Date != "2026-03-10" || tr.OverallChangeKG == nil || *tr.OverallChangeKG != -0.9 {
			t.Fatalf("latest/overall = %+v / %v", tr.Latest, tr.OverallChangeKG)
		}

		// Delete with a revision guard; the trend recomputes.
		msg = callErr(t, cs, "delete_weigh_in", map[string]any{"date": "2026-03-10", "expected_revision": 1})
		mustContain(t, msg, "conflict")
		var del DeleteWeighInOut
		callOK(t, cs, "delete_weigh_in", map[string]any{"date": "2026-03-10", "expected_revision": 2}, &del)
		if !del.Deleted || del.Date != "2026-03-10" {
			t.Fatalf("delete = %+v", del)
		}
		msg = callErr(t, cs, "delete_weigh_in", map[string]any{"date": "2026-03-10"})
		mustContain(t, msg, "not found")
		callOK(t, cs, "get_weight_trend", map[string]any{"weeks": 2}, &tr)
		if w := tr.Weeks[1]; w.DaysLogged != 1 || *w.AverageKG != 82.5 || *w.ChangeKG != -0.7 {
			t.Fatalf("after delete = %+v", w)
		}
		if w := getWorkout(t, cs, ""); w.Exists {
			t.Fatal("logging weight created a workout")
		}
	})
}

// noWeightStore exposes only the base store, hiding the weigh-in methods.
type noWeightStore struct{ training.Store }

func TestWeightDisabledToolError(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "training.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := training.NewService(noWeightStore{st}, func() time.Time { return testNow }).WithLocation(mustLoc(t, "Asia/Jerusalem"))
	cs := connectInMemory(t, New(svc))
	for _, c := range []struct {
		name string
		args map[string]any
	}{
		{"log_weigh_in", map[string]any{"weight_kg": 80}},
		{"get_weigh_in", nil},
		{"delete_weigh_in", map[string]any{"date": "2026-03-10"}},
		{"get_weight_trend", nil},
	} {
		mustContain(t, callErr(t, cs, c.name, c.args), "weight tracking is not available", "nothing was written")
	}
}
