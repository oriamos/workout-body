package sqlitestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/fullfran/training-mcp/internal/training"
)

var _ training.NutritionStore = (*Store)(nil)

const (
	opNutritionAdd  = "nutrition_add"
	opNutritionCopy = "nutrition_copy"
)

const nutritionCols = `id,date,name,calories_kcal,protein_g,carbs_g,fat_g,copied_from,revision,created_at,updated_at`

func scanNutrition(r scanner) (training.NutritionEntry, error) {
	var e training.NutritionEntry
	var cal, pro, carb, fat sql.NullFloat64
	var from sql.NullInt64
	if err := r.Scan(&e.ID, &e.Date, &e.Name, &cal, &pro, &carb, &fat, &from, &e.Revision, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return e, err
	}
	e.CaloriesKcal, e.ProteinG, e.CarbsG, e.FatG = ptrF(cal), ptrF(pro), ptrF(carb), ptrF(fat)
	if from.Valid {
		v := from.Int64
		e.CopiedFrom = &v
	}
	return e, nil
}

// getEntry reads one entry with its items.
func getEntry(ctx context.Context, q dbtx, id int64) (training.NutritionEntry, error) {
	e, err := scanNutrition(q.QueryRowContext(ctx, `SELECT `+nutritionCols+` FROM nutrition_entries WHERE id=?`, id))
	if err != nil {
		return e, err
	}
	list := []training.NutritionEntry{e}
	if err := loadItems(ctx, q, list); err != nil {
		return e, err
	}
	return list[0], nil
}

// loadItems fills Items and IncompleteValues of each entry, in place.
func loadItems(ctx context.Context, q dbtx, entries []training.NutritionEntry) error {
	idx := make(map[int64]int, len(entries))
	for i := range entries {
		entries[i].Items, entries[i].IncompleteValues = []training.NutritionItem{}, []string{}
		idx[entries[i].ID] = i
	}
	const chunk = 500
	for start := 0; start < len(entries); start += chunk {
		end := min(start+chunk, len(entries))
		args := make([]any, 0, end-start)
		marks := make([]string, 0, end-start)
		for _, e := range entries[start:end] {
			args = append(args, e.ID)
			marks = append(marks, "?")
		}
		rows, err := q.QueryContext(ctx, `SELECT entry_id,name,amount_g,calories_kcal,protein_g,carbs_g,fat_g FROM nutrition_items WHERE entry_id IN (`+strings.Join(marks, ",")+`) ORDER BY entry_id, position`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			var it training.NutritionItem
			var amt, cal, pro, carb, fat sql.NullFloat64
			if err := rows.Scan(&id, &it.Name, &amt, &cal, &pro, &carb, &fat); err != nil {
				rows.Close()
				return err
			}
			it.AmountG, it.CaloriesKcal, it.ProteinG, it.CarbsG, it.FatG = ptrF(amt), ptrF(cal), ptrF(pro), ptrF(carb), ptrF(fat)
			i := idx[id]
			entries[i].Items = append(entries[i].Items, it)
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	for i := range entries {
		if len(entries[i].Items) > 0 {
			_, entries[i].IncompleteValues = training.SumItems(entries[i].Items)
		}
	}
	return nil
}

// replaceItems stores the item list of an entry, replacing any existing one.
func replaceItems(ctx context.Context, tx *sql.Tx, id int64, items []training.NutritionItem) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM nutrition_items WHERE entry_id=?`, id); err != nil {
		return err
	}
	for i, it := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO nutrition_items(entry_id,position,name,amount_g,calories_kcal,protein_g,carbs_g,fat_g) VALUES (?,?,?,?,?,?,?,?)`,
			id, i, it.Name, nf(it.AmountG), nf(it.CaloriesKcal), nf(it.ProteinG), nf(it.CarbsG), nf(it.FatG)); err != nil {
			return err
		}
	}
	return nil
}

