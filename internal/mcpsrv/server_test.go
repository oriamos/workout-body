package mcpsrv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/training-mcp/internal/httpsrv"
	"github.com/fullfran/training-mcp/internal/sqlitestore"
	"github.com/fullfran/training-mcp/internal/training"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// All data below is synthetic.

const testToken = "test-secret-token"

var testNow = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

func mustLoc(t testing.TB, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// newService is the real service over a fresh SQLite file with a fixed clock.
func newService(t testing.TB, dbPath string) *training.Service {
	t.Helper()
	if dbPath == "" {
		dbPath = filepath.Join(t.TempDir(), "training.db")
	}
	st, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return training.NewService(st, func() time.Time { return testNow }).WithLocation(mustLoc(t, "Asia/Jerusalem"))
}

func connectInMemory(t testing.TB, s *Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := s.mcp.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// httpServer serves the MCP handler behind the production bearer middleware.
func httpServer(t testing.TB, s *Server) *httptest.Server {
	t.Helper()
	h := httptest.NewServer(httpsrv.Routes(s.Handler(), false, testToken, nil))
	t.Cleanup(h.Close)
	return h
}

func connectHTTP(t testing.TB, s *Server) *mcp.ClientSession {
	t.Helper()
	cs, err := dialHTTP(httpServer(t, s).URL, testToken)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func dialHTTP(base, token string) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   base + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: token}},
	}
	return client.Connect(context.Background(), transport, nil)
}

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return b.base.RoundTrip(r)
}

// transports runs fn once over in-memory transports and once over Streamable
// HTTP with bearer auth, each against its own fresh database.
func transports(t *testing.T, fn func(t *testing.T, cs *mcp.ClientSession)) {
	t.Run("in-memory", func(t *testing.T) { fn(t, connectInMemory(t, New(newService(t, "")))) })
	t.Run("streamable-http", func(t *testing.T) { fn(t, connectHTTP(t, New(newService(t, "")))) })
}

func call(t testing.TB, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	return res
}

// callOK calls a tool, fails the test on a tool error, and decodes the result.
func callOK(t testing.TB, cs *mcp.ClientSession, name string, args map[string]any, out any) {
	t.Helper()
	res := call(t, cs, name, args)
	if res.IsError {
		t.Fatalf("%s: tool error: %s", name, text(res))
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("%s: decode %s: %v", name, data, err)
		}
	}
}

// callErr calls a tool and returns its error text, failing if it succeeded.
func callErr(t testing.TB, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res := call(t, cs, name, args)
	if !res.IsError {
		t.Fatalf("%s: expected a tool error, got %s", name, text(res))
	}
	return text(res)
}

func text(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func structured(t testing.TB, res *mcp.CallToolResult) string {
	t.Helper()
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustContain(t testing.TB, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%q does not contain %q", got, w)
		}
	}
}

func getWorkout(t testing.TB, cs *mcp.ClientSession, date string) WorkoutOut {
	t.Helper()
	args := map[string]any{}
	if date != "" {
		args["date"] = date
	}
	var out WorkoutOut
	callOK(t, cs, "get_workout", args, &out)
	return out
}

func setsOf(w *training.Session, exercise string) []training.Set {
	var out []training.Set
	for _, s := range w.Sets {
		if s.Exercise == exercise {
			out = append(out, s)
		}
	}
	return out
}

func progressOf(w *training.Session, exercise string) training.PlanProgress {
	for _, p := range w.Progress {
		if p.Exercise == exercise {
			return p
		}
	}
	return training.PlanProgress{}
}

// ---- scenarios --------------------------------------------------------------

