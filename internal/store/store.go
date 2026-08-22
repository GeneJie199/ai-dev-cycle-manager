package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
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
	// Keep SQLite pragmas connection-scoped and serialize this local workbench's writes.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("secure database permissions: %w", err)
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

const SchemaVersion = 3

var migrations = []string{`
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

CREATE TABLE IF NOT EXISTS task_dependencies (
  task_id TEXT NOT NULL,
  depends_on_task_id TEXT NOT NULL,
  PRIMARY KEY(task_id, depends_on_task_id),
  FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(depends_on_task_id) REFERENCES tasks(id) ON DELETE CASCADE,
  CHECK(task_id <> depends_on_task_id)
);
CREATE INDEX IF NOT EXISTS idx_task_dependencies_parent ON task_dependencies(depends_on_task_id);

CREATE TABLE IF NOT EXISTS evidence (
  id TEXT PRIMARY KEY,
  requirement_id TEXT NOT NULL,
  criterion_id TEXT NOT NULL DEFAULT '',
  task_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  uri TEXT NOT NULL DEFAULT '',
  inline_content TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  FOREIGN KEY(requirement_id) REFERENCES requirements(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_evidence_requirement ON evidence(requirement_id);
CREATE INDEX IF NOT EXISTS idx_evidence_criterion ON evidence(criterion_id);

CREATE TABLE IF NOT EXISTS verification_runs (
  id TEXT PRIMARY KEY,
  requirement_id TEXT NOT NULL,
  criterion_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  command TEXT NOT NULL,
  working_dir TEXT NOT NULL,
  status TEXT NOT NULL,
  exit_code INTEGER NOT NULL,
  output TEXT NOT NULL,
  started_at TEXT NOT NULL,
  completed_at TEXT NOT NULL,
  evidence_id TEXT NOT NULL,
  FOREIGN KEY(requirement_id) REFERENCES requirements(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_runs_requirement ON verification_runs(requirement_id);

CREATE TABLE IF NOT EXISTS agent_sessions (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  prompt TEXT NOT NULL,
  working_dir TEXT NOT NULL,
  status TEXT NOT NULL,
  pid INTEGER NOT NULL DEFAULT 0,
  log_path TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL,
  ended_at TEXT,
  FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_sessions_task ON agent_sessions(task_id);
`, `
CREATE TABLE planning_documents (
  requirement_id TEXT PRIMARY KEY,
  schema_version TEXT NOT NULL,
  revision INTEGER NOT NULL,
  status TEXT NOT NULL,
  source TEXT NOT NULL,
  provider TEXT NOT NULL DEFAULT '',
  document_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  applied_at TEXT,
  FOREIGN KEY(requirement_id) REFERENCES requirements(id) ON DELETE CASCADE
);

CREATE TABLE ai_providers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  base_url TEXT NOT NULL,
  model TEXT NOT NULL,
  api_path TEXT NOT NULL DEFAULT '',
  api_key_header TEXT NOT NULL DEFAULT '',
  api_key_prefix TEXT NOT NULL DEFAULT '',
  headers_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  timeout_seconds INTEGER NOT NULL DEFAULT 120,
  secret_ref TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`, `
CREATE TABLE agent_adapters (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  command TEXT NOT NULL,
  args_json TEXT NOT NULL,
  capabilities_json TEXT NOT NULL DEFAULT '[]',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE task_handoffs (
  id TEXT PRIMARY KEY,
  requirement_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  from_session_id TEXT,
  from_adapter TEXT NOT NULL DEFAULT '',
  to_adapter TEXT NOT NULL,
  summary TEXT NOT NULL,
  completed_work_json TEXT NOT NULL DEFAULT '[]',
  remaining_work_json TEXT NOT NULL DEFAULT '[]',
  risks_json TEXT NOT NULL DEFAULT '[]',
  validation_json TEXT NOT NULL DEFAULT '[]',
  changed_files_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'open',
  created_at TEXT NOT NULL,
  accepted_at TEXT,
  accepted_session_id TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(requirement_id) REFERENCES requirements(id) ON DELETE CASCADE,
  FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE,
  FOREIGN KEY(from_session_id) REFERENCES agent_sessions(id) ON DELETE SET NULL
);
CREATE INDEX idx_task_handoffs_task ON task_handoffs(task_id, created_at DESC);
`}

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > SchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, SchemaVersion)
	}
	for version < SchemaVersion {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(migrations[version]); err == nil {
			_, err = tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version+1))
		}
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		if err != nil {
			return fmt.Errorf("apply database migration %d: %w", version+1, err)
		}
		version++
	}
	return nil
}

