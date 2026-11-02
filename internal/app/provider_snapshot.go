package app

import (
	"context"

	devai "github.com/GeneJie199/ai-dev-cycle-manager/internal/ai"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

// This private wrapper prevents a snapshot's credential from becoming a public
// App binding while preserving the existing provider configuration interface.
type providerConfigSource struct{ app *App }

func (s providerConfigSource) GetAIProvider(ctx context.Context, id string) (models.AIProviderConfig, error) {
	return s.app.Store.GetAIProvider(ctx, id)
}

func (s providerConfigSource) ListAIProviders(ctx context.Context) ([]models.AIProviderConfig, error) {
	return s.app.Store.ListAIProviders(ctx)
}

func (s providerConfigSource) SnapshotAIProvider(ctx context.Context, id string) (devai.ProviderSnapshot, error) {
	s.app.productMu.Lock()
	defer s.app.productMu.Unlock()
	return s.app.captureProviderSnapshot(ctx, id)
}

// captureProviderSnapshot requires productMu; no lock crosses a network request.
func (a *App) captureProviderSnapshot(ctx context.Context, id string) (devai.ProviderSnapshot, error) {
	config, err := a.Store.GetAIProvider(ctx, id)
	if err != nil {
		return devai.ProviderSnapshot{}, err
	}
	return devai.CaptureProviderSnapshot(ctx, config, a.AI.Secrets)
}
