package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type DiscoveredModel struct {
	ID              string `json:"id"`
	Name            string `json:"name,omitempty"`
	ContextWindow   int    `json:"context_window,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
}

// Discovery is an explicit user action and sends only authentication, never repository content.
func (s *Service) DiscoverModels(ctx context.Context, id string) ([]DiscoveredModel, error) {
	c, err := s.Configs.GetAIProvider(ctx, id)
	if err != nil {
		return nil, err
	}
	c, err = NormalizeProviderConfig(c)
	if err != nil {
		return nil, err
	}
	if !c.Enabled {
		return nil, errors.New("provider is disabled")
	}
	if c.Kind != "openai_compatible" && c.Kind != "custom_openai" && c.Kind != "local_openai" {
		return nil, errors.New("model discovery unsupported for this protocol; configure a verified model explicitly")
	}
	endpoint, err := url.JoinPath(c.BaseURL, "models")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if c.APIKeyHeader != "" {
		if s.Secrets == nil {
			return nil, errors.New("credential store unavailable")
		}
		secret, e := s.Secrets.Get(ctx, c.SecretRef)
		if e != nil {
			return nil, errors.New("credential is not configured")
		}
		if secret == "" || strings.ContainsAny(secret, "\r\n") {
			return nil, errors.New("invalid credential")
		}
		if c.APIKeyPrefix != "" {
			secret = c.APIKeyPrefix + " " + secret
		}
		req.Header.Set(c.APIKeyHeader, secret)
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	if s.Billing == nil {
		return nil, errors.New("explicit request-count budget required before discovery")
	}
	select {
	case paidRequestSlot <- struct{}{}:
		defer func() { <-paidRequestSlot }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	reservation, err := s.Billing.ReserveProvider(ctx, id, 1, 1)
	if err != nil {
		return nil, err
	}
	// Official model metadata requests have no generation usage/cost; the call remains counted.
	defer func() {
		settleCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Billing.SettleProvider(settleCtx, reservation, 0, 0, true)
	}()
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("provider unavailable: model discovery request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("model discovery HTTP %d; check official endpoint and API permission", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, errors.New("model discovery response invalid or too large")
	}
	var result struct {
		Data []DiscoveredModel `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Data) == 0 || len(result.Data) > 1000 {
		return nil, errors.New("provider returned no valid model list")
	}
	for _, m := range result.Data {
		if m.ID == "" || len(m.ID) > 200 || strings.ContainsAny(m.ID, "\r\n") {
			return nil, errors.New("invalid discovered model ID")
		}
	}
	return result.Data, nil
}
