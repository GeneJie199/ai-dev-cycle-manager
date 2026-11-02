package app

import (
	"context"
	"encoding/json"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type dispatchGateSecrets struct {
	mu               sync.Mutex
	value            string
	enabled          bool
	calls            int
	entered, release chan struct{}
}

func (s *dispatchGateSecrets) Set(_ context.Context, id, value string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = value
	return "keyring:" + id, nil
}
func (s *dispatchGateSecrets) Delete(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = ""
	return nil
}
func (s *dispatchGateSecrets) Get(context.Context, string) (string, error) {
	s.mu.Lock()
	wait := false
	if s.enabled {
		s.calls++
		wait = s.calls == 2
	}
	s.mu.Unlock()
	if wait {
		close(s.entered)
		<-s.release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, nil
}

func TestMissionDispatchCannotUseReplacementSetupAfterRouting(t *testing.T) {
	a, input, old := productFixture(t)
	ctx := context.Background()
	secrets := &dispatchGateSecrets{entered: make(chan struct{}), release: make(chan struct{})}
	a.AI.Secrets = secrets
	in := AIProviderInput{ID: "fixture-api", Name: "Fixture API", Kind: models.AIProviderCustomOpenAI, BaseURL: old.URL + "/v1", Model: "fixture-model", APIKeyHeader: "Authorization", APIKeyPrefix: "Bearer", Secret: "synthetic-route-credential"}
	if _, err := a.ConfigureAIProvider(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ProbeProductProvider(ctx, "fixture-api"); err != nil {
		t.Fatal(err)
	}
	m, err := a.CreateSimpleMission(ctx, SimpleMissionInput{Repository: input.Repository, Goal: "Inspect guide text", Checks: input.Checks, Trust: true, SourceExport: true, Providers: []string{"fixture-api"}})
	if err != nil {
		t.Fatal(err)
	}
	var replacementCalls atomic.Int32
	replacement := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replacementCalls.Add(1)
		content := `{"summary":"local synthetic response","edits":[],"findings":[]}`
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10}})
	}))
	defer replacement.Close()
	var once sync.Once
	unblock := func() { once.Do(func() { close(secrets.release) }) }
	defer unblock()
	secrets.mu.Lock()
	secrets.enabled = true
	secrets.calls = 0
	secrets.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, err := a.AdvanceMission(ctx, m.ID); done <- err }()
	select {
	case <-secrets.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("routed readiness status capture was not reached")
	}
	in.BaseURL = replacement.URL
	in.Secret = ""
	if _, err := a.ConfigureAIProvider(ctx, in); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Logf("mission stopped: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("mission did not finish bounded attempt")
	}
	if replacementCalls.Load() != 0 {
		t.Fatalf("frozen Mission sent source to replacement unverified endpoint after routing: %d calls", replacementCalls.Load())
	}
}
