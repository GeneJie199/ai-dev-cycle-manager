package team

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"time"
)

var ErrConflict = errors.New("revision conflict; reload before retry")

type Store struct{ db *sql.DB }

// Open uses a separate database: v1 binaries retain their original schema and rollback path.
func Open(file string) (*Store, error) {
	db, err := sql.Open("sqlite", file)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	fail := func(e error) (*Store, error) { _ = db.Close(); return nil, e }
	if _, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		return fail(err)
	}
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fail(err)
	}
	if version > 1 {
		return fail(errors.New("team database version is newer than supported"))
	}
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS missions (id TEXT PRIMARY KEY, revision INTEGER NOT NULL, body TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS memory (project_id TEXT NOT NULL,id TEXT NOT NULL,revision INTEGER NOT NULL,body TEXT NOT NULL,PRIMARY KEY(project_id,id,revision)); PRAGMA user_version=1;`); err != nil {
		return fail(err)
	}
	if err = os.Chmod(file, 0o600); err != nil {
		return fail(err)
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Save(ctx context.Context, m *Mission) error {
	previous := m.Revision
	m.Revision++
	b, err := json.Marshal(m)
	if err != nil {
		m.Revision = previous
		return err
	}
	var result sql.Result
	if previous == 0 {
		result, err = s.db.ExecContext(ctx, `INSERT INTO missions(id,revision,body) VALUES(?,?,?)`, m.ID, m.Revision, string(b))
	} else {
		result, err = s.db.ExecContext(ctx, `UPDATE missions SET revision=?,body=? WHERE id=? AND revision=?`, m.Revision, string(b), m.ID, previous)
	}
	if err != nil {
		m.Revision = previous
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		m.Revision = previous
		return ErrConflict
	}
	return nil
}
func (s *Store) Get(ctx context.Context, id string) (Mission, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT body FROM missions WHERE id=?`, id).Scan(&raw)
	if err != nil {
		return Mission{}, err
	}
	var m Mission
	err = json.Unmarshal([]byte(raw), &m)
	return m, err
}
func (s *Store) List(ctx context.Context) ([]Mission, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM missions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mission{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var m Mission
		if err = json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) SaveMemory(ctx context.Context, v Memory) (Memory, error) {
	if v.ProjectID == "" || v.ID == "" || !Contains(MemoryKinds, v.Kind) || v.Source == "" || v.Author == "" || v.Content == "" || len(v.Content) > 16000 {
		return v, errors.New("invalid memory: project, id, kind, content, provenance and author required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	var current int
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0) FROM memory WHERE project_id=? AND id=?`, v.ProjectID, v.ID).Scan(&current)
	if err != nil {
		return v, err
	}
	if current != v.Revision {
		return v, ErrConflict
	}
	v.Revision++
	v.Updated = time.Now().UTC()
	raw, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO memory VALUES(?,?,?,?)`, v.ProjectID, v.ID, v.Revision, string(raw))
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}
func (s *Store) Memory(ctx context.Context, project string, history bool) ([]Memory, error) {
	q := `SELECT body FROM memory WHERE project_id=?`
	if !history {
		q += ` AND revision=(SELECT MAX(m.revision) FROM memory m WHERE m.project_id=memory.project_id AND m.id=memory.id)`
	}
	q += ` ORDER BY id,revision`
	rows, err := s.db.QueryContext(ctx, q, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Memory{}
	for rows.Next() {
		var raw string
		var v Memory
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("decode memory: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