// TestIncrementalLoggingScenario is the four-message conversation:
//  1. "bench 8x60, planning 3 sets"
//  2. "same"
//  3. "third one, then squats 130x4, planning 3 sets of those"
//  4. "actually bench set 2 was 7 reps"
func TestIncrementalLoggingScenario(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		var r1 training.BatchResult
		callOK(t, cs, "log_sets", map[string]any{
			"sets":            []any{map[string]any{"exercise": "Bench Press", "weight_kg": 60, "reps": 8}},
			"prescriptions":   []any{map[string]any{"exercise": "bench press", "target_sets": 3}},
			"idempotency_key": "msg-1",
		}, &r1)
		if !r1.Created || len(r1.Sets) != 1 || r1.Sets[0].ID == 0 || r1.Sets[0].Exercise != "bench press" {
			t.Fatalf("message 1: %+v", r1)
		}
		if r1.Sets[0].RPE != nil || r1.Sets[0].RIR != nil {
			t.Fatalf("message 1 invented RPE/RIR: %+v", r1.Sets[0])
		}
		if len(r1.NewExercises) != 1 || r1.NewExercises[0].Name != "bench press" {
			t.Fatalf("message 1 new_exercises = %+v", r1.NewExercises)
		}
		if r1.Workout.Date != "2026-03-10" {
			t.Fatalf("workout date = %s, want the Asia/Jerusalem local date", r1.Workout.Date)
		}

		// "same": the assistant resolves it to explicit values from context.
		var r2 training.BatchResult
		callOK(t, cs, "log_sets", map[string]any{
			"sets":            []any{map[string]any{"exercise": "bench press", "weight_kg": 60, "reps": 8}},
			"idempotency_key": "msg-2",
		}, &r2)
		if r2.Created || len(r2.Sets) != 1 || r2.Workout.ID != r1.Workout.ID {
			t.Fatalf("message 2: %+v", r2)
		}

		var r3 training.BatchResult
		callOK(t, cs, "log_sets", map[string]any{
			"sets": []any{
				map[string]any{"exercise": "bench press", "weight_kg": 60, "reps": 8},
				map[string]any{"exercise": "squat", "weight_kg": 130, "reps": 4},
			},
			"prescriptions":   []any{map[string]any{"exercise": "squat", "target_sets": 3}},
			"idempotency_key": "msg-3",
		}, &r3)
		if len(r3.Sets) != 2 || len(r3.Prescriptions) != 1 || r3.Prescriptions[0].TargetSets != 3 {
			t.Fatalf("message 3: %+v", r3)
		}

		// Correction: read, find bench set number 2, update it by its id.
		before := getWorkout(t, cs, "")
		bench := setsOf(before.Workout, "bench press")
		if len(bench) != 3 || bench[1].SetNumber != 2 {
			t.Fatalf("bench sets before correction = %+v", bench)
		}
		var upd SetOut
		callOK(t, cs, "update_set", map[string]any{"set_id": bench[1].ID, "reps": 7, "expected_revision": bench[1].Revision}, &upd)
		if upd.Set.ID != bench[1].ID || upd.Set.Reps == nil || *upd.Set.Reps != 7 || *upd.Set.WeightKG != 60 {
			t.Fatalf("update = %+v", upd.Set)
		}

		w := getWorkout(t, cs, "")
		if !w.Exists || w.Timezone != "Asia/Jerusalem" || w.Today != "2026-03-10" || w.Date != "2026-03-10" {
			t.Fatalf("get_workout header = %+v", w)
		}
		bench = setsOf(w.Workout, "bench press")
		var reps []int
		for i, s := range bench {
			if s.WeightKG == nil || *s.WeightKG != 60 || s.RPE != nil || s.SetNumber != i+1 || s.SetType != "working" {
				t.Fatalf("bench set %d = %+v", i, s)
			}
			reps = append(reps, *s.Reps)
		}
		if !reflect.DeepEqual(reps, []int{8, 7, 8}) {
			t.Fatalf("bench reps = %v, want [8 7 8]", reps)
		}
		squat := setsOf(w.Workout, "squat")
		if len(squat) != 1 || *squat[0].WeightKG != 130 || *squat[0].Reps != 4 || squat[0].SetNumber != 1 {
			t.Fatalf("squat sets = %+v", squat)
		}
		if p := progressOf(w.Workout, "bench press"); p.TargetSets != 3 || p.DoneSets != 3 {
			t.Fatalf("bench progress = %+v", p)
		}
		// Planning 3 squat sets must not mark any as completed.
		if p := progressOf(w.Workout, "squat"); p.TargetSets != 3 || p.DoneSets != 1 {
			t.Fatalf("squat progress = %+v", p)
		}
		if len(w.Workout.Sets) != 4 {
			t.Fatalf("workout has %d sets, want 4", len(w.Workout.Sets))
		}
	})
}

func TestGetWorkoutNeverCreates(t *testing.T) {
	cs := connectInMemory(t, New(newService(t, "")))
	w := getWorkout(t, cs, "2026-03-09")
	if w.Exists || w.Workout != nil || w.Date != "2026-03-09" || w.Today != "2026-03-10" || w.Timezone != "Asia/Jerusalem" {
		t.Fatalf("get_workout = %+v", w)
	}
	var list ListOut
	callOK(t, cs, "list_sessions", map[string]any{}, &list)
	if len(list.Sessions) != 0 {
		t.Fatalf("get_workout created a workout: %+v", list.Sessions)
	}
	msg := callErr(t, cs, "get_workout", map[string]any{"date": "2026-02-30"})
	mustContain(t, msg, "date", "YYYY-MM-DD")
}

func TestIdempotentRetry(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		args := map[string]any{
			"sets":            []any{map[string]any{"exercise": "deadlift", "weight_kg": 140, "reps": 5, "rpe": 8}},
			"idempotency_key": "retry-key-1",
		}
		var first, second training.BatchResult
		callOK(t, cs, "log_sets", args, &first)
		callOK(t, cs, "log_sets", args, &second)
		if first.Replayed || !second.Replayed {
			t.Fatalf("replayed flags: first=%v second=%v", first.Replayed, second.Replayed)
		}
		if len(second.Sets) != 1 || second.Sets[0].ID != first.Sets[0].ID {
			t.Fatalf("replay returned different sets: %+v vs %+v", second.Sets, first.Sets)
		}
		if w := getWorkout(t, cs, ""); len(w.Workout.Sets) != 1 {
			t.Fatalf("retry wrote again: %d sets", len(w.Workout.Sets))
		}

		// Same key, different payload: conflict, nothing written.
		msg := callErr(t, cs, "log_sets", map[string]any{
			"sets":            []any{map[string]any{"exercise": "deadlift", "weight_kg": 150, "reps": 5}},
			"idempotency_key": "retry-key-1",
		})
		mustContain(t, msg, "conflict", "retry-key-1", "different request", "Nothing was written", "new key")
		if w := getWorkout(t, cs, ""); len(w.Workout.Sets) != 1 {
			t.Fatalf("conflicting key wrote: %d sets", len(w.Workout.Sets))
		}

		// log_set uses the same mechanism.
		single := map[string]any{"exercise": "plank", "count": 2, "idempotency_key": "retry-key-2"}
		var a, b training.BatchResult
		callOK(t, cs, "log_set", single, &a)
		callOK(t, cs, "log_set", single, &b)
		if len(a.Sets) != 2 || !b.Replayed || a.Sets[1].ID != b.Sets[1].ID {
			t.Fatalf("log_set retry: %+v / %+v", a, b)
		}
	})
}

