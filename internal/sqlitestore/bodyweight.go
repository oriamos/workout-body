package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fullfran/training-mcp/internal/training"
)

var _ training.WeightStore = (*Store)(nil)

const weighInCols = `id,date,weight_kg,note,revision,created_at,updated_at`

func scanWeighIn(r scanner) (training.WeighIn, error) {
	var w training.WeighIn
	err := r.Scan(&w.ID, &w.Date, &w.WeightKG, &w.Note, &w.Revision, &w.CreatedAt, &w.UpdatedAt)
	return w, err
}

func (s *Store) UpsertWeighIn(ctx context.Context, w training.WeighIn, expected *int) (training.WeighIn, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.WeighIn{}, false, err
	}
	defer tx.Rollback()
	cur, err := scanWeighIn(tx.QueryRowContext(ctx, `SELECT `+weighInCols+` FROM weigh_ins WHERE date=?`, w.Date))
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return cur, false, err
	}
	if expected != nil {
		have := 0
		if exists {
			have = cur.Revision
		}
		if *expected != have {
			return cur, false, fmt.Errorf("%w: the weigh-in for %s is at revision %d, not %d; re-read it and retry", training.ErrStale, w.Date, have, *expected)
		}
	}
	if exists {
		if cur.WeightKG == w.WeightKG && cur.Note == w.Note {
			return cur, false, tx.Commit() // same values again: nothing to change
		}
		if _, err = tx.ExecContext(ctx, `UPDATE weigh_ins SET weight_kg=?,note=?,revision=revision+1,updated_at=? WHERE date=?`, w.WeightKG, w.Note, w.UpdatedAt, w.Date); err != nil {
			return cur, false, err
		}
	} else if _, err = tx.ExecContext(ctx, `INSERT INTO weigh_ins(date,weight_kg,note,created_at,updated_at) VALUES (?,?,?,?,?)`, w.Date, w.WeightKG, w.Note, w.CreatedAt, w.UpdatedAt); err != nil {
		return cur, false, err
	}
	out, err := scanWeighIn(tx.QueryRowContext(ctx, `SELECT `+weighInCols+` FROM weigh_ins WHERE date=?`, w.Date))
	if err != nil {
		return out, false, err
	}
	return out, !exists, tx.Commit()
}

func (s *Store) GetWeighIn(ctx context.Context, date string) (training.WeighIn, error) {
	w, err := scanWeighIn(s.db.QueryRowContext(ctx, `SELECT `+weighInCols+` FROM weigh_ins WHERE date=?`, date))
	if errors.Is(err, sql.ErrNoRows) {
		return w, fmt.Errorf("%w: no weigh-in on %s", training.ErrNotFound, date)
	}
	return w, err
}

func (s *Store) DeleteWeighIn(ctx context.Context, date string, expected *int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rev int
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM weigh_ins WHERE date=?`, date).Scan(&rev); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: no weigh-in on %s", training.ErrNotFound, date)
	} else if err != nil {
		return err
	}
	if expected != nil && *expected != rev {
		return fmt.Errorf("%w: the weigh-in for %s is at revision %d, not %d; re-read it and retry", training.ErrStale, date, rev, *expected)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM weigh_ins WHERE date=?`, date); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListWeighIns(ctx context.Context, from, to string) ([]training.WeighIn, error) {
	q := `SELECT ` + weighInCols + ` FROM weigh_ins WHERE 1=1`
	var args []any
	if from != "" {
		q += ` AND date>=?`
		args = append(args, from)
	}
	if to != "" {
		q += ` AND date<=?`
		args = append(args, to)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY date`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.WeighIn
	for rows.Next() {
		w, err := scanWeighIn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) LatestWeighIn(ctx context.Context, onOrBefore string) (*training.WeighIn, error) {
	w, err := scanWeighIn(s.db.QueryRowContext(ctx, `SELECT `+weighInCols+` FROM weigh_ins WHERE date<=? ORDER BY date DESC LIMIT 1`, onOrBefore))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}
