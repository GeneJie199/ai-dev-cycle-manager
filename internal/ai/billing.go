package ai

import (
	"context"
	"encoding/json"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

var paidRequestSlot = make(chan struct{}, 1)

type BillingStore interface {
	ProviderBudget(context.Context, string) (models.ProviderBudget, error)
	ReserveProvider(context.Context, string, int, int) (models.ProviderReservation, error)
	SettleProvider(context.Context, models.ProviderReservation, int, int, bool) error
}

func providerUsage(c models.AIProviderConfig, body []byte) (int, int, bool) {
	var r struct {
		Usage struct {
			Prompt      *int `json:"prompt_tokens"`
			Completion  *int `json:"completion_tokens"`
			Total       *int `json:"total_tokens"`
			Input       *int `json:"input_tokens"`
			Output      *int `json:"output_tokens"`
			CacheRead   *int `json:"cache_read_input_tokens"`
			CacheCreate *int `json:"cache_creation_input_tokens"`
			Hit         *int `json:"prompt_cache_hit_tokens"`
			Miss        *int `json:"prompt_cache_miss_tokens"`
		} `json:"usage"`
		Gemini struct {
			Prompt   *int `json:"promptTokenCount"`
			Output   *int `json:"candidatesTokenCount"`
			Thoughts *int `json:"thoughtsTokenCount"`
			Total    *int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if json.Unmarshal(body, &r) != nil {
		return 0, 0, false
	}
	for _, v := range []*int{r.Usage.Prompt, r.Usage.Completion, r.Usage.Total, r.Usage.Input, r.Usage.Output, r.Usage.CacheRead, r.Usage.CacheCreate, r.Usage.Hit, r.Usage.Miss, r.Gemini.Prompt, r.Gemini.Output, r.Gemini.Thoughts, r.Gemini.Total} {
		if v != nil && (*v < 0 || *v > 10000000) {
			return 0, 0, false
		}
	}
	a, b, total := r.Usage.Prompt, r.Usage.Completion, r.Usage.Total
	if c.Kind == models.AIProviderAnthropic {
		a, b, total = r.Usage.Input, r.Usage.Output, nil
	}
	if c.Kind == models.AIProviderGemini {
		a, b, total = r.Gemini.Prompt, r.Gemini.Output, r.Gemini.Total
	}
	if a == nil || b == nil {
		return 0, 0, false
	}
	input, output := *a, *b
	if c.Kind == models.AIProviderAnthropic {
		if r.Usage.CacheRead != nil {
			input += *r.Usage.CacheRead
		}
		if r.Usage.CacheCreate != nil {
			input += *r.Usage.CacheCreate
		}
	}
	if c.Kind == models.AIProviderGemini && r.Gemini.Thoughts != nil {
		output += *r.Gemini.Thoughts
	}
	if total != nil && *total != input+output {
		return 0, 0, false
	}
	if r.Usage.Hit != nil || r.Usage.Miss != nil {
		if r.Usage.Hit == nil || r.Usage.Miss == nil || *r.Usage.Hit+*r.Usage.Miss != *a {
			return 0, 0, false
		}
	}
	return input, output, true
}
