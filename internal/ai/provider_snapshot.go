package ai

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

// ProviderSnapshot contains one configuration and its corresponding credential.
// Its fields are private so credentials cannot be serialized into API/audit data.
type ProviderSnapshot struct {
	config     models.AIProviderConfig
	credential string
}

func (ProviderSnapshot) String() string   { return "ProviderSnapshot{credential:redacted}" }
func (ProviderSnapshot) GoString() string { return "ProviderSnapshot{credential:redacted}" }

// ProviderSnapshotStore captures configuration and credential atomically with
// respect to setup changes. Implementations release their lock before returning.
type ProviderSnapshotStore interface {
	SnapshotAIProvider(context.Context, string) (ProviderSnapshot, error)
}

type providerSnapshotKey struct{}

// CaptureProviderSnapshot performs no network operation. The application's
// configuration store calls it while holding its short-lived setup lock.
func CaptureProviderSnapshot(ctx context.Context, config models.AIProviderConfig, secrets SecretStore) (ProviderSnapshot, error) {
	config, err := NormalizeProviderConfig(config)
	if err != nil {
		return ProviderSnapshot{}, err
	}
	// A caller must not be able to change captured headers after releasing its lock.
	if config.Headers != nil {
		headers := make(map[string]string, len(config.Headers))
		for key, value := range config.Headers {
			headers[key] = value
		}
		config.Headers = headers
	}
	snapshot := ProviderSnapshot{config: config}
	if config.APIKeyHeader != "" {
		if config.SecretRef == "" || secrets == nil {
			return ProviderSnapshot{}, errors.New("AI provider credential is not configured")
		}
		secret, err := secrets.Get(ctx, config.SecretRef)
		if err != nil || secret == "" || strings.ContainsAny(secret, "\r\n") {
			return ProviderSnapshot{}, errors.New("AI provider credential unavailable or invalid")
		}
		snapshot.credential = secret
	}
	return snapshot, nil
}

// Config returns configuration metadata, never the captured credential value.
func (s ProviderSnapshot) Config() models.AIProviderConfig {
	config := s.config
	if config.Headers != nil {
		config.Headers = make(map[string]string, len(s.config.Headers))
		for key, value := range s.config.Headers {
			config.Headers[key] = value
		}
	}
	return config
}

func WithProviderSnapshot(ctx context.Context, snapshot ProviderSnapshot) context.Context {
	return context.WithValue(ctx, providerSnapshotKey{}, snapshot)
}

func (s *Service) providerSnapshot(ctx context.Context, id string) (ProviderSnapshot, error) {
	if snapshot, ok := ctx.Value(providerSnapshotKey{}).(ProviderSnapshot); ok && snapshot.config.ID == id {
		return snapshot, nil
	}
	if source, ok := s.Configs.(ProviderSnapshotStore); ok {
		return source.SnapshotAIProvider(ctx, id)
	}
	if s.Configs == nil {
		return ProviderSnapshot{}, sql.ErrNoRows
	}
	config, err := s.Configs.GetAIProvider(ctx, id)
	if err != nil {
		return ProviderSnapshot{}, err
	}
	return CaptureProviderSnapshot(ctx, config, s.Secrets)
}
