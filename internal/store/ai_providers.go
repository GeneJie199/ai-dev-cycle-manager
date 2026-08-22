package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

func (s *Store) SaveAIProvider(ctx context.Context, provider models.AIProviderConfig) (models.AIProviderConfig, error) {
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT created_at FROM ai_providers WHERE id = ?`, provider.ID).Scan(&created)
	if err == nil {
		provider.CreatedAt = parseTime(created)
	} else if errors.Is(err, sql.ErrNoRows) {
		provider.CreatedAt = nowUTC()
	} else {
		return provider, err
	}
	provider.UpdatedAt = nowUTC()
	headers, err := json.Marshal(provider.Headers)
	if err != nil {
		return provider, fmt.Errorf("encode provider headers: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO ai_providers (id, name, kind, base_url, model, api_path, api_key_header, api_key_prefix, headers_json, enabled, timeout_seconds, secret_ref, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, kind=excluded.kind, base_url=excluded.base_url, model=excluded.model, api_path=excluded.api_path, api_key_header=excluded.api_key_header, api_key_prefix=excluded.api_key_prefix, headers_json=excluded.headers_json, enabled=excluded.enabled, timeout_seconds=excluded.timeout_seconds, secret_ref=excluded.secret_ref, updated_at=excluded.updated_at`,
		provider.ID, provider.Name, provider.Kind, provider.BaseURL, provider.Model, provider.APIPath, provider.APIKeyHeader, provider.APIKeyPrefix, string(headers), boolToInt(provider.Enabled), provider.TimeoutSeconds, provider.SecretRef, formatTime(provider.CreatedAt), formatTime(provider.UpdatedAt))
	return provider, err
}

func (s *Store) GetAIProvider(ctx context.Context, id string) (models.AIProviderConfig, error) {
	var provider models.AIProviderConfig
	var headers, created, updated string
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT id, name, kind, base_url, model, api_path, api_key_header, api_key_prefix, headers_json, enabled, timeout_seconds, secret_ref, created_at, updated_at FROM ai_providers WHERE id = ?`, id).
		Scan(&provider.ID, &provider.Name, &provider.Kind, &provider.BaseURL, &provider.Model, &provider.APIPath, &provider.APIKeyHeader, &provider.APIKeyPrefix, &headers, &enabled, &provider.TimeoutSeconds, &provider.SecretRef, &created, &updated)
	if err != nil {
		return provider, err
	}
	if err = json.Unmarshal([]byte(headers), &provider.Headers); err != nil {
		return provider, fmt.Errorf("decode provider headers: %w", err)
	}
	provider.Enabled = enabled != 0
	provider.CreatedAt = parseTime(created)
	provider.UpdatedAt = parseTime(updated)
	return provider, nil
}

func (s *Store) ListAIProviders(ctx context.Context) ([]models.AIProviderConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, kind, base_url, model, api_path, api_key_header, api_key_prefix, headers_json, enabled, timeout_seconds, secret_ref, created_at, updated_at FROM ai_providers ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var providers []models.AIProviderConfig
	for rows.Next() {
		var provider models.AIProviderConfig
		var headers, created, updated string
		var enabled int
		if err = rows.Scan(&provider.ID, &provider.Name, &provider.Kind, &provider.BaseURL, &provider.Model, &provider.APIPath, &provider.APIKeyHeader, &provider.APIKeyPrefix, &headers, &enabled, &provider.TimeoutSeconds, &provider.SecretRef, &created, &updated); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(headers), &provider.Headers); err != nil {
			return nil, fmt.Errorf("decode provider headers: %w", err)
		}
		provider.Enabled = enabled != 0
		provider.CreatedAt = parseTime(created)
		provider.UpdatedAt = parseTime(updated)
		providers = append(providers, provider)
	}
	return providers, rows.Err()
}

func (s *Store) DeleteAIProvider(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM ai_providers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}