// InsertPlan atomically inserts an AI plan selected by the user. Either every
// selected criterion and task is persisted, or none are.
func (s *Store) InsertPlan(ctx context.Context, criteria []models.AcceptanceCriterion, tasks []models.Task) error {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for _, criterion := range criteria {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO acceptance_criteria (id, requirement_id, description, satisfied, created_at) VALUES (?, ?, ?, ?, ?)`, criterion.ID, criterion.RequirementID, criterion.Description, boolToInt(criterion.Satisfied), formatTime(criterion.CreatedAt)); err != nil {
			return err
		}
	}
	for _, task := range tasks {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO tasks (id, requirement_id, title, description, status, branch, worktree_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, task.ID, task.RequirementID, task.Title, task.Description, task.Status, task.Branch, task.WorktreePath, formatTime(task.CreatedAt), formatTime(task.UpdatedAt)); err != nil {
			return err
		}
		for _, dependencyID := range task.DependsOn {
			if _, err := transaction.ExecContext(ctx, `INSERT INTO task_dependencies (task_id, depends_on_task_id) VALUES (?, ?)`, task.ID, dependencyID); err != nil {
				return err
			}
		}
	}
	return transaction.Commit()
}

func nowUTC() time.Time { return time.Now().UTC() }

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

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

func (s *Store) DeleteRepository(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM repositories WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM acceptance_criteria WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `UPDATE evidence SET criterion_id = '' WHERE criterion_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE verification_runs SET criterion_id = '' WHERE criterion_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
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
	var status, created, updated, dependencies string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, requirement_id, title, description, status, branch, worktree_path, created_at, updated_at,
		        COALESCE((SELECT group_concat(depends_on_task_id, char(31)) FROM task_dependencies WHERE task_id = tasks.id ORDER BY depends_on_task_id), '')
		 FROM tasks WHERE id = ?`, id,
	).Scan(&t.ID, &t.RequirementID, &t.Title, &t.Description, &status, &t.Branch, &t.WorktreePath, &created, &updated, &dependencies)
	if err != nil {
		return t, err
	}
	t.Status = models.TaskStatus(status)
	t.CreatedAt = parseTime(created)
	t.UpdatedAt = parseTime(updated)
	t.DependsOn = splitDependencies(dependencies)
	return t, nil
}

func (s *Store) UpdateTask(ctx context.Context, task models.Task) (models.Task, error) {
	return s.UpdateTaskWithDependencies(ctx, task)
}

// UpdateTaskWithDependencies atomically updates a task and replaces its dependency set.
func (s *Store) UpdateTaskWithDependencies(ctx context.Context, task models.Task) (models.Task, error) {
	task.UpdatedAt = nowUTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.Task{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
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
	if _, err = tx.ExecContext(ctx, `DELETE FROM task_dependencies WHERE task_id = ?`, task.ID); err != nil {
		return models.Task{}, err
	}
	for _, dependencyID := range task.DependsOn {
		if _, err = tx.ExecContext(ctx, `INSERT INTO task_dependencies(task_id, depends_on_task_id) VALUES(?, ?)`, task.ID, dependencyID); err != nil {
			return models.Task{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return models.Task{}, err
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `UPDATE evidence SET task_id = '' WHERE task_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListTasksByRequirement(ctx context.Context, requirementID string) ([]models.Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, requirement_id, title, description, status, branch, worktree_path, created_at, updated_at,
		        COALESCE((SELECT group_concat(depends_on_task_id, char(31)) FROM task_dependencies WHERE task_id = tasks.id ORDER BY depends_on_task_id), '')
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
		`SELECT id, requirement_id, title, description, status, branch, worktree_path, created_at, updated_at,
		        COALESCE((SELECT group_concat(depends_on_task_id, char(31)) FROM task_dependencies WHERE task_id = tasks.id ORDER BY depends_on_task_id), '')
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
		var status, created, updated, dependencies string
		if err := rows.Scan(&t.ID, &t.RequirementID, &t.Title, &t.Description, &status, &t.Branch, &t.WorktreePath, &created, &updated, &dependencies); err != nil {
			return nil, err
		}
		t.Status = models.TaskStatus(status)
		t.CreatedAt = parseTime(created)
		t.UpdatedAt = parseTime(updated)
		t.DependsOn = splitDependencies(dependencies)
		out = append(out, t)
	}
	return out, rows.Err()
}

func splitDependencies(value string) []string {
	if value == "" {
		return []string{}
	}
	return strings.Split(value, string(rune(31)))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
