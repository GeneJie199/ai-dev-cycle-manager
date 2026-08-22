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
	if config.BaseURL == "" || config.Model == "" {
		return config, errors.New("provider base URL and model are required")
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

func (s *Service) generateHTTP(ctx context.Context, config models.AIProviderConfig, prompt string, maxOutput int) (string, error) {
	config, err := NormalizeProviderConfig(config)
	if err != nil {
		return "", err
	}
	if !config.Enabled {
		return "", errors.New("AI provider is disabled")
	}
	endpoint, err := providerEndpoint(config)
	if err != nil {
		return "", err
	}
	body, err := providerRequestBody(config, prompt)
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
		if config.SecretRef == "" || s.Secrets == nil {
			return "", errors.New("AI provider credential is not configured")
		}
		secret, secretErr := s.Secrets.Get(runCtx, config.SecretRef)
		if secretErr != nil {
			return "", secretErr
		}
		if secret == "" || strings.ContainsAny(secret, "\r\n") {
			return "", errors.New("AI provider credential is empty or invalid")
		}
		credential := secret
		if config.APIKeyPrefix != "" {
			credential = config.APIKeyPrefix + " " + secret
		}
		request.Header.Set(config.APIKeyHeader, credential)
	}
	client := &http.Client{Timeout: time.Duration(config.TimeoutSeconds) * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
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
		message, _ := Redact(string(responseBody))
		return "", fmt.Errorf("%s returned HTTP %d: %s", config.ID, response.StatusCode, truncate(message, 1000))
	}
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
		value = map[string]any{"model": config.Model, "messages": []map[string]string{{"role": "user", "content": prompt}}, "temperature": 0.2}
	}
	return json.Marshal(value)
}

func providerResponseText(config models.AIProviderConfig, body []byte) (string, error) {
	switch config.Kind {
	case models.AIProviderAnthropic:
		var response struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return "", err
		}
		var output strings.Builder
		for _, part := range response.Content {
			if part.Type == "text" {
				output.WriteString(part.Text)
			}
		}
		if output.Len() == 0 {
			return "", errors.New("anthropic response contained no text")
		}
		return output.String(), nil
	case models.AIProviderGemini:
		var response struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return "", err
		}
		if len(response.Candidates) == 0 {
			return "", errors.New("gemini response contained no candidate")
		}
		var output strings.Builder
		for _, part := range response.Candidates[0].Content.Parts {
			output.WriteString(part.Text)
		}
		if output.Len() == 0 {
			return "", errors.New("gemini response contained no text")
		}
		return output.String(), nil
	default:
		var response struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return "", err
		}
		if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
			return "", errors.New("OpenAI-compatible response contained no message content")
		}
		return response.Choices[0].Message.Content, nil
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
