package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

type rotationFixtureSecrets struct {
	mu               sync.Mutex
	value            string
	armed            atomic.Bool
	entered, release chan struct{}
}

func (s *rotationFixtureSecrets) Set(_ context.Context, id, value string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = value
	return "keyring:" + id, nil
}
func (s *rotationFixtureSecrets) Get(context.Context, string) (string, error) {
	if s.armed.CompareAndSwap(true, false) {
		close(s.entered)
		<-s.release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, nil
}
func (s *rotationFixtureSecrets) Delete(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = ""
	return nil
}

func TestProviderCredentialRotationCapturesAtomicallyAndReleasesBeforeNetwork(t *testing.T) {
	for _, path := range []string{"generation", "discovery", "probe", "coding"} {
		t.Run(path, func(t *testing.T) {
			a, input, _ := productFixture(t)
			ctx := context.Background()
			secrets := &rotationFixtureSecrets{entered: make(chan struct{}), release: make(chan struct{})}
			a.AI.Secrets = secrets
			requestEntered, releaseNetwork := make(chan struct{}), make(chan struct{})
			var captureOnce, networkOnce sync.Once
			unblockCapture := func() { captureOnce.Do(func() { close(secrets.release) }) }
			unblockNetwork := func() { networkOnce.Do(func() { close(releaseNetwork) }) }
			var actualAuth string
			var replacementCalls atomic.Int32
			old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				actualAuth = r.Header.Get("Authorization")
				close(requestEntered)
				<-releaseNetwork
				if r.Method == http.MethodGet {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "fixture-model"}}})
					return
				}
				content := `{"ok":true}`
				if path == "coding" {
					content = `{"summary":"fixture capture","edits":[],"findings":[]}`
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10}})
			}))
			replacement := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				replacementCalls.Add(1)
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer replacement.Close()
			defer old.Close()
			defer unblockCapture()
			defer unblockNetwork()
			in := AIProviderInput{ID: "fixture-api", Name: "Fixture API", Kind: models.AIProviderCustomOpenAI, BaseURL: old.URL, Model: "fixture-model", APIKeyHeader: "Authorization", APIKeyPrefix: "Bearer", Secret: "synthetic-original-credential"}
			if _, err := a.ConfigureAIProvider(ctx, in); err != nil {
				t.Fatal(err)
			}
			var task models.Task
			if path == "coding" {
				req, err := a.CreateRequirement(ctx, "Atomic capture", "local fixture")
				if err != nil {
					t.Fatal(err)
				}
				task, err = a.CreateTask(ctx, req.ID, "Explorer", "local fixture")
				if err == nil {
					task, _, err = a.LinkTaskToWorktree(ctx, input.Repository, task.ID, "atomic-capture", filepath.Join(t.TempDir(), "worktree"))
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			secrets.armed.Store(true)
			done := make(chan error, 1)
			go func() {
				var err error
				switch path {
				case "generation":
					var response struct{ OK bool }
					_, err = a.AI.GenerateJSON(ctx, in.ID, "atomic capture fixture", "hello", &response)
				case "discovery":
					_, err = a.AI.DiscoverModels(ctx, in.ID)
				case "probe":
					_, err = a.ProbeProductProvider(ctx, in.ID)
				case "coding":
					var session models.AgentSession
					session, err = a.startAgentSessionModel(context.WithValue(ctx, apiRoleKey{}, "explorer"), task.ID, "api-fixture-api", "fixture-model", "Inspect source")
					deadline := time.Now().Add(10 * time.Second)
					for err == nil && session.Status == "running" && time.Now().Before(deadline) {
						time.Sleep(10 * time.Millisecond)
						session, err = a.Store.GetAgentSession(ctx, session.ID)
					}
					if err == nil && session.Status != "completed" {
						err = errors.New("coding fixture did not complete")
					}
				}
				done <- err
			}()
			select {
			case <-secrets.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("credential snapshot was not reached")
			}
			mutationStarted, mutationDone := make(chan struct{}), make(chan error, 1)
			go func() {
				close(mutationStarted)
				in.BaseURL, in.Secret = replacement.URL, "synthetic-replacement-credential"
				_, err := a.ConfigureAIProvider(ctx, in)
				mutationDone <- err
			}()
			<-mutationStarted
			select {
			case err := <-mutationDone:
				t.Fatalf("setup changed while a credential capture was incomplete: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			unblockCapture()
			select {
			case <-requestEntered:
			case <-time.After(5 * time.Second):
				t.Fatal("captured request was not sent")
			}
			select {
			case err := <-mutationDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("network request retained the setup lock")
			}
			if actualAuth != "Bearer synthetic-original-credential" || replacementCalls.Load() != 0 {
				t.Fatal("request combined the original endpoint with replacement configuration or credentials")
			}
			unblockNetwork()
			select {
			case err := <-done:
				if (err != nil) != (path == "probe") {
					t.Fatalf("unexpected captured request outcome: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("captured request did not finish")
			}
			budget, err := a.ProviderBudget(ctx, "fixture-api")
			if err != nil || budget.Calls != 1 {
				t.Fatal("actual request was not counted exactly once", budget.Calls, err)
			}
		})
	}
}
