package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

type providerStoreStub struct {
	providers map[string]models.AIProviderConfig
}

func (s providerStoreStub) GetAIProvider(_ context.Context, id string) (models.AIProviderConfig, error) {
	provider, ok := s.providers[id]
	if !ok {
		return provider, sql.ErrNoRows
	}
	return provider, nil
}

func (s providerStoreStub) ListAIProviders(context.Context) ([]models.AIProviderConfig, error) {
	providers := make([]models.AIProviderConfig, 0, len(s.providers))
	for _, provider := range s.providers {
		providers = append(providers, provider)
	}
	return providers, nil
}

type secretStoreStub struct{ values map[string]string }

func (s *secretStoreStub) Set(_ context.Context, id, value string) (string, error) {
	if s.values == nil {
		s.values = map[string]string{}
	}
	ref := "memory:" + id
	s.values[ref] = value
	return ref, nil
}
func (s *secretStoreStub) Get(_ context.Context, ref string) (string, error) {
	value, ok := s.values[ref]
	if !ok {
		return "", errors.New("missing test secret")
	}
	return value, nil
}
func (s *secretStoreStub) Delete(_ context.Context, ref string) error {
	delete(s.values, ref)
	return nil
}

func TestConfiguredOpenAIProviderRedactsInputAndUsesCredentialHeader(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer real-key" {
			t.Fatalf("request path=%q authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"ok":true}`}}}})
	}))
	defer server.Close()
	config := models.AIProviderConfig{ID: "test-openai", Name: "Test OpenAI", Kind: models.AIProviderCustomOpenAI, BaseURL: server.URL + "/v1", Model: "test-model", APIPath: "/chat/completions", APIKeyHeader: "Authorization", APIKeyPrefix: "Bearer ", SecretRef: "memory:test-openai", Enabled: true, TimeoutSeconds: 10}
	service := NewService()
	service.Configs = providerStoreStub{providers: map[string]models.AIProviderConfig{config.ID: config}}
	service.Secrets = &secretStoreStub{values: map[string]string{config.SecretRef: "real-key"}}
	var result struct {
		OK bool `json:"ok"`
	}
	meta, err := service.GenerateJSON(context.Background(), config.ID, "test", "token=abc123 build the feature", &result)
	if err != nil || !result.OK {
		t.Fatalf("result=%+v meta=%+v err=%v", result, meta, err)
	}
	if strings.Contains(receivedBody, "abc123") || !strings.Contains(receivedBody, "[REDACTED]") || meta.RedactionCount != 1 {
		t.Fatalf("redaction failed: body=%s meta=%+v", receivedBody, meta)
	}
	preview, err := service.RequestPreview(context.Background(), config.ID, "password=hunter2 inspect")
	if err != nil || strings.Contains(preview.RedactedInput, "hunter2") || preview.RedactionCount != 1 || strings.Contains(strings.Join(preview.HeaderNames, ","), "real-key") {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
}

func TestProviderDoesNotFollowCredentialedRedirect(t *testing.T) {
	targetRequests := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetRequests++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer redirect.Close()
	config := models.AIProviderConfig{ID: "redirect-test", Name: "Redirect Test", Kind: models.AIProviderCustomOpenAI, BaseURL: redirect.URL, Model: "model", APIPath: "/chat", APIKeyHeader: "Authorization", APIKeyPrefix: "Bearer ", SecretRef: "memory:redirect", Enabled: true, TimeoutSeconds: 10}
	service := NewService()
	service.Configs = providerStoreStub{providers: map[string]models.AIProviderConfig{config.ID: config}}
	service.Secrets = &secretStoreStub{values: map[string]string{config.SecretRef: "key"}}
	var result map[string]any
	if _, err := service.GenerateJSON(context.Background(), config.ID, "test", "input", &result); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("unexpected error: %v", err)
	}
	if targetRequests != 0 {
		t.Fatalf("redirect target received %d requests", targetRequests)
	}
}

func TestProviderValidationRejectsCredentialOverRemoteHTTPAndSecretHeaders(t *testing.T) {
	config := models.AIProviderConfig{ID: "unsafe-http", Name: "Unsafe HTTP", Kind: models.AIProviderCustomOpenAI, BaseURL: "http://example.com/v1", Model: "model", APIKeyHeader: "Authorization", SecretRef: "env:KEY", Enabled: true}
	if _, err := NormalizeProviderConfig(config); err == nil || !strings.Contains(err.Error(), "require HTTPS") {
		t.Fatalf("unexpected error: %v", err)
	}
	config = models.AIProviderConfig{ID: "unsafe-header", Name: "Unsafe Header", Kind: models.AIProviderLocalOpenAI, Model: "model", Headers: map[string]string{"X-Service-Token": "plaintext"}, Enabled: true}
	if _, err := NormalizeProviderConfig(config); err == nil || !strings.Contains(err.Error(), "credential field") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnvironmentSecretReference(t *testing.T) {
	t.Setenv("DEVCYCLE_TEST_KEY", "secret-value")
	ref, err := EnvironmentSecretRef("DEVCYCLE_TEST_KEY")
	if err != nil {
		t.Fatal(err)
	}
	value, err := NewKeyringSecretStore().Get(context.Background(), ref)
	if err != nil || value != "secret-value" {
		t.Fatalf("value=%q err=%v", value, err)
	}
}

func TestAnthropicAndGeminiProviderProtocols(t *testing.T) {
	tests := []struct {
		name         string
		kind         string
		model        string
		wantPath     string
		wantHeader   string
		responseBody any
	}{
		{name: "anthropic", kind: models.AIProviderAnthropic, model: "claude-test", wantPath: "/v1/messages", wantHeader: "x-api-key", responseBody: map[string]any{"content": []any{map[string]string{"type": "text", "text": `{"ok":true}`}}}},
		{name: "gemini", kind: models.AIProviderGemini, model: "gemini-test", wantPath: "/v1beta/models/gemini-test:generateContent", wantHeader: "x-goog-api-key", responseBody: map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]string{"text": `{"ok":true}`}}}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.wantPath || r.Header.Get(tt.wantHeader) != "protocol-key" || r.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("path=%q headers=%v", r.URL.Path, r.Header)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if tt.kind == models.AIProviderAnthropic {
					if body["model"] != tt.model || r.Header.Get("Anthropic-Version") == "" {
						t.Fatalf("anthropic request=%v headers=%v", body, r.Header)
					}
				} else if _, ok := body["contents"]; !ok {
					t.Fatalf("gemini request=%v", body)
				}
				_ = json.NewEncoder(w).Encode(tt.responseBody)
			}))
			defer server.Close()
			base := server.URL + "/v1"
			if tt.kind == models.AIProviderGemini {
				base = server.URL + "/v1beta"
			}
			config := models.AIProviderConfig{ID: "protocol-" + tt.name, Name: "Protocol " + tt.name, Kind: tt.kind, BaseURL: base, Model: tt.model, SecretRef: "memory:protocol", Enabled: true, TimeoutSeconds: 10}
			service := NewService()
			service.Configs = providerStoreStub{providers: map[string]models.AIProviderConfig{config.ID: config}}
			service.Secrets = &secretStoreStub{values: map[string]string{config.SecretRef: "protocol-key"}}
			var result struct {
				OK bool `json:"ok"`
			}
			if _, err := service.GenerateJSON(context.Background(), config.ID, "protocol test", "respond", &result); err != nil || !result.OK {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}
