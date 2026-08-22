package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/google/uuid"
)

func (s *Store) CreateEvidence(ctx context.Context, e models.Evidence) (models.Evidence, error) {
	e.ID = uuid.NewString()
	e.CreatedAt = nowUTC()
	if e.Status == "" {
		e.Status = "passed"
	}
	meta, _ := json.Marshal(e.Metadata)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return e, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO evidence(id,requirement_id,criterion_id,task_id,kind,title,status,uri,inline_content,metadata_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, e.ID, e.RequirementID, e.CriterionID, e.TaskID, e.Kind, e.Title, e.Status, e.URI, e.Inline, string(meta), formatTime(e.CreatedAt)); err != nil {
		return e, err
	}
	if e.CriterionID != "" && e.Status != "passed" {
		if _, err = tx.ExecContext(ctx, `UPDATE acceptance_criteria SET satisfied=0 WHERE id=?`, e.CriterionID); err != nil {
			return e, err
		}
	}
	return e, tx.Commit()
}
func (s *Store) ListEvidence(ctx context.Context, requirementID string) ([]models.Evidence, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,requirement_id,criterion_id,task_id,kind,title,status,uri,inline_content,metadata_json,created_at FROM evidence WHERE requirement_id=? ORDER BY created_at DESC, rowid DESC`, requirementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Evidence{}
	for rows.Next() {
		var e models.Evidence
		var meta, created string
		if err = rows.Scan(&e.ID, &e.RequirementID, &e.CriterionID, &e.TaskID, &e.Kind, &e.Title, &e.Status, &e.URI, &e.Inline, &meta, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(meta), &e.Metadata)
		e.CreatedAt = parseTime(created)
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) CountPassingEvidenceForCriterion(ctx context.Context, criterionID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM evidence WHERE criterion_id=? AND status='passed'`, criterionID).Scan(&n)
	return n, err
}

