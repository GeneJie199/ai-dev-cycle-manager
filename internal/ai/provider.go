package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

var providerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,63}$`)

type RequestPreview struct {
	Provider       string   `json:"provider"`
	Kind           string   `json:"kind"`
	Endpoint       string   `json:"endpoint"`
	Model          string   `json:"model"`
	RedactedInput  string   `json:"redactedInput"`
	RedactionCount int      `json:"redactionCount"`
	HeaderNames    []string `json:"headerNames"`
	Sends          []string `json:"sends"`
	Excludes       []string `json:"excludes"`
}

func NormalizeProviderConfig(config models.AIProviderConfig) (models.AIProviderConfig, error) {
	config.ID = strings.ToLower(strings.TrimSpace(config.ID))
	config.Name = strings.TrimSpace(config.Name)
	config.Kind = strings.ToLower(strings.TrimSpace(config.Kind))
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	config.Model = strings.TrimSpace(config.Model)
	config.APIPath = strings.TrimSpace(config.APIPath)
	config.APIKeyHeader = strings.TrimSpace(config.APIKeyHeader)
	config.APIKeyPrefix = strings.TrimSpace(config.APIKeyPrefix)
	if !providerIDPattern.MatchString(config.ID) || config.ID == "kimi" || config.ID == "codex" || config.ID == "claude" {
		return config, errors.New("provider ID must be 2 to 64 safe lowercase characters and must not use a built-in CLI ID")
	}
	if len(config.Name) < 2 || len(config.Name) > 100 {
		return config, errors.New("provider name must contain 2 to 100 characters")
	}
	defaults := map[string]struct{ baseURL, apiPath, keyHeader, keyPrefix string }{
		models.AIProviderOpenAICompatible: {"https://api.openai.com/v1", "/chat/completions", "Authorization", "Bearer"},
		models.AIProviderAnthropic:        {"https://api.anthropic.com/v1", "/messages", "x-api-key", ""},
		models.AIProviderGemini:           {"https://generativelanguage.googleapis.com/v1beta", "", "x-goog-api-key", ""},
		models.AIProviderLocalOpenAI:      {"http://127.0.0.1:11434/v1", "/chat/completions", "", ""},
		models.AIProviderCustomOpenAI:     {"", "/chat/completions", "", ""},
	}
	definition, ok := defaults[config.Kind]
	if !ok {
		return config, errors.New("unsupported provider kind")
	}
	if config.BaseURL == "" {
		config.BaseURL = definition.baseURL
	}
	if config.APIPath == "" {
		config.APIPath = definition.apiPath
	}
	if config.APIKeyHeader == "" {
		config.APIKeyHeader = definition.keyHeader
	}
	if config.APIKeyPrefix == "" {
		config.APIKeyPrefix = definition.keyPrefix
	}
	if config.BaseURL == "" {
		return config, errors.New("provider base URL is required")
	}
	if config.Kind == models.AIProviderCustomOpenAI && config.SecretRef != "" && config.APIKeyHeader == "" {
		return config, errors.New("custom provider credentials require a credential header name")
	}
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return config, errors.New("provider base URL must be an http(s) origin or path without credentials, query, or fragment")
	}
	if parsed.Scheme == "http" && ProviderRequiresSecret(config) && !isLoopbackHost(parsed.Hostname()) {
		return config, errors.New("providers with credentials require HTTPS unless the endpoint is loopback")
	}
	if config.APIPath != "" && (!strings.HasPrefix(config.APIPath, "/") || strings.ContainsAny(config.APIPath, "?#")) {
		return config, errors.New("provider API path must be an absolute path without query or fragment")
	}
	if config.APIKeyHeader != "" && !validHeaderName(config.APIKeyHeader) {
		return config, errors.New("provider credential header name is invalid")
	}
	if len(config.APIKeyPrefix) > 64 || strings.ContainsAny(config.APIKeyPrefix, "\r\n") {
		return config, errors.New("provider credential prefix must be at most 64 characters and contain no line breaks")
	}
	if config.TimeoutSeconds == 0 {
		config.TimeoutSeconds = 120
	}
	if config.TimeoutSeconds < 5 || config.TimeoutSeconds > 600 {
		return config, errors.New("provider timeout must be between 5 and 600 seconds")
	}
	if len(config.Headers) > 20 {
		return config, errors.New("provider has too many custom headers")
	}
	normalizedHeaders := make(map[string]string, len(config.Headers))
	for name, value := range config.Headers {
		name = http.CanonicalHeaderKey(strings.TrimSpace(name))
		if !validHeaderName(name) || len(value) > 2048 || strings.ContainsAny(value, "\r\n") {
			return config, fmt.Errorf("custom header %q is invalid", name)
		}
		lowerName := strings.ToLower(name)
		switch lowerName {
		case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "x-goog-api-key":
			return config, fmt.Errorf("secret header %q must use the credential field", name)
		}
		if strings.Contains(lowerName, "token") || strings.Contains(lowerName, "secret") || strings.Contains(lowerName, "api-key") || strings.Contains(lowerName, "apikey") || strings.Contains(lowerName, "credential") {
			return config, fmt.Errorf("secret-looking header %q must use the credential field", name)
		}
		normalizedHeaders[name] = value
	}
	config.Headers = normalizedHeaders
	return config, nil
}

func ProviderRequiresSecret(config models.AIProviderConfig) bool {
	switch config.Kind {
	case models.AIProviderOpenAICompatible, models.AIProviderAnthropic, models.AIProviderGemini:
		return true
	case models.AIProviderCustomOpenAI:
		return config.APIKeyHeader != "" || config.SecretRef != ""
	default:
		return false
	}
}

func (s *Service) RequestPreview(ctx context.Context, provider, input string) (RequestPreview, error) {
	if s.Configs == nil {
		return RequestPreview{}, errors.New("configured API providers are unavailable")
	}
	config, err := s.Configs.GetAIProvider(ctx, provider)
	if err != nil {
		return RequestPreview{}, err
	}
	config, err = NormalizeProviderConfig(config)
	if err != nil {
		return RequestPreview{}, err
	}
	endpoint, err := providerEndpoint(config)
	if err != nil {
		return RequestPreview{}, err
	}
	redacted, count := Redact(input)
	headers := []string{"Content-Type"}
	for name := range config.Headers {
		headers = append(headers, name)
	}
	if config.APIKeyHeader != "" {
		headers = append(headers, http.CanonicalHeaderKey(config.APIKeyHeader))
	}
	if config.Kind == models.AIProviderAnthropic {
		headers = append(headers, "Anthropic-Version")
	}
	sort.Strings(headers)
	return RequestPreview{Provider: config.ID, Kind: config.Kind, Endpoint: endpoint, Model: config.Model, RedactedInput: redacted, RedactionCount: count, HeaderNames: headers, Sends: []string{"the redacted requirement and planning context", "the selected model name", "structured JSON generation instructions"}, Excludes: []string{"API keys and credential values", "private keys", "unselected files", "full environment variables", "Git credentials", "database business rows"}}, nil
}

func (s *Service) generateHTTP(ctx context.Context, snapshot ProviderSnapshot, prompt string, maxOutput int) (text string, resultErr error) {
	config := snapshot.config
	if !config.Enabled {
		return "", errors.New("AI provider is disabled")
	}
	if config.Model == "" {
		return "", errors.New("configuration required: choose a model from provider discovery")
	}
	endpoint, err := providerEndpoint(config)
	if err != nil {
		return "", err
	}
	if s.Billing == nil {
		return "", errors.New("configuration required: explicit provider budget")
	}
	select {
	case paidRequestSlot <- struct{}{}:
		defer func() { <-paidRequestSlot }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	budget, err := s.Billing.ProviderBudget(ctx, config.ID)
	if err != nil {
		return "", errors.New("configuration required: explicit provider budget")
	}
	body, err := providerRequestBody(config, prompt)
	if err != nil {
		return "", err
	}
	var boundedBody map[string]any
	if err = json.Unmarshal(body, &boundedBody); err != nil {
		return "", err
	}
	switch config.Kind {
	case models.AIProviderGemini:
		boundedBody["generationConfig"].(map[string]any)["maxOutputTokens"] = budget.MaxOutputTokens
	default:
		boundedBody["max_tokens"] = budget.MaxOutputTokens
	}
	body, err = json.Marshal(boundedBody)
	if err != nil {
		return "", err
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(config.TimeoutSeconds)*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(runCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	for name, value := range config.Headers {
		request.Header.Set(name, value)
	}
	if config.Kind == models.AIProviderAnthropic {
		request.Header.Set("Anthropic-Version", "2023-06-01")
	}
	if config.APIKeyHeader != "" {
		credential := snapshot.credential
		if config.APIKeyPrefix != "" {
			credential = config.APIKeyPrefix + " " + credential
		}
		request.Header.Set(config.APIKeyHeader, credential)
	}
	client := &http.Client{Timeout: time.Duration(config.TimeoutSeconds) * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	reservation, reserveErr := s.Billing.ReserveProvider(runCtx, config.ID, len(body)+1024, budget.MaxOutputTokens)
	if reserveErr != nil {
		return "", reserveErr
	}
	usageKnown := false
	inputTokens, outputTokens := 0, 0
	defer func() {
		settleCtx, settleCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer settleCancel()
		if settleErr := s.Billing.SettleProvider(settleCtx, reservation, inputTokens, outputTokens, usageKnown); settleErr != nil {
			text = ""
			if resultErr != nil {
				resultErr = fmt.Errorf("%v; %w", resultErr, settleErr)
			} else {
				resultErr = settleErr
			}
		}
	}()
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%s generation timed out after %d seconds", config.ID, config.TimeoutSeconds)
		}
		return "", fmt.Errorf("%s request failed: %w", config.ID, err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, int64(maxOutput)+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if len(responseBody) > maxOutput {
		return "", fmt.Errorf("%s output exceeded %d bytes", config.ID, maxOutput)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("%s returned HTTP %d; check credentials, model and provider status", config.ID, response.StatusCode)
	}
	inputTokens, outputTokens, usageKnown = providerUsage(config, responseBody)
	return providerResponseText(config, responseBody)
}

func providerEndpoint(config models.AIProviderConfig) (string, error) {
	if config.Kind == models.AIProviderGemini {
		model := strings.TrimPrefix(config.Model, "models/")
		if model == "" || strings.Contains(model, "..") {
			return "", errors.New("gemini model name is invalid")
		}
		return url.JoinPath(config.BaseURL, "models", model+":generateContent")
	}
	return url.JoinPath(config.BaseURL, strings.TrimPrefix(config.APIPath, "/"))
}

func providerRequestBody(config models.AIProviderConfig, prompt string) ([]byte, error) {
	var value any
	switch config.Kind {
	case models.AIProviderAnthropic:
		value = map[string]any{"model": config.Model, "max_tokens": 8192, "messages": []map[string]string{{"role": "user", "content": prompt}}}
	case models.AIProviderGemini:
		value = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []map[string]string{{"text": prompt}}}}, "generationConfig": map[string]any{"responseMimeType": "application/json"}}
	default:
		body := map[string]any{"model": config.Model, "messages": []map[string]string{{"role": "user", "content": prompt}}, "temperature": 0.2}
		// DeepSeek enables thinking by default; bounded structured responses use
		// its documented non-thinking mode, without naming a particular model.
		// https://api-docs.deepseek.com/guides/thinking_mode/
		if endpoint, err := url.Parse(config.BaseURL); err == nil && endpoint.Scheme == "https" && strings.EqualFold(endpoint.Hostname(), "api.deepseek.com") && (endpoint.Port() == "" || endpoint.Port() == "443") {
			body["thinking"] = map[string]string{"type": "disabled"}
		}
		value = body
	}
	return json.Marshal(value)
}

func providerResponseText(config models.AIProviderConfig, body []byte) (string, error) {
	switch config.Kind {
	case models.AIProviderAnthropic:
		var response struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return "", err
		}
		if response.StopReason != "end_turn" {
			return "", errors.New("anthropic response did not complete a normal text turn")
		}
		var output strings.Builder
		for _, part := range response.Content {
			if part.Type != "text" {
				return "", errors.New("anthropic response requested unsupported non-text content")
			}
			output.WriteString(part.Text)
		}
		if output.Len() == 0 {
			return "", errors.New("anthropic response contained no text")
		}
		return output.String(), nil
	case models.AIProviderGemini:
		var response struct {
			Candidates []struct {
				FinishReason string `json:"finishReason"`
				Content      struct {
					Parts []map[string]json.RawMessage `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return "", err
		}
		if len(response.Candidates) != 1 || response.Candidates[0].FinishReason != "STOP" {
			return "", errors.New("gemini response did not complete exactly one normal text candidate")
		}
		var output strings.Builder
		for _, part := range response.Candidates[0].Content.Parts {
			var text string
			if len(part) != 1 || json.Unmarshal(part["text"], &text) != nil {
				return "", errors.New("gemini response contained unsupported non-text content")
			}
			output.WriteString(text)
		}
		if output.Len() == 0 {
			return "", errors.New("gemini response contained no text")
		}
		return output.String(), nil
	default:
		var response struct {
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Message      struct {
					Content      string            `json:"content"`
					Refusal      string            `json:"refusal"`
					ToolCalls    []json.RawMessage `json:"tool_calls"`
					FunctionCall json.RawMessage   `json:"function_call"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return "", err
		}
		if len(response.Choices) != 1 || response.Choices[0].FinishReason != "stop" {
			return "", errors.New("OpenAI-compatible response did not complete exactly one normal text choice")
		}
		message := response.Choices[0].Message
		if message.Refusal != "" || len(message.ToolCalls) != 0 || (len(message.FunctionCall) != 0 && string(message.FunctionCall) != "null") {
			return "", errors.New("OpenAI-compatible response refused or requested unsupported tools")
		}
		if strings.TrimSpace(message.Content) == "" {
			return "", errors.New("OpenAI-compatible response contained no message content")
		}
		return message.Content, nil
	}
}

func validHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && !strings.ContainsRune("!#$%&'*+-.^_`|~", char) {
			return false
		}
	}
	return true
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
