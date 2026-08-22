package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

func (s *Store) SaveAgentAdapter(ctx context.Context, adapter models.AgentAdapterConfig) (models.AgentAdapterConfig, error) {
	now := nowUTC()
	if adapter.CreatedAt.IsZero() {
		adapter.CreatedAt = now
	}
	adapter.UpdatedAt = now
	args, err := json.Marshal(adapter.Args)
	if err != nil {
		return adapter, err
	}
	capabilities, err := json.Marshal(adapter.Capabilities)
	if err != nil {
		return adapter, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_adapters(id,name,description,command,args_json,capabilities_json,enabled,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,command=excluded.command,args_json=excluded.args_json,capabilities_json=excluded.capabilities_json,enabled=excluded.enabled,updated_at=excluded.updated_at`,
		adapter.ID, adapter.Name, adapter.Description, adapter.Command, string(args), string(capabilities), boolToInt(adapter.Enabled), formatTime(adapter.CreatedAt), formatTime(adapter.UpdatedAt))
	if err != nil {
		return adapter, err
	}
	return s.GetAgentAdapter(ctx, adapter.ID)
}

func (s *Store) GetAgentAdapter(ctx context.Context, id string) (models.AgentAdapterConfig, error) {
	var adapter models.AgentAdapterConfig
	var args, capabilities, created, updated string
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT id,name,description,command,args_json,capabilities_json,enabled,created_at,updated_at FROM agent_adapters WHERE id=?`, id).
		Scan(&adapter.ID, &adapter.Name, &adapter.Description, &adapter.Command, &args, &capabilities, &enabled, &created, &updated)
	if err != nil {
		return adapter, err
	}
	if err = json.Unmarshal([]byte(args), &adapter.Args); err != nil {
		return adapter, err
	}
	if err = json.Unmarshal([]byte(capabilities), &adapter.Capabilities); err != nil {
		return adapter, err
	}
	adapter.Enabled = enabled != 0
	adapter.CreatedAt = parseTime(created)
	adapter.UpdatedAt = parseTime(updated)
	return adapter, nil
}

func (s *Store) ListAgentAdapters(ctx context.Context) ([]models.AgentAdapterConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,command,args_json,capabilities_json,enabled,created_at,updated_at FROM agent_adapters ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.AgentAdapterConfig{}
	for rows.Next() {
		var adapter models.AgentAdapterConfig
		var args, capabilities, created, updated string
		var enabled int
		if err = rows.Scan(&adapter.ID, &adapter.Name, &adapter.Description, &adapter.Command, &args, &capabilities, &enabled, &created, &updated); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(args), &adapter.Args); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(capabilities), &adapter.Capabilities); err != nil {
			return nil, err
		}
		adapter.Enabled = enabled != 0
		adapter.CreatedAt = parseTime(created)
		adapter.UpdatedAt = parseTime(updated)
		out = append(out, adapter)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAgentAdapter(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM agent_adapters WHERE id=?`, id)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
