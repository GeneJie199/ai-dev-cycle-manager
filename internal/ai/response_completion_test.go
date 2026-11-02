package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

func TestResponseMustCompleteOneSupportedTextTurn(t *testing.T) {
	const verdict = `{"review":"pass","findings":[]}`
	for _, reason := range []string{"stop", "length", "content_filter", "tool_calls", "function_call", "", "unknown"} {
		t.Run("openai-"+reason, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": reason, "message": map[string]any{"content": verdict}}}})
			text, err := providerResponseText(models.AIProviderConfig{Kind: models.AIProviderOpenAICompatible}, body)
			if (err == nil) != (reason == "stop") || (err == nil && text != verdict) {
				t.Fatalf("incomplete response accepted or complete response lost: %q %v", text, err)
			}
		})
	}
	for _, reason := range []string{"end_turn", "max_tokens", "tool_use", "stop_sequence", "refusal", "", "unknown"} {
		t.Run("anthropic-"+reason, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"stop_reason": reason, "content": []any{map[string]any{"type": "text", "text": verdict}}})
			text, err := providerResponseText(models.AIProviderConfig{Kind: models.AIProviderAnthropic}, body)
			if (err == nil) != (reason == "end_turn") || (err == nil && text != verdict) {
				t.Fatalf("incomplete response accepted or complete response lost: %q %v", text, err)
			}
		})
	}
	for _, reason := range []string{"STOP", "MAX_TOKENS", "SAFETY", "RECITATION", "MALFORMED_FUNCTION_CALL", "", "unknown"} {
		t.Run("gemini-"+reason, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{"finishReason": reason, "content": map[string]any{"parts": []any{map[string]any{"text": verdict}}}}}})
			text, err := providerResponseText(models.AIProviderConfig{Kind: models.AIProviderGemini}, body)
			if (err == nil) != (reason == "STOP") || (err == nil && text != verdict) {
				t.Fatalf("incomplete response accepted or complete response lost: %q %v", text, err)
			}
		})
	}
	for _, field := range []string{"refusal", "tool_calls", "function_call"} {
		t.Run("openai-unsupported-"+field, func(t *testing.T) {
			message := map[string]any{"content": verdict}
			switch field {
			case "refusal":
				message[field] = "Refused"
			case "tool_calls":
				message[field] = []any{map[string]any{"type": "function"}}
			case "function_call":
				message[field] = map[string]any{"name": "unsupported"}
			}
			body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": message}}})
			if _, err := providerResponseText(models.AIProviderConfig{Kind: models.AIProviderOpenAICompatible}, body); err == nil {
				t.Fatal("unsupported action accepted despite valid-looking JSON verdict")
			}
		})
	}
	for _, test := range []struct{ kind, body string }{
		{models.AIProviderAnthropic, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{}"},{"type":"tool_use","name":"unsupported"}]}`},
		{models.AIProviderGemini, `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"{}"},{"functionCall":{"name":"unsupported"}}]}}]}`},
		{models.AIProviderOpenAICompatible, `{"choices":[{"finish_reason":"stop","message":{"content":"{}"}},{"finish_reason":"stop","message":{"content":"{}"}}]}`},
	} {
		if _, err := providerResponseText(models.AIProviderConfig{Kind: test.kind}, []byte(test.body)); err == nil {
			t.Fatal("unsupported/multiple completion accepted", test.kind)
		}
	}
}

func TestIncompleteResponseStillAccountsUsageWithoutAutomaticRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "length", "message": map[string]string{"content": `{"ok":true}`}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10}})
	}))
	defer server.Close()
	config := models.AIProviderConfig{ID: "incomplete-fixture", Name: "Incomplete fixture", Kind: models.AIProviderLocalOpenAI, BaseURL: server.URL, Model: "fixture", Enabled: true}
	service := NewService()
	service.Configs = providerStoreStub{map[string]models.AIProviderConfig{config.ID: config}}
	authorizeProviderFixture(t, service, config.ID)
	var result struct{ OK bool }
	_, err := service.GenerateJSON(context.Background(), config.ID, "incomplete fixture", "Return JSON", &result)
	budget, budgetErr := service.Billing.ProviderBudget(context.Background(), config.ID)
	if err == nil || result.OK || calls.Load() != 1 || budgetErr != nil || budget.Calls != 1 || budget.ChargedMicros != 100 || budget.Blocked {
		t.Fatalf("incomplete response passed, retried, or lost known usage: result=%+v calls=%d budget=%+v errors=%v/%v", result, calls.Load(), budget, err, budgetErr)
	}
}
