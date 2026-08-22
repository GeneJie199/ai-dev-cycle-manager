package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	devai "github.com/GeneJie199/ai-dev-cycle-manager/internal/ai"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

type AIProviderInput struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Kind              string            `json:"kind"`
	BaseURL           string            `json:"baseUrl"`
	Model             string            `json:"model"`
	APIPath           string            `json:"apiPath,omitempty"`
	APIKeyHeader      string            `json:"apiKeyHeader,omitempty"`
	APIKeyPrefix      string            `json:"apiKeyPrefix,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	Enabled           *bool             `json:"enabled,omitempty"`
	TimeoutSeconds    int               `json:"timeoutSeconds,omitempty"`
	Secret            string            `json:"secret,omitempty"`
	SecretEnvironment string            `json:"secretEnvironment,omitempty"`
	ClearSecret       bool              `json:"clearSecret,omitempty"`
}

func (a *App) AIProviders(ctx context.Context) ([]devai.ProviderStatus, error) {
	return a.AI.AllProviders(ctx)
}

func (a *App) ConfigureAIProvider(ctx context.Context, input AIProviderInput) (devai.ProviderStatus, error) {
	if a.AI == nil || a.AI.Secrets == nil {
		return devai.ProviderStatus{}, errors.New("AI provider service is unavailable")
	}
	if input.Secret != "" && input.SecretEnvironment != "" {
		return devai.ProviderStatus{}, validationf("choose either a keyring credential or an environment variable")
	}
	if input.ClearSecret && (input.Secret != "" || input.SecretEnvironment != "") {
		return devai.ProviderStatus{}, validationf("clearSecret cannot be combined with a new credential")
	}
	if utf8.RuneCountInString(input.Secret) > 16384 {
		return devai.ProviderStatus{}, validationf("credential value is too long")
	}
	provider := models.AIProviderConfig{ID: input.ID, Name: input.Name, Kind: input.Kind, BaseURL: input.BaseURL, Model: input.Model, APIPath: input.APIPath, APIKeyHeader: input.APIKeyHeader, APIKeyPrefix: input.APIKeyPrefix, Headers: input.Headers, Enabled: true, TimeoutSeconds: input.TimeoutSeconds}
	if input.Enabled != nil {
		provider.Enabled = *input.Enabled
	}
	existing, err := a.Store.GetAIProvider(ctx, strings.ToLower(strings.TrimSpace(input.ID)))
	if err == nil {
		provider.SecretRef = existing.SecretRef
	} else if !errors.Is(err, sql.ErrNoRows) {
		return devai.ProviderStatus{}, err
	}
	oldSecretRef := provider.SecretRef
	oldSecretValue := ""
	oldSecretReadable := false
	if input.Secret != "" && strings.HasPrefix(oldSecretRef, "keyring:") {
		oldSecretValue, err = a.AI.Secrets.Get(ctx, oldSecretRef)
		oldSecretReadable = err == nil
	}
	if input.ClearSecret {
		provider.SecretRef = ""
	} else if input.SecretEnvironment != "" {
		provider.SecretRef, err = devai.EnvironmentSecretRef(input.SecretEnvironment)
		if err != nil {
			return devai.ProviderStatus{}, validationf("%s", err.Error())
		}
	} else if input.Secret != "" {
		provider.SecretRef = "keyring:" + strings.ToLower(strings.TrimSpace(input.ID))
	}
	provider, err = devai.NormalizeProviderConfig(provider)
	if err != nil {
		return devai.ProviderStatus{}, validationf("%s", err.Error())
	}
	if input.Secret != "" {
		provider.SecretRef, err = a.AI.Secrets.Set(ctx, provider.ID, input.Secret)
		if err != nil {
			return devai.ProviderStatus{}, err
		}
	}
	if _, err = a.Store.SaveAIProvider(ctx, provider); err != nil {
		if input.Secret != "" {
			if oldSecretRef == provider.SecretRef && oldSecretReadable {
				_, _ = a.AI.Secrets.Set(ctx, provider.ID, oldSecretValue)
			} else {
				_ = a.AI.Secrets.Delete(ctx, provider.SecretRef)
			}
		}
		return devai.ProviderStatus{}, err
	}
	if oldSecretRef != "" && oldSecretRef != provider.SecretRef && strings.HasPrefix(oldSecretRef, "keyring:") {
		if err = a.AI.Secrets.Delete(ctx, oldSecretRef); err != nil {
			return devai.ProviderStatus{}, fmt.Errorf("provider saved, but the replaced credential could not be removed: %w", err)
		}
	}
	return a.aiProviderStatus(ctx, provider.ID)
}

func (a *App) DeleteAIProvider(ctx context.Context, id string) error {
	provider, err := a.Store.GetAIProvider(ctx, id)
	if err != nil {
		return err
	}
	if err = a.Store.DeleteAIProvider(ctx, id); err != nil {
		return err
	}
	if strings.HasPrefix(provider.SecretRef, "keyring:") && a.AI.Secrets != nil {
		if err = a.AI.Secrets.Delete(ctx, provider.SecretRef); err != nil {
			return fmt.Errorf("provider deleted, but its credential could not be removed: %w", err)
		}
	}
	return nil
}

func (a *App) AIRequestPreview(ctx context.Context, provider, input string) (devai.RequestPreview, error) {
	if strings.TrimSpace(input) == "" {
		return devai.RequestPreview{}, validationf("preview input is required")
	}
	return a.AI.RequestPreview(ctx, provider, input)
}

func (a *App) TestAIProvider(ctx context.Context, provider string) (devai.GenerationMeta, error) {
	var response struct {
		OK bool `json:"ok"`
	}
	meta, err := a.AI.GenerateJSON(ctx, provider, "connection test", `Return exactly one JSON object with this structure: {"ok":true}. Do not include Markdown or any other text.`, &response)
	if err != nil {
		return meta, err
	}
	if !response.OK {
		return meta, fmt.Errorf("provider returned a response but did not satisfy the structured connection test")
	}
	return meta, nil
}

func (a *App) aiProviderStatus(ctx context.Context, id string) (devai.ProviderStatus, error) {
	providers, err := a.AI.AllProviders(ctx)
	if err != nil {
		return devai.ProviderStatus{}, err
	}
	for _, provider := range providers {
		if provider.ID == id {
			return provider, nil
		}
	}
	return devai.ProviderStatus{}, sql.ErrNoRows
}
