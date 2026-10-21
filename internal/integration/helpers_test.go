// Package integration exercises the engine end to end: the real SQLite store
// behind the real training.Service, with no mocks.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // tests must not depend on the host's zoneinfo

	_ "modernc.org/sqlite"

	"github.com/fullfran/training-mcp/internal/sqlitestore"
	"github.com/fullfran/training-mcp/internal/training"
)

var jerusalem = mustLoc("Asia/Jerusalem")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// env is a service over a fresh database file with a controllable clock.
type env struct {
	t   *testing.T
	svc *training.Service
	st  *sqlitestore.Store
	now time.Time
	db  string
	raw *sql.DB
}

func newEnv(t *testing.T) *env {
	t.Helper()
	return openEnv(t, filepath.Join(t.TempDir(), "test.db"))
}

// openEnv opens (and migrates) the database at path.
func openEnv(t *testing.T, path string) *env {
	t.Helper()
	e := &env{t: t, db: path, now: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)}
	st, err := sqlitestore.Open(e.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e.st = st
	e.svc = training.NewService(st, func() time.Time { return e.now }).WithLocation(jerusalem)
	return e
}

// at moves the clock (given in UTC).
func (e *env) at(utc string) {
	e.t.Helper()
	ts, err := time.Parse(time.RFC3339, utc)
	if err != nil {
		e.t.Fatal(err)
	}
	e.now = ts
}

// sqlDB is a second, independent connection to the same file, used to assert
// on rows directly rather than only through the service under test.
func (e *env) sqlDB() *sql.DB {
	e.t.Helper()
	if e.raw == nil {
		db, err := sql.Open("sqlite", e.db+"?_pragma=busy_timeout(10000)")
		if err != nil {
			e.t.Fatal(err)
		}
		e.t.Cleanup(func() { db.Close() })
		e.raw = db
	}
	return e.raw
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.sqlDB().QueryRow(query, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (e *env) log(in training.BatchInput) training.BatchResult {
	e.t.Helper()
	res, err := e.svc.LogSets(ctx(), in)
	if err != nil {
		e.t.Fatalf("LogSets(%+v): %v", in, err)
	}
	return res
}

func (e *env) workout(date string) training.Session {
	e.t.Helper()
	w, err := e.svc.GetWorkout(ctx(), date)
	if err != nil {
		e.t.Fatalf("GetWorkout(%q): %v", date, err)
	}
	return w
}

func (e *env) set(id int64) training.Set {
	e.t.Helper()
	s, err := e.svc.GetSet(ctx(), id)
	if err != nil {
		e.t.Fatalf("GetSet(%d): %v", id, err)
	}
	return s
}

func (e *env) exerciseNames() []string {
	e.t.Helper()
	all, err := e.svc.Exercises(ctx())
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, x := range all {
		out = append(out, x.Name)
	}
	sort.Strings(out)
	return out
}

func ctx() context.Context { return context.Background() }

func f(v float64) *float64 { return &v }
func n(v int) *int         { return &v }
func s(v string) *string   { return &v }

func set(exercise string, kg float64, reps int) training.AddSetInput {
	return training.AddSetInput{Exercise: exercise, WeightKG: f(kg), Reps: n(reps)}
}

func warmup(exercise string, kg float64, reps int) training.AddSetInput {
	in := set(exercise, kg, reps)
	in.SetType = training.SetTypeWarmup
	return in
}

func batch(date string, sets ...training.AddSetInput) training.BatchInput {
	return training.BatchInput{Date: date, Sets: sets}
}

func rx(exercise string, sets, repMin, repMax int, kg *float64) training.PlanItem {
	return training.PlanItem{Exercise: exercise, TargetSets: sets, RepMin: repMin, RepMax: repMax, TargetWeightKG: kg}
}

func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("want %v, got %v", target, err)
	}
}

// issueFields returns the field paths of a validation error, failing the test
// when err is not one.
func issueFields(t *testing.T, err error) map[string]string {
	t.Helper()
	wantErr(t, err, training.ErrValidation)
	iss := training.IssuesOf(err)
	if len(iss) == 0 {
		t.Fatalf("validation error without issues: %v", err)
	}
	out := map[string]string{}
	for _, i := range iss {
		out[i.Field] = i.Message
	}
	return out
}

func requireFields(t *testing.T, err error, fields ...string) map[string]string {
	t.Helper()
	got := issueFields(t, err)
	for _, want := range fields {
		if _, ok := got[want]; !ok {
			t.Errorf("missing issue for %q; got %v", want, got)
		}
	}
	return got
}

func progressFor(w training.Session, exercise string) (training.PlanProgress, bool) {
	for _, p := range w.Progress {
		if p.Exercise == exercise {
			return p, true
		}
	}
	return training.PlanProgress{}, false
}

func setsOf(w training.Session, exercise string) []training.Set {
	var out []training.Set
	for _, st := range w.Sets {
		if st.Exercise == exercise {
			out = append(out, st)
		}
	}
	return out
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func eqF(a, b *float64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
func eqI(a, b *int) bool     { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func fmtF(p *float64) string {
	if p == nil {
		return "nil"
	}
	b, _ := json.Marshal(*p)
	return string(b)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func joinFields(m map[string]string) string {
	var k []string
	for f := range m {
		k = append(k, f)
	}
	sort.Strings(k)
	return strings.Join(k, ",")
}