func requestHash(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// idempotent runs write inside tx unless key was already used. A key reused
// with the same operation and payload returns the stored entry (replayed);
// with anything else it is a conflict.
func idempotent(ctx context.Context, tx *sql.Tx, key, op string, payload any, write func() (training.NutritionEntry, error)) (training.NutritionEntry, bool, error) {
	if key == "" {
		e, err := write()
		return e, false, err
	}
	hash, err := requestHash(payload)
	if err != nil {
		return training.NutritionEntry{}, false, err
	}
	var gotOp, gotHash, body string
	err = tx.QueryRowContext(ctx, `SELECT operation,request_hash,response_json FROM idempotency_keys WHERE key=?`, key).Scan(&gotOp, &gotHash, &body)
	switch {
	case err == nil:
		if gotOp != op || gotHash != hash {
			return training.NutritionEntry{}, false, fmt.Errorf("%w: idempotency_key %q was already used for a different request; use a new key for a different write", training.ErrConflict, key)
		}
		var prev training.NutritionEntry
		if err := json.Unmarshal([]byte(body), &prev); err != nil {
			return prev, false, err
		}
		// Return the entry as it is now when it still exists.
		if cur, err := getEntry(ctx, tx, prev.ID); err == nil {
			prev = cur
		}
		return prev, true, nil
	case !errors.Is(err, sql.ErrNoRows):
		return training.NutritionEntry{}, false, err
	}
	e, err := write()
	if err != nil {
		return e, false, err
	}
	resp, err := json.Marshal(e)
	if err != nil {
		return e, false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE created_at < datetime('now','-90 days')`); err != nil {
		return e, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys(key,operation,request_hash,response_json,created_at) VALUES (?,?,?,?,CURRENT_TIMESTAMP)`, key, op, hash, string(resp)); err != nil {
		return e, false, err
	}
	return e, false, nil
}

func insertNutrition(ctx context.Context, tx *sql.Tx, e training.NutritionEntry) (training.NutritionEntry, error) {
	var from any
	if e.CopiedFrom != nil {
		from = *e.CopiedFrom
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO nutrition_entries(date,name,calories_kcal,protein_g,carbs_g,fat_g,copied_from,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		e.Date, e.Name, nf(e.CaloriesKcal), nf(e.ProteinG), nf(e.CarbsG), nf(e.FatG), from, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return training.NutritionEntry{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return training.NutritionEntry{}, err
	}
	if err := replaceItems(ctx, tx, id, e.Items); err != nil {
		return training.NutritionEntry{}, err
	}
	return getEntry(ctx, tx, id)
}

func (s *Store) AddNutrition(ctx context.Context, e training.NutritionEntry, key string) (training.NutritionEntry, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.NutritionEntry{}, false, err
	}
	defer tx.Rollback()
	// The payload binding the key excludes timestamps, which differ per try.
	payload := struct {
		Date, Name string
		N          training.Nutrients
		Items      []training.NutritionItem `json:",omitempty"`
	}{e.Date, e.Name, e.Nutrients, e.Items}
	out, replayed, err := idempotent(ctx, tx, key, opNutritionAdd, payload, func() (training.NutritionEntry, error) {
		return insertNutrition(ctx, tx, e)
	})
	if err != nil {
		return out, false, err
	}
	return out, replayed, tx.Commit()
}

func (s *Store) GetNutrition(ctx context.Context, id int64) (training.NutritionEntry, error) {
	e, err := getEntry(ctx, s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		return e, fmt.Errorf("%w: nutrition entry %d does not exist", training.ErrNotFound, id)
	}
	return e, err
}

// ListNutrition returns entries newest date first and, within a date, in the
// order they were logged. A negative limit means no limit (internal use).
func (s *Store) ListNutrition(ctx context.Context, f training.NutritionFilter) ([]training.NutritionEntry, error) {
	q := `SELECT ` + nutritionCols + ` FROM nutrition_entries WHERE 1=1`
	var args []any
	if f.From != "" {
		q += ` AND date>=?`
		args = append(args, f.From)
	}
	if f.To != "" {
		q += ` AND date<=?`
		args = append(args, f.To)
	}
	if f.Query != "" {
		q += ` AND instr(lower(name), lower(?)) > 0`
		args = append(args, f.Query)
	}
	q += ` ORDER BY date DESC, created_at, id`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.NutritionEntry
	for rows.Next() {
		e, err := scanNutrition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, loadItems(ctx, s.db, out)
}

func (s *Store) UpdateNutrition(ctx context.Context, id int64, p training.NutritionPatch, now string) (training.NutritionEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.NutritionEntry{}, err
	}
	defer tx.Rollback()
	e, err := getEntry(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return e, fmt.Errorf("%w: nutrition entry %d does not exist", training.ErrNotFound, id)
	}
	if err != nil {
		return e, err
	}
	setsValues := p.CaloriesKcal != nil || p.ProteinG != nil || p.CarbsG != nil || p.FatG != nil || p.ClearCalories || p.ClearProtein || p.ClearCarbs || p.ClearFat
	if p.Items == nil && len(e.Items) > 0 && setsValues {
		return e, &training.ValidationError{Issues: []training.Issue{{Field: "items", Message: fmt.Sprintf("nutrition entry %d is made of %d ingredients and takes its values from them; change the items instead (or replace items with an empty list to enter the values directly)", id, len(e.Items))}}}
	}
	if p.ExpectedRevision != nil && *p.ExpectedRevision != e.Revision {
		return e, fmt.Errorf("%w: nutrition entry %d is at revision %d, not %d; re-read it and retry", training.ErrStale, id, e.Revision, *p.ExpectedRevision)
	}
	if p.Name != nil {
		e.Name = *p.Name
	}
	if p.Date != nil {
		e.Date = *p.Date
	}
	apply := func(cur **float64, v *float64, clear bool) {
		switch {
		case clear:
			*cur = nil
		case v != nil:
			*cur = v
		}
	}
	apply(&e.CaloriesKcal, p.CaloriesKcal, p.ClearCalories)
	apply(&e.ProteinG, p.ProteinG, p.ClearProtein)
	apply(&e.CarbsG, p.CarbsG, p.ClearCarbs)
	apply(&e.FatG, p.FatG, p.ClearFat)
	if p.Items != nil {
		if err := replaceItems(ctx, tx, id, *p.Items); err != nil {
			return e, err
		}
		if len(*p.Items) > 0 {
			e.Nutrients, _ = training.SumItems(*p.Items)
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE nutrition_entries SET date=?,name=?,calories_kcal=?,protein_g=?,carbs_g=?,fat_g=?,revision=revision+1,updated_at=? WHERE id=?`,
		e.Date, e.Name, nf(e.CaloriesKcal), nf(e.ProteinG), nf(e.CarbsG), nf(e.FatG), now, id); err != nil {
		return e, err
	}
	out, err := getEntry(ctx, tx, id)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) DeleteNutrition(ctx context.Context, id int64, expected *int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rev int
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM nutrition_entries WHERE id=?`, id).Scan(&rev); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: nutrition entry %d does not exist", training.ErrNotFound, id)
	} else if err != nil {
		return err
	}
	if expected != nil && *expected != rev {
		return fmt.Errorf("%w: nutrition entry %d is at revision %d, not %d; re-read it and retry", training.ErrStale, id, rev, *expected)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM nutrition_entries WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CopyNutrition(ctx context.Context, id int64, date, now, key string) (training.NutritionEntry, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.NutritionEntry{}, false, err
	}
	defer tx.Rollback()
	payload := struct {
		ID   int64
		Date string
	}{id, date}
	out, replayed, err := idempotent(ctx, tx, key, opNutritionCopy, payload, func() (training.NutritionEntry, error) {
		src, err := getEntry(ctx, tx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return src, fmt.Errorf("%w: nutrition entry %d does not exist", training.ErrNotFound, id)
		}
		if err != nil {
			return src, err
		}
		from := src.ID
		return insertNutrition(ctx, tx, training.NutritionEntry{Date: date, Name: src.Name, Nutrients: src.Nutrients, Items: src.Items, CopiedFrom: &from, CreatedAt: now, UpdatedAt: now})
	})
	if err != nil {
		return out, false, err
	}
	return out, replayed, tx.Commit()
}

const targetCols = `effective_from,calories_kcal,protein_g,carbs_g,fat_g,revision`

func scanTargets(r scanner) (training.NutritionTargets, error) {
	var t training.NutritionTargets
	var cal, pro, carb, fat sql.NullFloat64
	if err := r.Scan(&t.EffectiveFrom, &cal, &pro, &carb, &fat, &t.Revision); err != nil {
		return t, err
	}
	t.CaloriesKcal, t.ProteinG, t.CarbsG, t.FatG = ptrF(cal), ptrF(pro), ptrF(carb), ptrF(fat)
	return t, nil
}

func targetsAt(ctx context.Context, q dbtx, date string) (training.NutritionTargets, error) {
	t, err := scanTargets(q.QueryRowContext(ctx, `SELECT `+targetCols+` FROM nutrition_targets WHERE effective_from<=? ORDER BY effective_from DESC LIMIT 1`, date))
	if errors.Is(err, sql.ErrNoRows) {
		return training.NutritionTargets{}, nil // no target set yet: everything unset, revision 0
	}
	return t, err
}

func (s *Store) TargetsAt(ctx context.Context, date string) (training.NutritionTargets, error) {
	return targetsAt(ctx, s.db, date)
}

func (s *Store) TargetHistory(ctx context.Context) ([]training.NutritionTargets, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+targetCols+` FROM nutrition_targets ORDER BY effective_from`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.NutritionTargets
	for rows.Next() {
		t, err := scanTargets(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetTargets merges the patch into the targets in effect on `from` and stores
// the result as the change for that date (inserting it, or replacing a change
// already made on that exact date). Later changes are untouched, so the new
// values apply only until the next change; earlier dates never move.
func (s *Store) SetTargets(ctx context.Context, from string, p training.TargetPatch, now string) (training.NutritionTargets, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.NutritionTargets{}, err
	}
	defer tx.Rollback()
	cur, err := targetsAt(ctx, tx, from)
	if err != nil {
		return cur, err
	}
	if p.ExpectedRevision != nil && *p.ExpectedRevision != cur.Revision {
		return cur, fmt.Errorf("%w: the targets in effect on %s are at revision %d, not %d; re-read them and retry", training.ErrStale, from, cur.Revision, *p.ExpectedRevision)
	}
	next := cur.Nutrients
	merge := func(dst **float64, v *float64, clear bool) {
		switch {
		case clear:
			*dst = nil
		case v != nil:
			*dst = v
		}
	}
	merge(&next.CaloriesKcal, p.CaloriesKcal, p.ClearCalories)
	merge(&next.ProteinG, p.ProteinG, p.ClearProtein)
	merge(&next.CarbsG, p.CarbsG, p.ClearCarbs)
	merge(&next.FatG, p.FatG, p.ClearFat)
	res, err := tx.ExecContext(ctx, `INSERT INTO nutrition_target_revisions DEFAULT VALUES`)
	if err != nil {
		return cur, err
	}
	rev, err := res.LastInsertId()
	if err != nil {
		return cur, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO nutrition_targets(effective_from,calories_kcal,protein_g,carbs_g,fat_g,revision,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(effective_from) DO UPDATE SET calories_kcal=excluded.calories_kcal, protein_g=excluded.protein_g,
 carbs_g=excluded.carbs_g, fat_g=excluded.fat_g, revision=excluded.revision, updated_at=excluded.updated_at`,
		from, nf(next.CaloriesKcal), nf(next.ProteinG), nf(next.CarbsG), nf(next.FatG), rev, now, now); err != nil {
		return cur, err
	}
	out, err := targetsAt(ctx, tx, from)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) DeleteTargetChange(ctx context.Context, from string, expected *int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rev int
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM nutrition_targets WHERE effective_from=?`, from).Scan(&rev); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: no target change on %s", training.ErrNotFound, from)
	} else if err != nil {
		return err
	}
	if expected != nil && *expected != rev {
		return fmt.Errorf("%w: the target change on %s is at revision %d, not %d; re-read it and retry", training.ErrStale, from, rev, *expected)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM nutrition_targets WHERE effective_from=?`, from); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecentNutritionItems(ctx context.Context, query string, limit int) ([]training.NutritionItemSuggestion, error) {
	// The latest use of each name: the item on the newest entry (by date,
	// then log order), with the number of times the name was used.
	q := `
WITH ranked AS (
 SELECT i.name, i.amount_g, i.calories_kcal, i.protein_g, i.carbs_g, i.fat_g, e.date,
  ROW_NUMBER() OVER (PARTITION BY lower(i.name) ORDER BY e.date DESC, e.created_at DESC, e.id DESC, i.position) AS rn,
  COUNT(*) OVER (PARTITION BY lower(i.name)) AS uses
 FROM nutrition_items i JOIN nutrition_entries e ON e.id = i.entry_id
 WHERE ? = '' OR instr(lower(i.name), lower(?)) > 0
)
SELECT name, amount_g, calories_kcal, protein_g, carbs_g, fat_g, date, uses FROM ranked WHERE rn = 1
ORDER BY date DESC, uses DESC, lower(name) LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, query, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.NutritionItemSuggestion
	for rows.Next() {
		var g training.NutritionItemSuggestion
		var amt, cal, pro, carb, fat sql.NullFloat64
		if err := rows.Scan(&g.Name, &amt, &cal, &pro, &carb, &fat, &g.LastUsed, &g.Uses); err != nil {
			return nil, err
		}
		g.AmountG, g.CaloriesKcal, g.ProteinG, g.CarbsG, g.FatG = ptrF(amt), ptrF(cal), ptrF(pro), ptrF(carb), ptrF(fat)
		out = append(out, g)
	}
	return out, rows.Err()
}
