package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/team"
)

func TestProbeCannotVerifyOrRestoreChangedSetup(t *testing.T) {
	for _, mutation := range []string{"model", "model-aba", "roles", "revoke", "other-provider"} {
		t.Run(mutation, func(t *testing.T) {
			a, _, _ := productFixture(t)
			ctx := context.Background()
			entered, release := make(chan struct{}), make(chan struct{})
			var enteredOnce, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				enteredOnce.Do(func() { close(entered) })
				<-release
				_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":10,"completion_tokens":10}}`)
			}))
			defer server.Close()
			defer unblock()
			in := AIProviderInput{ID: "fixture-api", Name: "Fixture API", Kind: models.AIProviderCustomOpenAI, BaseURL: server.URL, Model: "fixture-model"}
			if _, err := a.ConfigureAIProvider(ctx, in); err != nil {
				t.Fatal(err)
			}
			probeDone := make(chan error, 1)
			go func() { _, err := a.ProbeProductProvider(ctx, in.ID); probeDone <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("probe did not enter local fixture")
			}
			mutationDone := make(chan error, 1)
			go func() {
				var err error
				switch mutation {
				case "model", "model-aba":
					in.Model = "replacement-model-never-tested"
					_, err = a.ConfigureAIProvider(ctx, in)
					if mutation == "model-aba" && err == nil {
						in.Model = "fixture-model"
						_, err = a.ConfigureAIProvider(ctx, in)
					}
				case "revoke":
					err = a.RevokeProductProvider(ctx, in.ID)
				case "roles", "other-provider":
					roles := []string{"explorer"}
					if mutation == "other-provider" {
						in.ID = "second-provider"
					}
					_, err = a.RegisterProductProvider(ctx, ProviderRegistration{Transport: "api", Provider: in, Model: "fixture-model", ContextLimit: 200000, Tier: 2, Roles: roles, Capabilities: team.Capabilities})
				}
				mutationDone <- err
			}()
			select {
			case err := <-mutationDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("setup mutation blocked behind the model request")
			}
			unblock()
			select {
			case err := <-probeDone:
				if err == nil {
					t.Fatal("a stale probe accepted an intervening setup mutation")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("probe did not finish")
			}
			entries, err := a.registeredModels(ctx)
			if err != nil {
				t.Fatal(err)
			}
			foundOriginal, foundSecond := false, false
			for _, entry := range entries {
				if entry.Model.Provider == "second-provider" {
					foundSecond = true
				}
				if entry.Model.Provider != "fixture-api" {
					continue
				}
				foundOriginal = true
				if entry.VerifiedDigest != "" {
					t.Fatal("stale probe verified changed provider")
				}
				if mutation == "roles" && (len(entry.Roles) != 1 || entry.Roles[0] != "explorer") {
					t.Fatal("stale probe overwrote new role assignment")
				}
			}
			if mutation == "revoke" && foundOriginal {
				t.Fatal("late probe resurrected revoked provider")
			}
			if mutation == "other-provider" && (!foundOriginal || !foundSecond) {
				t.Fatal("late probe overwrote a concurrent registration")
			}
		})
	}
}

func TestGuidedTierAndHistoricalEvaluationKeepVerifiedAuthority(t *testing.T) {
	a, input, server := productFixture(t)
	ctx := context.Background()
	_, err := a.RegisterProductProvider(ctx, ProviderRegistration{Transport: "api", Provider: AIProviderInput{ID: "bad-tier", Name: "Invalid tier", Kind: models.AIProviderCustomOpenAI, BaseURL: server.URL}, Model: "fixture-model", ContextLimit: 200000, Tier: 3, Roles: team.Roles, Capabilities: team.Capabilities})
	if err == nil {
		t.Fatal("guided setup accepted a tier the Mission schema rejects")
	}
	if _, err = a.ProbeProductProvider(ctx, "fixture-api"); err != nil {
		t.Fatal(err)
	}
	m, err := a.CreateSimpleMission(ctx, SimpleMissionInput{Repository: input.Repository, Goal: "Inspect documentation", Checks: input.Checks, Trust: true, SourceExport: true, Providers: []string{"fixture-api"}})
	if err != nil {
		t.Fatal(err)
	}
	m.Models[0].Evaluation = 0.75
	m.Models[0].Capabilities = []string{"invented"}
	m.Models[0].Tier = 99
	routed, err := a.routedMission(ctx, m, "explorer")
	if err != nil || len(routed.Models) != 1 {
		t.Fatal(routed.Models, err)
	}
	if routed.Models[0].Evaluation != 0.75 || routed.Models[0].Tier != 2 || !team.Contains(routed.Models[0].Capabilities, "coding") {
		t.Fatalf("ranking discarded or unverified authority accepted: %+v", routed.Models[0])
	}
}
