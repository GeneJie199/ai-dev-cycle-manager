package ai

import (
	"context"
	"encoding/json"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAPIRequestNeverSentWithoutExplicitBudget(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	c := models.AIProviderConfig{ID: "fixture", Name: "fixture", Kind: models.AIProviderLocalOpenAI, BaseURL: server.URL, Model: "fixture", Enabled: true}
	s := NewService()
	s.Configs = providerStoreStub{map[string]models.AIProviderConfig{c.ID: c}}
	var result map[string]any
	if _, err := s.GenerateJSON(context.Background(), c.ID, "test", "hello", &result); err == nil || calls.Load() != 0 {
		t.Fatal("unbudgeted request sent")
	}
}
func TestMeteredAPIUnknownOrInvalidUsageBlocksSubsequentCalls(t *testing.T) {
	for _, mode := range []string{"missing", "negative", "over-bound", "unauthorized", "rate-limited", "malformed", "inconsistent-total"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var request map[string]any
				json.NewDecoder(r.Body).Decode(&request)
				if request["max_tokens"] != float64(2048) {
					t.Error("output cap omitted")
				}
				if _, ok := request["tools"]; ok {
					t.Error("hidden billable tools requested")
				}
				if mode == "unauthorized" || mode == "rate-limited" {
					code := 401
					if mode == "rate-limited" {
						code = 429
					}
					w.WriteHeader(code)
					io.WriteString(w, "upstream echoed opaque-real-key-value")
					return
				}
				if mode == "malformed" {
					io.WriteString(w, "bad JSON")
					return
				}
				response := map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": `{"ok":true}`}}}}
				if mode != "missing" {
					prompt, completion := 10, 10
					if mode == "negative" {
						prompt = -1
					}
					if mode == "over-bound" {
						completion = 3000
					}
					response["usage"] = map[string]int{"prompt_tokens": prompt, "completion_tokens": completion}
					if mode == "inconsistent-total" {
						response["usage"].(map[string]int)["total_tokens"] = 999
					}
				}
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			c := models.AIProviderConfig{ID: "fixture", Name: "fixture", Kind: models.AIProviderLocalOpenAI, BaseURL: server.URL, Model: "fixture", Enabled: true}
			s := NewService()
			s.Configs = providerStoreStub{map[string]models.AIProviderConfig{c.ID: c}}
			authorizeProviderFixture(t, s, c.ID)
			var result struct {
				OK bool `json:"ok"`
			}
			_, err := s.GenerateJSON(context.Background(), c.ID, "test", "hello", &result)
			if err == nil || strings.Contains(err.Error(), "opaque-real-key-value") {
				t.Fatalf("invalid/secret response accepted: %v", err)
			}
			_, err = s.GenerateJSON(context.Background(), c.ID, "test", "again", &result)
			if err == nil || calls.Load() != 1 {
				t.Fatalf("unknown usage did not block next call: %d %v", calls.Load(), err)
			}
			budget, _ := s.Billing.ProviderBudget(context.Background(), c.ID)
			if !budget.Blocked || budget.Calls != 1 || budget.ChargedMicros <= 0 {
				t.Fatalf("invalid reservation ledger %+v", budget)
			}
		})
	}
}
func TestStrictAPIResponseRejectsConflictsAndDuplicateVerdicts(t *testing.T) {
	for _, content := range []string{`{"review":"fail","findings":[]}
{"review":"pass","findings":[]}`, `{"review":"fail","review":"pass","findings":[]}`, "```json\n{\"review\":\"pass\",\"findings\":[]}\n```"} {
		var result struct {
			Review   string   `json:"review"`
			Findings []string `json:"findings"`
		}
		if err := decodeResponse(WithStrictResponse(context.Background()), content, &result); err == nil {
			t.Fatal("ambiguous/fenced API verdict accepted")
		}
	}
}

func TestMeteredAPITimeoutRetainsReservationAndBlocksRetry(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() { close(release); server.Close() }()
	c := models.AIProviderConfig{ID: "fixture", Name: "fixture", Kind: models.AIProviderLocalOpenAI, BaseURL: server.URL, Model: "fixture", Enabled: true}
	s := NewService()
	s.Configs = providerStoreStub{map[string]models.AIProviderConfig{c.ID: c}}
	authorizeProviderFixture(t, s, c.ID)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var result map[string]any
	if _, e := s.GenerateJSON(ctx, c.ID, "timeout", "hello", &result); e == nil {
		t.Fatal("timeout accepted")
	}
	budget, _ := s.Billing.ProviderBudget(context.Background(), c.ID)
	if calls.Load() != 1 || !budget.Blocked || budget.ChargedMicros <= 0 || budget.Calls != 1 {
		t.Fatalf("timeout not reserved/blocked: %+v calls=%d", budget, calls.Load())
	}
}
