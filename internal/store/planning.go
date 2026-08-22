package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

var ErrPlanningRevisionConflict = errors.New("planning document revision conflict")

type planningExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) GetPlanningDocument(ctx context.Context, requirementID string) (models.PlanningDocument, error) {
	var raw, created, updated string
	var applied sql.NullString
	var document models.PlanningDocument
	err := s.db.QueryRowContext(ctx, `SELECT document_json, created_at, updated_at, applied_at FROM planning_documents WHERE requirement_id = ?`, requirementID).Scan(&raw, &created, &updated, &applied)
	if err != nil {
		return document, err
	}
	if err = json.Unmarshal([]byte(raw), &document); err != nil {
		return document, fmt.Errorf("decode planning document: %w", err)
	}
	document.CreatedAt = parseTime(created)
	document.UpdatedAt = parseTime(updated)
	if applied.Valid {
		document.AppliedAt = parseTime(applied.String)
	}
	return document, nil
}

func (s *Store) SavePlanningDocument(ctx context.Context, document models.PlanningDocument) (models.PlanningDocument, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return document, err
	}
	defer tx.Rollback()
	saved, err := savePlanningDocument(ctx, tx, document)
	if err != nil {
		return document, err
	}
	if err = tx.Commit(); err != nil {
		return document, err
	}
	return saved, nil
}

// ApplyPlanningDocument atomically persists the reviewed plan and creates all
// selected criteria/tasks. A revision conflict or item failure leaves no partial plan.
func (s *Store) ApplyPlanningDocument(ctx context.Context, document models.PlanningDocument, criteria []models.AcceptanceCriterion, tasks []models.Task) (models.PlanningDocument, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return document, err
	}
	defer tx.Rollback()
	for _, criterion := range criteria {
		if _, err = tx.ExecContext(ctx, `INSERT INTO acceptance_criteria (id, requirement_id, description, satisfied, created_at) VALUES (?, ?, ?, ?, ?)`, criterion.ID, criterion.RequirementID, criterion.Description, boolToInt(criterion.Satisfied), formatTime(criterion.CreatedAt)); err != nil {
			return document, err
		}
	}
	for _, task := range tasks {
		if _, err = tx.ExecContext(ctx, `INSERT INTO tasks (id, requirement_id, title, description, status, branch, worktree_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, task.ID, task.RequirementID, task.Title, task.Description, task.Status, task.Branch, task.WorktreePath, formatTime(task.CreatedAt), formatTime(task.UpdatedAt)); err != nil {
			return document, err
		}
		for _, dependencyID := range task.DependsOn {
			if _, err = tx.ExecContext(ctx, `INSERT INTO task_dependencies (task_id, depends_on_task_id) VALUES (?, ?)`, task.ID, dependencyID); err != nil {
				return document, err
			}
		}
	}
	document.Status = "applied"
	document.AppliedAt = time.Now().UTC().Truncate(time.Second)
	saved, err := savePlanningDocument(ctx, tx, document)
	if err != nil {
		return document, err
	}
	if err = tx.Commit(); err != nil {
		return document, err
	}
	return saved, nil
}

func savePlanningDocument(ctx context.Context, target planningExecutor, document models.PlanningDocument) (models.PlanningDocument, error) {
	var currentRevision int
	var created string
	err := target.QueryRowContext(ctx, `SELECT revision, created_at FROM planning_documents WHERE requirement_id = ?`, document.RequirementID).Scan(&currentRevision, &created)
	now := time.Now().UTC().Truncate(time.Second)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if document.Revision != 0 {
			return document, ErrPlanningRevisionConflict
		}
		document.Revision = 1
		document.CreatedAt = now
	case err != nil:
		return document, err
	default:
		if document.Revision != currentRevision {
			return document, ErrPlanningRevisionConflict
		}
		document.Revision++
		document.CreatedAt = parseTime(created)
	}
	document.UpdatedAt = now
	raw, err := json.Marshal(document)
	if err != nil {
		return document, err
	}
	applied := any(nil)
	if !document.AppliedAt.IsZero() {
		applied = formatTime(document.AppliedAt)
	}
	_, err = target.ExecContext(ctx, `INSERT INTO planning_documents (requirement_id, schema_version, revision, status, source, provider, document_json, created_at, updated_at, applied_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(requirement_id) DO UPDATE SET schema_version=excluded.schema_version, revision=excluded.revision, status=excluded.status, source=excluded.source, provider=excluded.provider, document_json=excluded.document_json, updated_at=excluded.updated_at, applied_at=excluded.applied_at`,
		document.RequirementID, document.SchemaVersion, document.Revision, document.Status, document.Source, document.Provider, string(raw), formatTime(document.CreatedAt), formatTime(document.UpdatedAt), applied)
	return document, err
}