// LatestEvidenceForCriterion returns the newest durable result for one criterion.
// The rowid tie-breaker preserves insertion order for legacy second-resolution rows.
func (s *Store) LatestEvidenceForCriterion(ctx context.Context, criterionID string) (models.Evidence, error) {
	var e models.Evidence
	var meta, created string
	err := s.db.QueryRowContext(ctx, `SELECT id,requirement_id,criterion_id,task_id,kind,title,status,uri,inline_content,metadata_json,created_at FROM evidence WHERE criterion_id=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, criterionID).Scan(&e.ID, &e.RequirementID, &e.CriterionID, &e.TaskID, &e.Kind, &e.Title, &e.Status, &e.URI, &e.Inline, &meta, &created)
	if err != nil {
		return e, err
	}
	_ = json.Unmarshal([]byte(meta), &e.Metadata)
	e.CreatedAt = parseTime(created)
	return e, nil
}
func (s *Store) CreateVerificationRun(ctx context.Context, r models.VerificationRun) (models.VerificationRun, error) {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO verification_runs(id,requirement_id,criterion_id,name,command,working_dir,status,exit_code,output,started_at,completed_at,evidence_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, r.ID, r.RequirementID, r.CriterionID, r.Name, r.Command, r.WorkingDir, r.Status, r.ExitCode, r.Output, formatTime(r.StartedAt), formatTime(r.CompletedAt), r.EvidenceID)
	return r, err
}
func (s *Store) ListVerificationRuns(ctx context.Context, requirementID string) ([]models.VerificationRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,requirement_id,criterion_id,name,command,working_dir,status,exit_code,output,started_at,completed_at,evidence_id FROM verification_runs WHERE requirement_id=? ORDER BY started_at DESC`, requirementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.VerificationRun{}
	for rows.Next() {
		var r models.VerificationRun
		var started, completed string
		if err = rows.Scan(&r.ID, &r.RequirementID, &r.CriterionID, &r.Name, &r.Command, &r.WorkingDir, &r.Status, &r.ExitCode, &r.Output, &started, &completed, &r.EvidenceID); err != nil {
			return nil, err
		}
		r.StartedAt = parseTime(started)
		r.CompletedAt = parseTime(completed)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetVerificationRun(ctx context.Context, id string) (models.VerificationRun, error) {
	var r models.VerificationRun
	var started, completed string
	err := s.db.QueryRowContext(ctx, `SELECT id,requirement_id,criterion_id,name,command,working_dir,status,exit_code,output,started_at,completed_at,evidence_id FROM verification_runs WHERE id=?`, id).Scan(&r.ID, &r.RequirementID, &r.CriterionID, &r.Name, &r.Command, &r.WorkingDir, &r.Status, &r.ExitCode, &r.Output, &started, &completed, &r.EvidenceID)
	r.StartedAt = parseTime(started)
	r.CompletedAt = parseTime(completed)
	return r, err
}

func (s *Store) UpdateVerificationRun(ctx context.Context, r models.VerificationRun) (models.VerificationRun, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE verification_runs SET status=?,exit_code=?,output=?,completed_at=?,evidence_id=? WHERE id=?`, r.Status, r.ExitCode, r.Output, formatTime(r.CompletedAt), r.EvidenceID, r.ID)
	if err != nil {
		return r, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return r, sql.ErrNoRows
	}
	return s.GetVerificationRun(ctx, r.ID)
}

func (s *Store) RecoverRunningVerificationRuns(ctx context.Context) error {
	now := formatTime(nowUTC())
	_, err := s.db.ExecContext(ctx, `UPDATE verification_runs SET status='interrupted',exit_code=-1,output=CASE WHEN output='' THEN '[verification interrupted by service restart]' ELSE output END,completed_at=? WHERE status='running'`, now)
	return err
}
func (s *Store) CreateAgentSession(ctx context.Context, a models.AgentSession) (models.AgentSession, error) {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	if a.StartedAt.IsZero() {
		a.StartedAt = nowUTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_sessions(id,task_id,provider,prompt,working_dir,status,pid,log_path,started_at,ended_at) VALUES(?,?,?,?,?,?,?,?,?,NULL)`, a.ID, a.TaskID, a.Provider, a.Prompt, a.WorkingDir, a.Status, a.PID, a.LogPath, formatTime(a.StartedAt))
	return a, err
}
func (s *Store) UpdateAgentSession(ctx context.Context, id, status string, pid int, ended *time.Time) (models.AgentSession, error) {
	var end any
	if ended != nil {
		end = formatTime(*ended)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE agent_sessions SET status=?,pid=?,ended_at=? WHERE id=?`, status, pid, end, id)
	if err != nil {
		return models.AgentSession{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return models.AgentSession{}, sql.ErrNoRows
	}
	return s.GetAgentSession(ctx, id)
}

// RecoverRunningAgentSessions closes records left behind by an unclean process exit.
func (s *Store) RecoverRunningAgentSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_sessions SET status='interrupted',pid=0,ended_at=? WHERE status='running'`, formatTime(nowUTC()))
	return err
}
func (s *Store) GetAgentSession(ctx context.Context, id string) (models.AgentSession, error) {
	var a models.AgentSession
	var started string
	var ended sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,task_id,provider,prompt,working_dir,status,pid,log_path,started_at,ended_at FROM agent_sessions WHERE id=?`, id).Scan(&a.ID, &a.TaskID, &a.Provider, &a.Prompt, &a.WorkingDir, &a.Status, &a.PID, &a.LogPath, &started, &ended)
	a.StartedAt = parseTime(started)
	if ended.Valid {
		a.EndedAt = parseTime(ended.String)
	}
	return a, err
}
func (s *Store) ListAgentSessions(ctx context.Context, taskID string) ([]models.AgentSession, error) {
	query := `SELECT id,task_id,provider,prompt,working_dir,status,pid,log_path,started_at,ended_at FROM agent_sessions`
	args := []any{}
	if taskID != "" {
		query += " WHERE task_id=?"
		args = append(args, taskID)
	}
	query += " ORDER BY started_at DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.AgentSession{}
	for rows.Next() {
		var a models.AgentSession
		var started string
		var ended sql.NullString
		if err = rows.Scan(&a.ID, &a.TaskID, &a.Provider, &a.Prompt, &a.WorkingDir, &a.Status, &a.PID, &a.LogPath, &started, &ended); err != nil {
			return nil, err
		}
		a.StartedAt = parseTime(started)
		if ended.Valid {
			a.EndedAt = parseTime(ended.String)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
