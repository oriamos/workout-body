package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fullfran/training-mcp/internal/training"
)

var _ training.MediaStore = (*Store)(nil)

const mediaCols = `id,set_id,exercise_id,kind,storage_key,filename,content_type,size_bytes,sha256,caption,created_at`

func scanMedia(r scanner) (training.Media, error) {
	var m training.Media
	var setID, exID sql.NullInt64
	if err := r.Scan(&m.ID, &setID, &exID, &m.Kind, &m.StorageKey, &m.Filename, &m.ContentType, &m.SizeBytes, &m.SHA256, &m.Caption, &m.CreatedAt); err != nil {
		return m, err
	}
	if setID.Valid {
		m.OwnerType, m.OwnerID = training.OwnerSet, setID.Int64
	} else {
		m.OwnerType, m.OwnerID = training.OwnerExercise, exID.Int64
	}
	return m, nil
}

func (s *Store) AddMedia(ctx context.Context, m training.Media) (training.Media, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.Media{}, err
	}
	defer tx.Rollback()
	var table, col string
	var limit int
	switch m.OwnerType {
	case training.OwnerSet:
		table, col, limit = "sets", "set_id", training.MaxMediaPerSet
	case training.OwnerExercise:
		table, col, limit = "exercises", "exercise_id", training.MaxMediaPerExercise
	default:
		return training.Media{}, training.ErrValidation
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id=?`, m.OwnerID).Scan(&exists); err != nil {
		return training.Media{}, err
	}
	if exists == 0 {
		return training.Media{}, fmt.Errorf("%w: %s %d does not exist", training.ErrNotFound, m.OwnerType, m.OwnerID)
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media WHERE `+col+`=?`, m.OwnerID).Scan(&n); err != nil {
		return training.Media{}, err
	}
	if n >= limit {
		return training.Media{}, fmt.Errorf("%w: a %s can have at most %d videos; delete one first", training.ErrConflict, m.OwnerType, limit)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO media(`+col+`,kind,storage_key,filename,content_type,size_bytes,sha256,caption,created_at) VALUES (?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		m.OwnerID, "video", m.StorageKey, m.Filename, m.ContentType, m.SizeBytes, m.SHA256, m.Caption)
	if err != nil {
		return training.Media{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return training.Media{}, err
	}
	out, err := scanMedia(tx.QueryRowContext(ctx, `SELECT `+mediaCols+` FROM media WHERE id=?`, id))
	if err != nil {
		return training.Media{}, err
	}
	return out, tx.Commit()
}

func (s *Store) GetMedia(ctx context.Context, id int64) (training.Media, error) {
	m, err := scanMedia(s.db.QueryRowContext(ctx, `SELECT `+mediaCols+` FROM media WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, fmt.Errorf("%w: media %d does not exist", training.ErrNotFound, id)
	}
	return m, err
}

func (s *Store) ListMedia(ctx context.Context, ownerType string, ownerID int64) ([]training.Media, error) {
	col := "exercise_id"
	if ownerType == training.OwnerSet {
		col = "set_id"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+mediaCols+` FROM media WHERE `+col+`=? ORDER BY id`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []training.Media
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMedia(ctx context.Context, id int64) (training.Media, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return training.Media{}, err
	}
	defer tx.Rollback()
	m, err := scanMedia(tx.QueryRowContext(ctx, `SELECT `+mediaCols+` FROM media WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, fmt.Errorf("%w: media %d does not exist", training.ErrNotFound, id)
	}
	if err != nil {
		return m, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM media WHERE id=?`, id); err != nil {
		return m, err
	}
	return m, tx.Commit()
}

func (s *Store) keys(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err = rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) KeysForSet(ctx context.Context, setID int64) ([]string, error) {
	return s.keys(ctx, `SELECT storage_key FROM media WHERE set_id=?`, setID)
}

func (s *Store) KeysForWorkout(ctx context.Context, sessionID int64) ([]string, error) {
	return s.keys(ctx, `SELECT m.storage_key FROM media m JOIN sets st ON st.id=m.set_id WHERE st.session_id=?`, sessionID)
}

func (s *Store) AllKeys(ctx context.Context) (map[string]bool, error) {
	list, err := s.keys(ctx, `SELECT storage_key FROM media`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(list))
	for _, k := range list {
		out[k] = true
	}
	return out, nil
}

func (s *Store) ExerciseInstructions(ctx context.Context, exerciseID int64) (string, error) {
	var text string
	err := s.db.QueryRowContext(ctx, `SELECT instructions FROM exercises WHERE id=?`, exerciseID).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: exercise %d does not exist", training.ErrNotFound, exerciseID)
	}
	return text, err
}

func (s *Store) SetExerciseInstructions(ctx context.Context, exerciseID int64, text string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE exercises SET instructions=? WHERE id=?`, text, exerciseID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: exercise %d does not exist", training.ErrNotFound, exerciseID)
	}
	return nil
}