func TestValidationErrorsAreActionable(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		msg := callErr(t, cs, "log_sets", map[string]any{
			"sets": []any{
				map[string]any{"exercise": "bench press", "weight_kg": 60, "reps": 0},
				map[string]any{"exercise": "bench press", "weight_kg": 60, "reps": 8, "rpe": 11},
			},
			"prescriptions": []any{map[string]any{"exercise": "squat", "target_sets": 0}},
		})
		mustContain(t, msg, "validation error", "sets[0].reps", "sets[1].rpe", "prescriptions[0].target_sets", "Nothing was written")
		if w := getWorkout(t, cs, ""); w.Exists {
			t.Fatal("an invalid batch created a workout")
		}

		msg = callErr(t, cs, "log_sets", map[string]any{"date": "2026-03-11", "sets": []any{map[string]any{"exercise": "row", "reps": 10}}})
		mustContain(t, msg, "date", "future", "2026-03-10")

		msg = callErr(t, cs, "log_sets", map[string]any{"sets": []any{map[string]any{"weight_kg": 20}}})
		mustContain(t, msg, "sets[0].exercise", "exercise_id")

		var r training.BatchResult
		callOK(t, cs, "log_sets", map[string]any{"sets": []any{map[string]any{"exercise": "row", "weight_kg": 50, "reps": 10}}}, &r)
		msg = callErr(t, cs, "update_set", map[string]any{"set_id": r.Sets[0].ID, "reps": 9, "clear": []any{"reps"}})
		mustContain(t, msg, "reps", "cannot both set a value and clear it")

		msg = callErr(t, cs, "update_set", map[string]any{"set_id": 999999, "reps": 9})
		mustContain(t, msg, "set 999999 does not exist", "get_workout")

		// Inverted range from a legacy tool keeps its guidance.
		msg = callErr(t, cs, "list_sessions", map[string]any{"from": "2026-08-07", "to": "2026-08-06"})
		mustContain(t, msg, "from: must not be after to")
		if strings.Contains(strings.ToLower(msg), "sql") || strings.Contains(msg, "internal tool error") {
			t.Fatalf("validation error masked or leaked internals: %q", msg)
		}
	})
}

func TestConflictOnStaleRevision(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		var r training.BatchResult
		callOK(t, cs, "log_sets", map[string]any{"sets": []any{map[string]any{"exercise": "ohp", "weight_kg": 40, "reps": 6}}}, &r)
		set := r.Sets[0]

		var upd SetOut
		callOK(t, cs, "update_set", map[string]any{"set_id": set.ID, "rpe": 8, "expected_revision": set.Revision}, &upd)
		if upd.Set.Revision <= set.Revision {
			t.Fatalf("revision did not advance: %d -> %d", set.Revision, upd.Set.Revision)
		}
		// The original revision is now stale.
		msg := callErr(t, cs, "update_set", map[string]any{"set_id": set.ID, "reps": 5, "expected_revision": set.Revision})
		mustContain(t, msg, "conflict", "re-read", "Re-read the current state", "Nothing was written")
		msg = callErr(t, cs, "delete_set", map[string]any{"set_id": set.ID, "expected_revision": set.Revision})
		mustContain(t, msg, "conflict", "re-read")

		w := getWorkout(t, cs, "")
		msg = callErr(t, cs, "log_sets", map[string]any{
			"sets":              []any{map[string]any{"exercise": "ohp", "weight_kg": 40, "reps": 6}},
			"expected_revision": w.Workout.Revision - 1,
		})
		mustContain(t, msg, "conflict", "workout 2026-03-10 is at revision")
		var ok training.BatchResult
		callOK(t, cs, "log_sets", map[string]any{
			"sets":              []any{map[string]any{"exercise": "ohp", "weight_kg": 40, "reps": 6}},
			"expected_revision": w.Workout.Revision,
		}, &ok)

		var got GetSetOut
		callOK(t, cs, "get_set", map[string]any{"set_id": set.ID}, &got)
		if *got.Set.Reps != 6 || *got.Set.RPE != 8 {
			t.Fatalf("stale writes changed the set: %+v", got.Set)
		}
		var del DeleteOut
		callOK(t, cs, "delete_set", map[string]any{"set_id": set.ID, "expected_revision": got.Set.Revision}, &del)
		if del.DeletedSetID != set.ID || del.Remaining != 1 {
			t.Fatalf("delete = %+v", del)
		}
	})
}

