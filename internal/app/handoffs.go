package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	devai "github.com/GeneJie199/ai-dev-cycle-manager/internal/ai"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

type TaskHandoffInput struct {
	FromSessionID string   `json:"fromSessionId,omitempty"`
	ToAdapter     string   `json:"toAdapter"`
	Summary       string   `json:"summary"`
	CompletedWork []string `json:"completedWork"`
	RemainingWork []string `json:"remainingWork"`
	Risks         []string `json:"risks"`
	Validation    []string `json:"validation"`
}

func (a *App) CreateTaskHandoff(ctx context.Context, taskID string, input TaskHandoffInput) (models.TaskHandoff, error) {
	task, err := a.Store.GetTask(ctx, taskID)
	if err != nil {
		return models.TaskHandoff{}, err
	}
	adapter, err := a.GetAgentAdapter(ctx, input.ToAdapter)
	if err != nil {
		return models.TaskHandoff{}, fmt.Errorf("target adapter: %w", err)
	}
	if !adapter.Enabled {
		return models.TaskHandoff{}, validationf("target adapter is disabled")
	}
	fromAdapter := ""
	if input.FromSessionID != "" {
		session, sessionErr := a.Store.GetAgentSession(ctx, input.FromSessionID)
		if sessionErr != nil {
			return models.TaskHandoff{}, fmt.Errorf("source session: %w", sessionErr)
		}
		if session.TaskID != task.ID {
			return models.TaskHandoff{}, validationf("source session does not belong to the task")
		}
		if session.Status == "running" {
			return models.TaskHandoff{}, validationf("stop or finish the source session before creating a handoff")
		}
		fromAdapter = session.Provider
	}
	summary, _ := devai.Redact(strings.TrimSpace(input.Summary))
	if utf8.RuneCountInString(summary) < 5 || utf8.RuneCountInString(summary) > 4000 {
		return models.TaskHandoff{}, validationf("handoff summary must contain 5 to 4000 characters")
	}
	completed, err := normalizeHandoffItems(input.CompletedWork, "completedWork")
	if err != nil {
		return models.TaskHandoff{}, err
	}
	remaining, err := normalizeHandoffItems(input.RemainingWork, "remainingWork")
	if err != nil {
		return models.TaskHandoff{}, err
	}
	risks, err := normalizeHandoffItems(input.Risks, "risks")
	if err != nil {
		return models.TaskHandoff{}, err
	}
	validation, err := normalizeHandoffItems(input.Validation, "validation")
	if err != nil {
		return models.TaskHandoff{}, err
	}
	if len(completed)+len(remaining)+len(risks)+len(validation) == 0 {
		return models.TaskHandoff{}, validationf("handoff must include completed work, remaining work, risks, or validation")
	}
	changedFiles := []string{}
	if task.WorktreePath != "" {
		if impact, impactErr := a.AnalyzeChanges(ctx, task.WorktreePath); impactErr == nil {
			changedFiles = impact.Files
		}
	}
	handoff := models.TaskHandoff{RequirementID: task.RequirementID, TaskID: task.ID, FromSessionID: input.FromSessionID, FromAdapter: fromAdapter, ToAdapter: adapter.ID, Summary: summary, CompletedWork: completed, RemainingWork: remaining, Risks: risks, Validation: validation, ChangedFiles: changedFiles, Status: models.HandoffStatusOpen}
	return a.Store.CreateTaskHandoff(ctx, handoff)
}

func normalizeHandoffItems(items []string, field string) ([]string, error) {
	if len(items) > 30 {
		return nil, validationf("%s must not contain more than 30 items", field)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item, _ = devai.Redact(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if utf8.RuneCountInString(item) > 2000 {
			return nil, validationf("%s items must not exceed 2000 characters", field)
		}
		out = append(out, item)
	}
	return out, nil
}

func (a *App) ListTaskHandoffs(ctx context.Context, taskID string) ([]models.TaskHandoff, error) {
	if _, err := a.Store.GetTask(ctx, taskID); err != nil {
		return nil, err
	}
	return a.Store.ListTaskHandoffs(ctx, taskID)
}

func (a *App) AcceptTaskHandoff(ctx context.Context, id, sessionID string) (models.TaskHandoff, error) {
	handoff, err := a.Store.GetTaskHandoff(ctx, id)
	if err != nil {
		return handoff, err
	}
	if handoff.Status != models.HandoffStatusOpen {
		return handoff, errors.New("handoff is already accepted")
	}
	if sessionID != "" {
		session, sessionErr := a.Store.GetAgentSession(ctx, sessionID)
		if sessionErr != nil {
			return handoff, fmt.Errorf("accepting session: %w", sessionErr)
		}
		if session.TaskID != handoff.TaskID || session.Provider != handoff.ToAdapter {
			return handoff, validationf("accepting session must belong to the task and use the target adapter")
		}
	}
	return a.Store.AcceptTaskHandoff(ctx, id, sessionID)
}
