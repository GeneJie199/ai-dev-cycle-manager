package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/google/uuid"

	_ "modernc.org/sqlite"
)

// Store is a SQLite-backed persistence layer for requirements, tasks, and criteria.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at path and runs migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Reasonable defaults for local single-user app
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS repositories (
  id TEXT PRIMARY KEY,
  path TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS requirements (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS acceptance_criteria (
  id TEXT PRIMARY KEY,
  requirement_id TEXT NOT NULL,
  description TEXT NOT NULL,
  satisfied INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  FOREIGN KEY(requirement_id) REFERENCES requirements(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  requirement_id TEXT NOT NULL,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'todo',
  branch TEXT NOT NULL DEFAULT '',
  worktree_path TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(requirement_id) REFERENCES requirements(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_criteria_requirement ON acceptance_criteria(requirement_id);
CREATE INDEX IF NOT EXISTS idx_tasks_requirement ON tasks(requirement_id);
`
	_, err := s.db.Exec(schema)
	return err
}

func nowUTC() time.Time { return time.Now().UTC().Truncate(time.Second) }

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func parseTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

// --- Repositories ---

func (s *Store) CreateRepository(ctx context.Context, path, name string) (models.Repository, error) {
	repo := models.Repository{
		ID:        uuid.NewString(),
		Path:      path,
		Name:      name,
		CreatedAt: nowUTC(),
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO repositories (id, path, name, created_at) VALUES (?, ?, ?, ?)`,
		repo.ID, repo.Path, repo.Name, formatTime(repo.CreatedAt),
	)
	return repo, err
}

func (s *Store) GetRepositoryByPath(ctx context.Context, path string) (models.Repository, error) {
	var r models.Repository
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, path, name, created_at FROM repositories WHERE path = ?`, path,
	).Scan(&r.ID, &r.Path, &r.Name, &created)
	if err != nil {
		return r, err
	}
	r.CreatedAt = parseTime(created)
	return r, nil
}

func (s *Store) ListRepositories(ctx context.Context) ([]models.Repository, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, path, name, created_at FROM repositories ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Repository
	for rows.Next() {
		var r models.Repository
		var created string
		if err := rows.Scan(&r.ID, &r.Path, &r.Name, &created); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- Requirements ---

func (s *Store) CreateRequirement(ctx context.Context, title, description string) (models.Requirement, error) {
	now := nowUTC()
	req := models.Requirement{
		ID:          uuid.NewString(),
		Title:       title,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO requirements (id, title, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		req.ID, req.Title, req.Description, formatTime(req.CreatedAt), formatTime(req.UpdatedAt),
	)
	return req, err
}

func (s *Store) GetRequirement(ctx context.Context, id string) (models.Requirement, error) {
	var r models.Requirement
	var created, updated string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, description, created_at, updated_at FROM requirements WHERE id = ?`, id,
	).Scan(&r.ID, &r.Title, &r.Description, &created, &updated)
	if err != nil {
		return r, err
	}
	r.CreatedAt = parseTime(created)
	r.UpdatedAt = parseTime(updated)
	return r, nil
}

func (s *Store) UpdateRequirement(ctx context.Context, id, title, description string) (models.Requirement, error) {
	now := nowUTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE requirements SET title = ?, description = ?, updated_at = ? WHERE id = ?`,
		title, description, formatTime(now), id,
	)
	if err != nil {
		return models.Requirement{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return models.Requirement{}, sql.ErrNoRows
	}
	return s.GetRequirement(ctx, id)
}

func (s *Store) DeleteRequirement(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM requirements WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListRequirements(ctx context.Context) ([]models.Requirement, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, description, created_at, updated_at FROM requirements ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Requirement
	for rows.Next() {
		var r models.Requirement
		var created, updated string
		if err := rows.Scan(&r.ID, &r.Title, &r.Description, &created, &updated); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		r.UpdatedAt = parseTime(updated)
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- Acceptance Criteria ---

func (s *Store) CreateCriterion(ctx context.Context, requirementID, description string) (models.AcceptanceCriterion, error) {
	c := models.AcceptanceCriterion{
		ID:            uuid.NewString(),
		RequirementID: requirementID,
		Description:   description,
		Satisfied:     false,
		CreatedAt:     nowUTC(),
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO acceptance_criteria (id, requirement_id, description, satisfied, created_at) VALUES (?, ?, ?, ?, ?)`,
		c.ID, c.RequirementID, c.Description, boolToInt(c.Satisfied), formatTime(c.CreatedAt),
	)
	return c, err
}

func (s *Store) UpdateCriterion(ctx context.Context, id, description string, satisfied bool) (models.AcceptanceCriterion, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE acceptance_criteria SET description = ?, satisfied = ? WHERE id = ?`,
		description, boolToInt(satisfied), id,
	)
	if err != nil {
		return models.AcceptanceCriterion{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return models.AcceptanceCriterion{}, sql.ErrNoRows
	}
	return s.GetCriterion(ctx, id)
}

func (s *Store) GetCriterion(ctx context.Context, id string) (models.AcceptanceCriterion, error) {
	var c models.AcceptanceCriterion
	var sat int
	var created string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, requirement_id, description, satisfied, created_at FROM acceptance_criteria WHERE id = ?`, id,
	).Scan(&c.ID, &c.RequirementID, &c.Description, &sat, &created)
	if err != nil {
		return c, err
	}
	c.Satisfied = sat != 0
	c.CreatedAt = parseTime(created)
	return c, nil
}

