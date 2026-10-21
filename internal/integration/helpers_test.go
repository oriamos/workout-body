// Package integration exercises the engine end to end: the real SQLite store
// behind the real training.Service, with no mocks.
package integration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, db: filepath.Join(t.TempDir(), "test.db"), now: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)}
	st, err := sqlitestore.Open(e.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e.st = st
	e.svc = training.NewService(st, func() time.Time { return e.now }).WithLocation(jerusalem)
	return e
}

func ctx() context.Context { return context.Background() }

func f(v float64) *float64 { return &v }
func n(v int) *int         { return &v }

func set(exercise string, kg float64, reps int) training.AddSetInput {
	return training.AddSetInput{Exercise: exercise, WeightKG: f(kg), Reps: n(reps)}
}

func batch(date string, sets ...training.AddSetInput) training.BatchInput {
	return training.BatchInput{Date: date, Sets: sets}
}
