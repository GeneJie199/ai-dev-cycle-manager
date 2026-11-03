package app

import (
	"context"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

func (a *App) ProviderBudget(ctx context.Context, id string) (models.ProviderBudget, error) {
	s, err := a.TeamStore()
	if err != nil {
		return models.ProviderBudget{}, err
	}
	return s.ProviderBudget(ctx, id)
}
func (a *App) ReserveProvider(ctx context.Context, id string, input, output int) (models.ProviderReservation, error) {
	s, err := a.TeamStore()
	if err != nil {
		return models.ProviderReservation{}, err
	}
	return s.ReserveProvider(ctx, id, input, output)
}
func (a *App) SettleProvider(ctx context.Context, r models.ProviderReservation, input, output int, known bool) error {
	s, err := a.TeamStore()
	if err != nil {
		return err
	}
	return s.SettleProvider(ctx, r, input, output, known)
}