func (s *Store) DeleteCriterion(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM acceptance_criteria WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListCriteriaByRequirement(ctx context.Context, requirementID string) ([]models.AcceptanceCriterion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, requirement_id, description, satisfied, created_at FROM acceptance_criteria WHERE requirement_id = ? ORDER BY created_at`,
		requirementID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AcceptanceCriterion
	for rows.Next() {
		var c models.AcceptanceCriterion
		var sat int
		var created string
		if err := rows.Scan(&c.ID, &c.RequirementID, &c.Description, &sat, &created); err != nil {
			return nil, err
		}
		c.Satisfied = sat != 0
		c.CreatedAt = parseTime(created)
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- Tasks ---

func (s *Store) CreateTask(ctx context.Context, requirementID, title, description string) (models.Task, error) {
	now := nowUTC()
	task := models.Task{
		ID:            uuid.NewString(),
		RequirementID: requirementID,
		Title:         title,
		Description:   description,
		Status:        models.TaskStatusTodo,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (id, requirement_id, title, description, status, branch, worktree_path, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.RequirementID, task.Title, task.Description, string(task.Status),
		task.Branch, task.WorktreePath, formatTime(task.CreatedAt), formatTime(task.UpdatedAt),
	)
	return task, err
}

func (s *Store) GetTask(ctx context.Context, id string) (models.Task, error) {
	var t models.Task
	var status, created, updated string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, requirement_id, title, description, status, branch, worktree_path, created_at, updated_at
		 FROM tasks WHERE id = ?`, id,
	).Scan(&t.ID, &t.RequirementID, &t.Title, &t.Description, &status, &t.Branch, &t.WorktreePath, &created, &updated)
	if err != nil {
		return t, err
	}
	t.Status = models.TaskStatus(status)
	t.CreatedAt = parseTime(created)
	t.UpdatedAt = parseTime(updated)
	return t, nil
}

func (s *Store) UpdateTask(ctx context.Context, task models.Task) (models.Task, error) {
	task.UpdatedAt = nowUTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET title = ?, description = ?, status = ?, branch = ?, worktree_path = ?, updated_at = ?
		 WHERE id = ?`,
		task.Title, task.Description, string(task.Status), task.Branch, task.WorktreePath,
		formatTime(task.UpdatedAt), task.ID,
	)
	if err != nil {
		return models.Task{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return models.Task{}, sql.ErrNoRows
	}
	return s.GetTask(ctx, task.ID)
}

// LinkTaskGit associates a task with a branch (独立工作版本) and worktree path (隔离开发目录).
func (s *Store) LinkTaskGit(ctx context.Context, taskID, branch, worktreePath string) (models.Task, error) {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return task, err
	}
	task.Branch = branch
	task.WorktreePath = worktreePath
	if task.Status == models.TaskStatusTodo {
		task.Status = models.TaskStatusInProgress
	}
	return s.UpdateTask(ctx, task)
}

func (s *Store) DeleteTask(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListTasksByRequirement(ctx context.Context, requirementID string) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, requirement_id, title, description, status, branch, worktree_path, created_at, updated_at
		 FROM tasks WHERE requirement_id = ? ORDER BY created_at`, requirementID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (s *Store) ListTasks(ctx context.Context) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, requirement_id, title, description, status, branch, worktree_path, created_at, updated_at
		 FROM tasks ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

func scanTasks(rows *sql.Rows) ([]models.Task, error) {
	var out []models.Task
	for rows.Next() {
		var t models.Task
		var status, created, updated string
		if err := rows.Scan(&t.ID, &t.RequirementID, &t.Title, &t.Description, &status, &t.Branch, &t.WorktreePath, &created, &updated); err != nil {
			return nil, err
		}
		t.Status = models.TaskStatus(status)
		t.CreatedAt = parseTime(created)
		t.UpdatedAt = parseTime(updated)
		out = append(out, t)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
