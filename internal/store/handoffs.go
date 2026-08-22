package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/google/uuid"
)

func (s *Store) CreateTaskHandoff(ctx context.Context, handoff models.TaskHandoff) (models.TaskHandoff, error) {
	if handoff.ID == "" {
		handoff.ID = uuid.NewString()
	}
	if handoff.CreatedAt.IsZero() {
		handoff.CreatedAt = nowUTC()
	}
	if handoff.Status == "" {
		handoff.Status = models.HandoffStatusOpen
	}
	completed, _ := json.Marshal(handoff.CompletedWork)
	remaining, _ := json.Marshal(handoff.RemainingWork)
	risks, _ := json.Marshal(handoff.Risks)
	validation, _ := json.Marshal(handoff.Validation)
	files, _ := json.Marshal(handoff.ChangedFiles)
	var fromSession any
	if handoff.FromSessionID != "" {
		fromSession = handoff.FromSessionID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO task_handoffs(id,requirement_id,task_id,from_session_id,from_adapter,to_adapter,summary,completed_work_json,remaining_work_json,risks_json,validation_json,changed_files_json,status,created_at,accepted_at,accepted_session_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,NULL,'')`, handoff.ID, handoff.RequirementID, handoff.TaskID, fromSession, handoff.FromAdapter, handoff.ToAdapter, handoff.Summary, string(completed), string(remaining), string(risks), string(validation), string(files), handoff.Status, formatTime(handoff.CreatedAt))
	if err != nil {
		return handoff, err
	}
	return s.GetTaskHandoff(ctx, handoff.ID)
}

func (s *Store) GetTaskHandoff(ctx context.Context, id string) (models.TaskHandoff, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,requirement_id,task_id,from_session_id,from_adapter,to_adapter,summary,completed_work_json,remaining_work_json,risks_json,validation_json,changed_files_json,status,created_at,accepted_at,accepted_session_id FROM task_handoffs WHERE id=?`, id)
	return scanHandoff(row)
}

func (s *Store) ListTaskHandoffs(ctx context.Context, taskID string) ([]models.TaskHandoff, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,requirement_id,task_id,from_session_id,from_adapter,to_adapter,summary,completed_work_json,remaining_work_json,risks_json,validation_json,changed_files_json,status,created_at,accepted_at,accepted_session_id FROM task_handoffs WHERE task_id=? ORDER BY created_at DESC,rowid DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.TaskHandoff{}
	for rows.Next() {
		handoff, scanErr := scanHandoff(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, handoff)
	}
	return out, rows.Err()
}

func (s *Store) AcceptTaskHandoff(ctx context.Context, id, sessionID string) (models.TaskHandoff, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE task_handoffs SET status=?,accepted_at=?,accepted_session_id=? WHERE id=? AND status=?`, models.HandoffStatusAccepted, formatTime(nowUTC()), sessionID, id, models.HandoffStatusOpen)
	if err != nil {
		return models.TaskHandoff{}, err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return models.TaskHandoff{}, sql.ErrNoRows
	}
	return s.GetTaskHandoff(ctx, id)
}

type rowScanner interface {
	Scan(...any) error
}

func scanHandoff(row rowScanner) (models.TaskHandoff, error) {
	var handoff models.TaskHandoff
	var fromSession, acceptedAt sql.NullString
	var completed, remaining, risks, validation, files, created string
	err := row.Scan(&handoff.ID, &handoff.RequirementID, &handoff.TaskID, &fromSession, &handoff.FromAdapter, &handoff.ToAdapter, &handoff.Summary, &completed, &remaining, &risks, &validation, &files, &handoff.Status, &created, &acceptedAt, &handoff.AcceptedSession)
	if err != nil {
		return handoff, err
	}
	handoff.FromSessionID = fromSession.String
	handoff.CreatedAt = parseTime(created)
	if acceptedAt.Valid {
		handoff.AcceptedAt = parseTime(acceptedAt.String)
	}
	values := []struct {
		raw    string
		target *[]string
	}{{completed, &handoff.CompletedWork}, {remaining, &handoff.RemainingWork}, {risks, &handoff.Risks}, {validation, &handoff.Validation}, {files, &handoff.ChangedFiles}}
	for _, value := range values {
		if err = json.Unmarshal([]byte(value.raw), value.target); err != nil {
			return handoff, err
		}
	}
	return handoff, nil
}
