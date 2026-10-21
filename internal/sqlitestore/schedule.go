package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/fullfran/training-mcp/internal/training"
)

// encodeSchedule flattens a normalized schedule into the three plan columns.
func encodeSchedule(s *training.Schedule) (typ, days string, times int) {
	if s == nil {
		return "", "", 0
	}
	return s.Type, strings.Join(s.Days, ","), s.TimesPerWeek
}

func decodeSchedule(typ, days string, times int) *training.Schedule {
	switch typ {
	case training.ScheduleWeekdays:
		return &training.Schedule{Type: typ, Days: strings.Split(days, ",")}
	case training.ScheduleTimesPerWeek:
		return &training.Schedule{Type: typ, TimesPerWeek: times}
	}
	return nil
}

func (s *Store) SetPlanSchedule(ctx context.Context, id int64, sched *training.Schedule, expected *int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rev int
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM plans WHERE id=?`, id).Scan(&rev); errors.Is(err, sql.ErrNoRows) {
		return training.ErrNotFound
	} else if err != nil {
		return err
	}
	if expected != nil && *expected != rev {
		return fmt.Errorf("%w: plan %d is at revision %d, not %d; re-read it and retry", training.ErrStale, id, rev, *expected)
	}
	typ, days, times := encodeSchedule(sched)
	if _, err = tx.ExecContext(ctx, `UPDATE plans SET schedule_type=?,schedule_days=?,schedule_times=?,revision=revision+1 WHERE id=?`, typ, days, times, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) WorkoutsInRange(ctx context.Context, from, to string) ([]training.WorkoutDay, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id,s.date,COALESCE(s.plan_id,0),s.plan_name,s.revision,
 (SELECT COUNT(*) FROM sets st WHERE st.session_id=s.id AND st.set_type<>'warmup'),
 (SELECT COUNT(*) FROM sets st WHERE st.session_id=s.id AND st.set_type='warmup')
FROM sessions s WHERE s.date>=? AND s.date<=? ORDER BY s.date`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.WorkoutDay
	for rows.Next() {
		var d training.WorkoutDay
		if err = rows.Scan(&d.ID, &d.Date, &d.PlanID, &d.PlanName, &d.Revision, &d.WorkingSets, &d.WarmupSets); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
