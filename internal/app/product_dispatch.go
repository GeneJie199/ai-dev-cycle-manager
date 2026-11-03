package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/team"
)

type missionDispatchKey struct{}

// validatePinnedProviderDispatch requires productMu. A Simple Mission's approved
// binding must still match the exact configuration captured for this dispatch.
func (a *App) validatePinnedProviderDispatch(ctx context.Context, adapter, model string, config any) error {
	missionID, _ := ctx.Value(missionDispatchKey{}).(string)
	if missionID == "" {
		return nil
	}
	s, err := a.TeamStore()
	if err != nil {
		return err
	}
	var bindings []RegisteredModel
	if err = s.ProductSetting(ctx, "mission-routing:"+missionID, &bindings); errors.Is(err, sql.ErrNoRows) {
		return nil // Existing Expert Missions retain their explicit adapter policy.
	} else if err != nil {
		return err
	}
	role, _ := ctx.Value(apiRoleKey{}).(string)
	entries, err := a.registeredModels(ctx)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.Model.Adapter != adapter || binding.Model.ID != model || !team.Contains(binding.Roles, role) || binding.VerifiedDigest == "" {
			continue
		}
		for _, entry := range entries {
			if entry.Model.Provider == binding.Model.Provider && entry.Model.Adapter == adapter && entry.Model.ID == model && entry.LastError == "" && entry.VerifiedDigest == binding.VerifiedDigest && entry.VerifiedDigest == providerDigest(entry, config) {
				return nil
			}
		}
	}
	return validationf("provider configuration or role changed after Mission approval; no source sent, verify the connection and create a new Mission")
}
