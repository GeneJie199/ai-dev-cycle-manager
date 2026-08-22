package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/google/uuid"
)

func TestPlanningDocumentRevisionAndAtomicApply(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	requirement, err := store.CreateRequirement(ctx, "Plan a safe login change", "Keep password login")
	if err != nil {
		t.Fatal(err)
	}
	document := models.PlanningDocument{SchemaVersion: models.PlanningDocumentSchemaV1, RequirementID: requirement.ID, Understanding: "Add SMS login", Source: "manual", Status: "draft", Criteria: []models.PlanCriterion{{Description: "Password login remains available"}}, Tasks: []models.PlanTask{{Title: "Implement SMS login", Order: 1}}}
	saved, err := store.SavePlanningDocument(ctx, document)
	if err != nil || saved.Revision != 1 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	if _, err = store.SavePlanningDocument(ctx, document); !errors.Is(err, ErrPlanningRevisionConflict) {
		t.Fatalf("expected revision conflict, got %v", err)
	}

	now := time.Now().UTC()
	criterion := models.AcceptanceCriterion{ID: uuid.NewString(), RequirementID: requirement.ID, Description: "Password login remains available", CreatedAt: now}
	brokenTask := models.Task{ID: uuid.NewString(), RequirementID: requirement.ID, Title: "Implement SMS login", Status: models.TaskStatusTodo, DependsOn: []string{uuid.NewString()}, CreatedAt: now, UpdatedAt: now}
	saved.Status = "draft"
	if _, err = store.ApplyPlanningDocument(ctx, saved, []models.AcceptanceCriterion{criterion}, []models.Task{brokenTask}); err == nil {
		t.Fatal("expected foreign-key failure")
	}
	criteria, _ := store.ListCriteriaByRequirement(ctx, requirement.ID)
	if len(criteria) != 0 {
		t.Fatalf("partial criteria persisted: %+v", criteria)
	}
	unchanged, err := store.GetPlanningDocument(ctx, requirement.ID)
	if err != nil || unchanged.Revision != 1 || unchanged.Status != "draft" {
		t.Fatalf("document changed after rollback: %+v err=%v", unchanged, err)
	}

	validTask := brokenTask
	validTask.DependsOn = nil
	applied, err := store.ApplyPlanningDocument(ctx, saved, []models.AcceptanceCriterion{criterion}, []models.Task{validTask})
	if err != nil || applied.Revision != 2 || applied.Status != "applied" || applied.AppliedAt.IsZero() {
		t.Fatalf("applied=%+v err=%v", applied, err)
	}
}

func TestAIProviderSecretReferenceIsNotSerialized(t *testing.T) {
	store := openTestStore(t)
	provider := models.AIProviderConfig{ID: "provider-one", Name: "Provider One", Kind: models.AIProviderOpenAICompatible, BaseURL: "https://example.com/v1", Model: "model", Enabled: true, TimeoutSeconds: 30, SecretRef: "env:PROVIDER_ONE_KEY", Headers: map[string]string{"X-Organization": "example"}}
	saved, err := store.SaveAIProvider(context.Background(), provider)
	if err != nil || saved.SecretRef == "" {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	loaded, err := store.GetAIProvider(context.Background(), provider.ID)
	if err != nil || loaded.SecretRef != provider.SecretRef || loaded.Headers["X-Organization"] != "example" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	encoded, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), provider.SecretRef) || strings.Contains(string(encoded), "secret_ref") {
		t.Fatalf("secret reference leaked through JSON: %s", encoded)
	}
}