func TestImportExportPlanRoundTrip(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		doc := map[string]any{
			"format": "workout-plan", "version": 1, "name": "Upper A", "notes": "Synthetic plan.",
			"items": []any{
				map[string]any{"exercise": "bench press", "sets": 3, "rep_min": 6, "rep_max": 8, "target_weight_kg": 60, "target_rpe": 8},
				map[string]any{"exercise": "barbell row", "sets": 3, "rep_min": 8, "rep_max": 10, "target_rir": 2, "superset": "A"},
			},
		}
		var dry ImportPlanOut
		callOK(t, cs, "import_plan", map[string]any{"plan": doc, "dry_run": true}, &dry)
		if !dry.Result.DryRun || !dry.Result.Created || len(dry.Result.NewExercises) != 2 {
			t.Fatalf("dry run = %+v", dry.Result)
		}
		var plans PlansOut
		callOK(t, cs, "list_plans", nil, &plans)
		if len(plans.Plans) != 0 {
			t.Fatalf("dry run wrote a plan: %+v", plans.Plans)
		}

		var imported ImportPlanOut
		callOK(t, cs, "import_plan", map[string]any{"plan": doc}, &imported)
		id := imported.Result.Plan.ID
		if id == 0 || !imported.Result.Created || imported.Result.DryRun {
			t.Fatalf("import = %+v", imported.Result)
		}

		var exported ExportPlanOut
		callOK(t, cs, "export_plan", map[string]any{"plan_id": id}, &exported)
		gotJSON, _ := json.Marshal(exported.Plan)
		var got, want map[string]any
		_ = json.Unmarshal(gotJSON, &got)
		wantJSON, _ := json.Marshal(doc)
		_ = json.Unmarshal(wantJSON, &want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("export does not round-trip:\n got %s\nwant %s", gotJSON, wantJSON)
		}

		msg := callErr(t, cs, "import_plan", map[string]any{"plan": doc})
		mustContain(t, msg, "conflict", "Upper A", "on_conflict=replace")

		var plan PlanOut
		callOK(t, cs, "get_plan", map[string]any{"plan_id": id}, &plan)
		if plan.Plan.Revision < 1 || plan.Plan.Items[0].TargetWeightKG == nil || *plan.Plan.Items[0].TargetWeightKG != 60 {
			t.Fatalf("get_plan = %+v", plan.Plan)
		}

		// Replace with the exported document, edited, guarded by revision.
		edited := exported.Plan
		edited.Items[0].Sets = 4
		var replaced PlanOut
		callOK(t, cs, "replace_plan", map[string]any{"plan_id": id, "plan": edited, "expected_revision": plan.Plan.Revision}, &replaced)
		if replaced.Plan.ID != id || replaced.Plan.Items[0].TargetSets != 4 || replaced.Plan.Revision <= plan.Plan.Revision {
			t.Fatalf("replace = %+v", replaced.Plan)
		}
		msg = callErr(t, cs, "replace_plan", map[string]any{"plan_id": id, "plan": edited, "expected_revision": plan.Plan.Revision})
		mustContain(t, msg, "conflict", "re-read")

		var again ImportPlanOut
		callOK(t, cs, "import_plan", map[string]any{"plan": doc, "on_conflict": "replace"}, &again)
		if !again.Result.Replaced || again.Result.Plan.ID != id {
			t.Fatalf("replace import = %+v", again.Result)
		}

		bad := map[string]any{
			"format": "workout-plan", "version": 1, "name": "Broken",
			"items": []any{map[string]any{"exercise": "curl", "sets": 0, "tempo": "3-1-1"}},
		}
		msg = callErr(t, cs, "import_plan", map[string]any{"plan": bad, "dry_run": true})
		mustContain(t, msg, "validation error", "items[0].sets", "items[0].tempo: unknown field")

		var del PlanDeleteOut
		callOK(t, cs, "delete_plan", map[string]any{"plan_id": id}, &del)
		if !del.Deleted {
			t.Fatalf("delete_plan = %+v", del)
		}
	})
}

func TestPartialValuesStayUnrecorded(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		res := call(t, cs, "log_sets", map[string]any{"sets": []any{
			map[string]any{"exercise": "pull-up", "reps": 10},
			map[string]any{"exercise": "pull-up", "weight_kg": 0, "reps": 8, "set_type": "failure"},
			map[string]any{"exercise": "pull-up"},
		}})
		if res.IsError {
			t.Fatal(text(res))
		}
		raw := structured(t, res)
		mustContain(t, raw, `"weight_kg":null`, `"rpe":null`, `"rir":null`, `"weight_kg":0`, `"reps":null`)
		var r training.BatchResult
		_ = json.Unmarshal([]byte(raw), &r)
		if r.Sets[0].WeightKG != nil || r.Sets[1].WeightKG == nil || *r.Sets[1].WeightKG != 0 || r.Sets[2].Reps != nil {
			t.Fatalf("sets = %+v", r.Sets)
		}

		var upd SetOut
		callOK(t, cs, "update_set", map[string]any{"set_id": r.Sets[1].ID, "clear": []any{"weight_kg", "reps"}}, &upd)
		if upd.Set.WeightKG != nil || upd.Set.Reps != nil || upd.Set.SetType != "failure" {
			t.Fatalf("clear = %+v", upd.Set)
		}

		var stats StatsOut
		callOK(t, cs, "exercise_stats", map[string]any{"exercise": "pull-up"}, &stats)
		if stats.Stats.WorkingSets != 3 || stats.Stats.VolumeKG != 0 || len(stats.Stats.Points) != 1 || stats.Stats.Points[0].UnrecordedSets != 3 {
			t.Fatalf("stats = %+v", stats.Stats)
		}
	})
}

