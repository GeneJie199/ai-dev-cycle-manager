package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	devagent "github.com/GeneJie199/ai-dev-cycle-manager/internal/agent"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

func (a *App) AgentAdapters(ctx context.Context) ([]models.AgentAdapterStatus, error) {
	configured, err := a.Store.ListAgentAdapters(ctx)
	if err != nil {
		return nil, err
	}
	all := append(devagent.BuiltInAdapters(), configured...)
	statuses := make([]models.AgentAdapterStatus, 0, len(all))
	for _, adapter := range all {
		status := models.AgentAdapterStatus{AgentAdapterConfig: adapter}
		if !adapter.Enabled {
			status.Reason = "adapter is disabled"
			statuses = append(statuses, status)
			continue
		}
		if !adapter.BuiltIn {
			adapter, err = devagent.NormalizeAdapterConfig(adapter)
			if err != nil {
				status.Reason = err.Error()
				statuses = append(statuses, status)
				continue
			}
			status.AgentAdapterConfig = adapter
		}
		if _, lookErr := exec.LookPath(adapter.Command); lookErr != nil {
			status.Reason = fmt.Sprintf("%s executable not found", adapter.Command)
		} else {
			status.Available = true
		}
		statuses = append(statuses, status)
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		if statuses[i].BuiltIn != statuses[j].BuiltIn {
			return statuses[i].BuiltIn
		}
		return strings.ToLower(statuses[i].Name) < strings.ToLower(statuses[j].Name)
	})
	return statuses, nil
}

func (a *App) GetAgentAdapter(ctx context.Context, id string) (models.AgentAdapterConfig, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if adapter, ok := devagent.BuiltIn(id); ok {
		return adapter, nil
	}
	adapter, err := a.Store.GetAgentAdapter(ctx, id)
	if err != nil {
		return adapter, err
	}
	return devagent.NormalizeAdapterConfig(adapter)
}

func (a *App) ConfigureAgentAdapter(ctx context.Context, adapter models.AgentAdapterConfig) (models.AgentAdapterStatus, error) {
	adapter, err := devagent.NormalizeAdapterConfig(adapter)
	if err != nil {
		return models.AgentAdapterStatus{}, validationf("%s", err.Error())
	}
	adapter, err = a.Store.SaveAgentAdapter(ctx, adapter)
	if err != nil {
		return models.AgentAdapterStatus{}, err
	}
	statuses, err := a.AgentAdapters(ctx)
	if err != nil {
		return models.AgentAdapterStatus{}, err
	}
	for _, status := range statuses {
		if status.ID == adapter.ID {
			return status, nil
		}
	}
	return models.AgentAdapterStatus{}, sql.ErrNoRows
}

func (a *App) DeleteAgentAdapter(ctx context.Context, id string) error {
	id = strings.ToLower(strings.TrimSpace(id))
	if devagent.IsBuiltIn(id) {
		return validationf("built-in adapters cannot be deleted")
	}
	sessions, err := a.Store.ListAgentSessions(ctx, "")
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if session.Provider == id && session.Status == "running" {
			return fmt.Errorf("stop running agent session %s before deleting the adapter", session.ID)
		}
	}
	if err = a.Store.DeleteAgentAdapter(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return fmt.Errorf("delete agent adapter: %w", err)
	}
	return nil
}