func TestToolsListAnnotationsAndContract(t *testing.T) {
	s := New(newService(t, ""))
	cs := connectInMemory(t, s)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		byName[tool.Name] = tool
		if tool.Annotations == nil || tool.Annotations.Title == "" {
			t.Errorf("%s: missing annotations", tool.Name)
			continue
		}
		a := tool.Annotations
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("%s: openWorldHint should be false", tool.Name)
		}
		if !a.ReadOnlyHint && a.DestructiveHint == nil {
			t.Errorf("%s: write tool without explicit destructiveHint", tool.Name)
		}
		if tool.InputSchema == nil || tool.Description == "" {
			t.Errorf("%s: missing schema or description", tool.Name)
		}
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, s.ToolNames()) {
		t.Fatalf("ToolNames() = %v, listed %v", s.ToolNames(), names)
	}
	required := []string{
		"get_workout", "log_sets", "log_set", "add_set", "update_set", "delete_set", "get_set", "get_session", "list_sessions",
		"start_session", "delete_session", "create_plan", "list_plans", "get_plan", "update_plan", "set_plan_exercise",
		"remove_plan_exercise", "move_plan_exercise", "delete_plan", "import_plan", "export_plan", "replace_plan",
		"session_progress", "add_session_exercise", "adjust_session_exercise", "swap_session_exercise",
		"remove_session_exercise", "save_session_as_plan", "list_exercises", "add_exercise_alias",
		"remove_exercise_alias", "rename_exercise", "exercise_stats", "training_summary", "exercise_history",
		"weekly_volume", "volume_by_muscle", "suggest_load", "set_exercise_note", "list_exercise_notes",
		"set_exercise_group", "list_exercise_groups", "record_feedback", "volume_recommendation",
		"set_volume_landmarks", "list_volume_landmarks",
		"get_exercise_guide", "set_exercise_instructions", "list_set_videos", "delete_video",
		"set_plan_schedule", "get_schedule",
		"add_nutrition_entry", "get_nutrition_entry", "list_nutrition_entries", "update_nutrition_entry",
		"delete_nutrition_entry", "copy_nutrition_entry", "get_nutrition_day", "get_nutrition_targets", "set_nutrition_targets",
		"suggest_nutrition_items",
		"log_weigh_in", "get_weigh_in", "delete_weigh_in", "get_weight_trend",
	}
	for _, n := range required {
		if byName[n] == nil {
			t.Errorf("tool %s is not registered", n)
		}
	}
	for _, n := range []string{"get_workout", "get_session", "list_sessions", "get_plan", "export_plan", "list_exercises", "exercise_stats", "training_summary", "exercise_history", "get_set", "get_exercise_guide", "list_set_videos", "get_schedule", "get_nutrition_entry", "list_nutrition_entries", "get_nutrition_day", "get_nutrition_targets", "suggest_nutrition_items", "get_weigh_in", "get_weight_trend"} {
		if tool := byName[n]; tool != nil && !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s should be read-only", n)
		}
	}
	for _, n := range []string{"delete_set", "delete_session", "delete_plan", "update_set", "replace_plan", "rename_exercise", "remove_exercise_alias", "delete_video", "set_plan_schedule", "update_nutrition_entry", "delete_nutrition_entry", "set_nutrition_targets", "log_weigh_in", "delete_weigh_in"} {
		if tool := byName[n]; tool != nil && (tool.Annotations.ReadOnlyHint || !*tool.Annotations.DestructiveHint) {
			t.Errorf("%s should be destructive", n)
		}
	}
	for _, n := range []string{"log_sets", "log_set", "add_set", "add_nutrition_entry", "copy_nutrition_entry"} {
		if tool := byName[n]; tool != nil && (*tool.Annotations.DestructiveHint || tool.Annotations.IdempotentHint || tool.Annotations.ReadOnlyHint) {
			t.Errorf("%s should be additive and not idempotent without a key", n)
		}
	}
	if !byName["start_session"].Annotations.IdempotentHint {
		t.Error("start_session is get-or-create and should be idempotent")
	}

	logSets := byName["log_sets"].Description
	mustContain(t, logSets, "kilograms", "1-10", "0-10", "not recorded", "never invent", "idempotency_key", "same key and the same payload replays",
		"different payload is rejected as a conflict", "global", "YYYY-MM-DD", "configured timezone", "never create completed sets", "'workout' and a 'session' are the same")
	mustContain(t, byName["start_session"].Description, "Get-or-create", "same thing")
	mustContain(t, byName["get_workout"].Description, "without creating", "timezone", "stable id", "set_number")
	mustContain(t, byName["delete_set"].Description, "confirm")
	for _, n := range []string{"get_exercise_guide", "list_set_videos", "delete_video"} {
		mustContain(t, byName[n].Description, "/api/v1/sets/{id}/videos", "/api/v1/exercises/{ref}/videos", "metadata only", "never the video content", "disabled")
	}
	mustContain(t, byName["set_exercise_instructions"].Description, "never invent")
	mustContain(t, byName["delete_video"].Description, "confirm")
	mustContain(t, logSets, "target_sets")
	for _, n := range []string{"add_nutrition_entry", "copy_nutrition_entry"} {
		mustContain(t, byName[n].Description, "configured timezone", "Asia/Jerusalem", "never creates", "same key and the same payload replays", "conflict", "no key or a new key")
	}
	mustContain(t, byName["add_nutrition_entry"].Description, "kcal", "grams", "WHOLE portion", "never treated as 0", "never recomputed",
		"prefer logging it as items", "values for THAT amount", "never send 0 for unknown", "computed as the sums", "suggest_nutrition_items")
	mustContain(t, byName["update_nutrition_entry"].Description, "WHOLE new list", "recomputed", "[] removes the items")
	mustContain(t, byName["suggest_nutrition_items"].Description, "logged before", "latest values", "scaling them by amount_g", "tell the user you scaled")
	for _, n := range []string{"add_nutrition_entry", "update_nutrition_entry"} {
		items := schemaMap(t, byName[n].InputSchema)["properties"].(map[string]any)["items"].(map[string]any)
		itemProps := items["items"].(map[string]any)["properties"].(map[string]any)
		if items["type"] != "array" || items["maxItems"] != float64(training.MaxNutritionItems) || itemProps["amount_g"].(map[string]any)["type"] != "number" || itemProps["name"] == nil {
			t.Errorf("%s items schema = %v", n, items)
		}
	}
	mustContain(t, byName["set_nutrition_targets"].Description, "never invent", "Earlier dates never change", "kcal", "grams")
	mustContain(t, byName["delete_nutrition_entry"].Description, "confirm")
	// log_weigh_in replaces the day's value (destructive) but repeating it is harmless (idempotent).
	for _, n := range []string{"log_weigh_in", "delete_weigh_in"} {
		if !byName[n].Annotations.IdempotentHint {
			t.Errorf("%s should be idempotent", n)
		}
	}
	mustContain(t, byName["log_weigh_in"].Description, "kilograms", "pounds", "REPLACES", "retry", "configured timezone", "Asia/Jerusalem", "future", "never creates")
	mustContain(t, byName["delete_weigh_in"].Description, "confirm")
	mustContain(t, byName["get_weight_trend"].Description, "WORKOUT_WEEK_START", "only the days that have a weigh-in", "previous week", "never invent", "weekly average")
	mustContain(t, byName["get_weigh_in"].Description, "found=false")
	if !byName["set_plan_schedule"].Annotations.IdempotentHint {
		t.Error("set_plan_schedule should be idempotent")
	}
	mustContain(t, byName["set_plan_schedule"].Description, "NEVER creates or changes a workout", "mon..sun", "configured timezone", "WORKOUT_WEEK_START", "on_conflict='replace'", "clear")
	mustContain(t, byName["get_schedule"].Description, "never creates a workout", "done =", "started =", "missed =", "due =", "upcoming =", "Sunday to Saturday by default")

	schema := schemaMap(t, byName["log_sets"].InputSchema)
	setProps := schema["properties"].(map[string]any)["sets"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if got := stringSlice(setProps["set_type"].(map[string]any)["enum"]); !reflect.DeepEqual(got, training.SetTypes) {
		t.Errorf("set_type enum = %v", got)
	}
	if setProps["weight_kg"].(map[string]any)["type"] != "number" || setProps["reps"].(map[string]any)["type"] != "integer" {
		t.Errorf("optional metrics should be plain non-nullable types: %v / %v", setProps["weight_kg"], setProps["reps"])
	}
	if req := stringSlice(schema["required"]); len(req) != 0 {
		t.Errorf("log_sets required = %v, want none", req)
	}
	upd := schemaMap(t, byName["update_set"].InputSchema)
	if got := stringSlice(upd["properties"].(map[string]any)["clear"].(map[string]any)["items"].(map[string]any)["enum"]); !reflect.DeepEqual(got, []string{"weight_kg", "reps", "rpe", "rir"}) {
		t.Errorf("clear enum = %v", got)
	}
	if req := stringSlice(upd["required"]); !reflect.DeepEqual(req, []string{"set_id"}) {
		t.Errorf("update_set required = %v", req)
	}
	imp := schemaMap(t, byName["import_plan"].InputSchema)
	if req := stringSlice(imp["properties"].(map[string]any)["plan"].(map[string]any)["required"]); !reflect.DeepEqual(req, []string{"format", "version", "name", "items"}) {
		t.Errorf("import_plan plan required = %v", req)
	}
	for _, n := range []string{"create_plan", "set_plan_exercise", "add_session_exercise", "adjust_session_exercise"} {
		sm := schemaMap(t, byName[n].InputSchema)
		props := sm["properties"].(map[string]any)
		if n == "create_plan" {
			props = props["items"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
		}
		if props["target_weight_kg"] == nil || props["target_rir"] == nil {
			t.Errorf("%s lacks target_weight_kg/target_rir: %v", n, props)
		}
	}
}

func TestHTTPAuthRejectsMissingOrWrongBearer(t *testing.T) {
	h := httpServer(t, New(newService(t, "")))
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"raw","version":"1"}}}`
	for name, header := range map[string]string{"missing": "", "wrong": "Bearer wrong-token", "not-bearer": "Basic " + testToken} {
		req, _ := http.NewRequest(http.MethodPost, h.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s bearer: status %d, want 401", name, resp.StatusCode)
		}
	}
	if cs, err := dialHTTP(h.URL, ""); err == nil {
		cs.Close()
		t.Error("client connected without a token")
	}
	if cs, err := dialHTTP(h.URL, "wrong-token"); err == nil {
		cs.Close()
		t.Error("client connected with a wrong token")
	}
	cs, err := dialHTTP(h.URL, testToken)
	if err != nil {
		t.Fatalf("correct token rejected: %v", err)
	}
	defer cs.Close()
	if w := getWorkout(t, cs, ""); w.Exists {
		t.Fatal("fresh database has a workout")
	}
}

// TestLegacyAndCatalogueTools keeps coverage of the tools that predate engine
// v2 and of the exercise identity tools.
func TestLegacyAndCatalogueTools(t *testing.T) {
	transports(t, func(t *testing.T, cs *mcp.ClientSession) {
		var empty ListOut
		res := call(t, cs, "list_sessions", map[string]any{})
		if raw := structured(t, res); raw != `{"sessions":[]}` {
			t.Fatalf("empty list_sessions = %s", raw)
		}
		_ = empty

		var started, again SessionOut
		callOK(t, cs, "start_session", map[string]any{"date": "2026-03-08"}, &started)
		callOK(t, cs, "start_session", map[string]any{"date": "2026-03-08"}, &again)
		if started.Session.ID == 0 || again.Session.ID != started.Session.ID {
			t.Fatalf("start_session is not get-or-create: %d vs %d", started.Session.ID, again.Session.ID)
		}

		var added SetOut
		callOK(t, cs, "add_set", map[string]any{"session_id": started.Session.ID, "exercise": " Bench ", "weight_kg": 80, "reps": 5, "rpe": 8}, &added)
		if added.Set.Exercise != "bench" || added.Set.SI != 1 || added.TotalSI != 1 {
			t.Fatalf("add_set = %+v", added)
		}
		var noRPE SetOut
		callOK(t, cs, "add_set", map[string]any{"session_id": started.Session.ID, "exercise": "bench", "weight_kg": 80, "reps": 5}, &noRPE)
		if noRPE.Set.RPE != nil || noRPE.Set.SI != 0 {
			t.Fatalf("add_set without rpe = %+v", noRPE.Set)
		}

		var sess SessionOut
		callOK(t, cs, "get_session", map[string]any{"session_id": started.Session.ID}, &sess)
		if len(sess.Session.Sets) != 2 || sess.Session.Date != "2026-03-08" {
			t.Fatalf("get_session = %+v", sess.Session)
		}
		var list ListOut
		callOK(t, cs, "list_sessions", map[string]any{"from": "2026-03-01", "to": "2026-03-31"}, &list)
		if len(list.Sessions) != 1 || list.Sessions[0].SetCount != 2 {
			t.Fatalf("list_sessions = %+v", list.Sessions)
		}

		// Plans with the new targets, then a workout following one.
		var plan PlanOut
		callOK(t, cs, "create_plan", map[string]any{"name": "Push", "items": []any{
			map[string]any{"exercise": "bench", "target_sets": 3, "rep_min": 5, "rep_max": 8, "target_weight_kg": 82.5, "target_rir": 2},
		}}, &plan)
		if it := plan.Plan.Items[0]; it.TargetWeightKG == nil || *it.TargetWeightKG != 82.5 || it.TargetRIR == nil || *it.TargetRIR != 2 {
			t.Fatalf("create_plan = %+v", plan.Plan)
		}
		callOK(t, cs, "set_plan_exercise", map[string]any{"plan_id": plan.Plan.ID, "exercise": "dip", "target_sets": 2, "target_rpe": 9}, &plan)
		callOK(t, cs, "move_plan_exercise", map[string]any{"plan_id": plan.Plan.ID, "exercise": "dip", "direction": "up"}, &plan)
		if plan.Plan.Items[0].Exercise != "dip" {
			t.Fatalf("move = %+v", plan.Plan.Items)
		}
		callOK(t, cs, "update_plan", map[string]any{"plan_id": plan.Plan.ID, "notes": "synthetic"}, &plan)
		callOK(t, cs, "remove_plan_exercise", map[string]any{"plan_id": plan.Plan.ID, "exercise": "dip"}, &plan)
		if len(plan.Plan.Items) != 1 || plan.Plan.Notes != "synthetic" {
			t.Fatalf("plan edits = %+v", plan.Plan)
		}

		var today SessionOut
		callOK(t, cs, "start_session", map[string]any{"plan_id": plan.Plan.ID}, &today)
		var item ItemOut
		callOK(t, cs, "add_session_exercise", map[string]any{"session_id": today.Session.ID, "exercise": "fly", "target_sets": 2, "target_weight_kg": 14}, &item)
		callOK(t, cs, "adjust_session_exercise", map[string]any{"session_id": today.Session.ID, "exercise": "bench", "target_rir": 1, "target_weight_kg": 85}, &item)
		var bench training.PlanProgress
		for _, p := range item.Progress {
			if p.Exercise == "bench" {
				bench = p
			}
		}
		if bench.TargetRIR == nil || *bench.TargetRIR != 1 || *bench.TargetWeightKG != 85 || bench.DoneSets != 0 || bench.TargetSets != 3 {
			t.Fatalf("adjusted bench = %+v", bench)
		}
		callOK(t, cs, "swap_session_exercise", map[string]any{"session_id": today.Session.ID, "exercise": "fly", "replacement": "cable fly"}, &item)
		callOK(t, cs, "remove_session_exercise", map[string]any{"session_id": today.Session.ID, "exercise": "cable fly"}, &item)
		var prog ProgressOut
		callOK(t, cs, "session_progress", map[string]any{"session_id": today.Session.ID}, &prog)
		if prog.PlanName != "Push" || len(prog.Progress) != 1 {
			t.Fatalf("session_progress = %+v", prog)
		}
		var saved PlanOut
		callOK(t, cs, "save_session_as_plan", map[string]any{"session_id": today.Session.ID, "name": "Push v2"}, &saved)
		if saved.Plan.ID == 0 || saved.Plan.Items[0].TargetWeightKG == nil {
			t.Fatalf("save_session_as_plan = %+v", saved.Plan)
		}

		// Exercise identity.
		var exs ExercisesOut
		callOK(t, cs, "list_exercises", nil, &exs)
		var benchID int64
		for _, e := range exs.Exercises {
			if e.Name == "bench" {
				benchID = e.ID
			}
		}
		if benchID == 0 {
			t.Fatalf("list_exercises = %+v", exs.Exercises)
		}
		var ex ExerciseOut
		callOK(t, cs, "rename_exercise", map[string]any{"exercise": "bench", "name": "bench press"}, &ex)
		if ex.Exercise.ID != benchID || ex.Exercise.Name != "bench press" {
			t.Fatalf("rename = %+v", ex.Exercise)
		}
		callOK(t, cs, "add_exercise_alias", map[string]any{"exercise_id": benchID, "alias": "BP"}, &ex)
		if !reflect.DeepEqual(ex.Exercise.Aliases, []string{"bp"}) {
			t.Fatalf("alias = %+v", ex.Exercise)
		}
		msg := callErr(t, cs, "add_exercise_alias", map[string]any{"exercise": "dip", "alias": "bp"})
		mustContain(t, msg, "conflict")
		var r training.BatchResult
		callOK(t, cs, "log_set", map[string]any{"date": "2026-03-09", "exercise": "bp", "weight_kg": 85, "reps": 3, "count": 2}, &r)
		if r.Sets[0].ExerciseID != benchID || r.Sets[0].Exercise != "bench press" || len(r.NewExercises) != 0 {
			t.Fatalf("alias did not resolve: %+v", r)
		}
		var removed RemoveAliasOut
		callOK(t, cs, "remove_exercise_alias", map[string]any{"alias": "bp"}, &removed)
		if !removed.Removed {
			t.Fatalf("remove alias = %+v", removed)
		}
		var near training.BatchResult
		callOK(t, cs, "log_sets", map[string]any{"date": "2026-03-09", "sets": []any{map[string]any{"exercise": "bench-press", "reps": 5}}}, &near)
		if len(near.NewExercises) != 1 || !reflect.DeepEqual(near.NewExercises[0].Similar, []string{"bench press"}) {
			t.Fatalf("new_exercises similar = %+v", near.NewExercises)
		}

		// Analytics.
		var stats StatsOut
		callOK(t, cs, "exercise_stats", map[string]any{"exercise": "bench press", "from": "2026-03-01", "to": "2026-03-10"}, &stats)
		if stats.Stats.Workouts != 2 || stats.Stats.WorkingSets != 4 || stats.Stats.MaxWeight == nil || stats.Stats.MaxWeight.Value != 85 {
			t.Fatalf("exercise_stats = %+v", stats.Stats)
		}
		var sum SummaryOut
		callOK(t, cs, "training_summary", map[string]any{}, &sum)
		if sum.Summary.To != "2026-03-10" || sum.Summary.From != "2026-02-11" || sum.Summary.WorkingSets != 5 {
			t.Fatalf("training_summary = %+v", sum.Summary)
		}
		var hist HistoryOut
		callOK(t, cs, "exercise_history", map[string]any{"exercise": "bench press"}, &hist)
		if len(hist.History.Sets) != 4 {
			t.Fatalf("exercise_history = %+v", hist.History)
		}
		var weekly WeeklyOut
		callOK(t, cs, "weekly_volume", map[string]any{}, &weekly)
		var vol VolumeOut
		callOK(t, cs, "volume_by_muscle", map[string]any{}, &vol)
		var group GroupOut
		callOK(t, cs, "set_exercise_group", map[string]any{"exercise": "bench press", "muscle_group": "pecho"}, &group)
		var groups GroupsOut
		callOK(t, cs, "list_exercise_groups", nil, &groups)
		if len(groups.Groups) != 1 {
			t.Fatalf("groups = %+v", groups)
		}
		var notes NotesOut
		callOK(t, cs, "set_exercise_note", map[string]any{"exercise": "bench press", "note": "grip 81cm"}, &notes)
		callOK(t, cs, "list_exercise_notes", nil, &notes)
		if len(notes.Notes) != 1 {
			t.Fatalf("notes = %+v", notes)
		}
		var load LoadSuggestionOut
		callOK(t, cs, "suggest_load", map[string]any{"exercise": "bench press"}, &load)
		if load.Suggestion.Reason == "" {
			t.Fatalf("suggest_load = %+v", load)
		}
		var fb FeedbackOut
		callOK(t, cs, "record_feedback", map[string]any{"session_id": started.Session.ID, "muscle_group": "pecho", "fatigue": 1, "pump": 2, "recovery": 1}, &fb)
		if len(fb.Feedback) != 1 {
			t.Fatalf("feedback = %+v", fb)
		}
		var lm LandmarksOut
		callOK(t, cs, "set_volume_landmarks", map[string]any{"muscle_group": "pecho", "mev": 8, "mrv": 20}, &lm)
		callOK(t, cs, "list_volume_landmarks", nil, &lm)
		var rec RecommendOut
		callOK(t, cs, "volume_recommendation", nil, &rec)

		var delSess DeleteSessionOut
		callOK(t, cs, "delete_session", map[string]any{"session_id": started.Session.ID}, &delSess)
		if delSess.DeletedSets != 2 {
			t.Fatalf("delete_session = %+v", delSess)
		}
	})
}

// failingStore fails one read with an unexpected storage error.
type failingStore struct{ training.Store }

func (failingStore) ListSessions(context.Context, training.ListFilter) ([]training.SessionSummary, error) {
	return nil, errors.New("sql: database is locked at /secret/path.db")
}

func TestUnexpectedErrorsAreMasked(t *testing.T) {
	svc := training.NewService(failingStore{}, func() time.Time { return testNow })
	cs := connectInMemory(t, New(svc))
	if msg := callErr(t, cs, "list_sessions", map[string]any{}); msg != "internal tool error" {
		t.Fatalf("message = %q", msg)
	}
}

// TestRunStdio drives RunStdio in a child process (this test binary) through
// the SDK's command transport.
func TestRunStdio(t *testing.T) {
	if os.Getenv("MCPSRV_STDIO_CHILD") == "1" {
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStdioChild$")
	cmd.Env = append(os.Environ(), "MCPSRV_STDIO_CHILD=1", "MCPSRV_STDIO_DB="+filepath.Join(t.TempDir(), "stdio.db"))
	cmd.Stderr = os.Stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-client", Version: "1.0.0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	var r training.BatchResult
	callOK(t, cs, "log_sets", map[string]any{"sets": []any{map[string]any{"exercise": "squat", "weight_kg": 100, "reps": 5}}}, &r)
	w := getWorkout(t, cs, "")
	if !w.Exists || len(w.Workout.Sets) != 1 || w.Workout.Sets[0].ID != r.Sets[0].ID {
		t.Fatalf("stdio get_workout = %+v", w)
	}
}

func TestStdioChild(t *testing.T) {
	if os.Getenv("MCPSRV_STDIO_CHILD") != "1" {
		t.Skip("helper process for TestRunStdio")
	}
	s := New(newService(t, os.Getenv("MCPSRV_STDIO_DB")))
	if err := s.RunStdio(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		t.Logf("RunStdio: %v", err)
	}
}

func schemaMap(t testing.TB, schema any) map[string]any {
	t.Helper()
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func stringSlice(v any) []string {
	vs, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(vs))
	for _, x := range vs {
		out = append(out, x.(string))
	}
	return out
}
