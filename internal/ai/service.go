package ai

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

type ProviderStatus struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Kind             string            `json:"kind"`
	Transport        string            `json:"transport"`
	Available        bool              `json:"available"`
	Configured       bool              `json:"configured"`
	Enabled          bool              `json:"enabled"`
	Reason           string            `json:"reason,omitempty"`
	BaseURL          string            `json:"baseUrl,omitempty"`
	Model            string            `json:"model,omitempty"`
	APIPath          string            `json:"apiPath,omitempty"`
	APIKeyHeader     string            `json:"apiKeyHeader,omitempty"`
	APIKeyPrefix     string            `json:"apiKeyPrefix,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	TimeoutSeconds   int               `json:"timeoutSeconds,omitempty"`
	SecretSource     string            `json:"secretSource,omitempty"`
	RequiresSecret   bool              `json:"requiresSecret"`
	SecretConfigured bool              `json:"secretConfigured"`
	Binary           string            `json:"-"`
}

type ProviderConfigStore interface {
	GetAIProvider(context.Context, string) (models.AIProviderConfig, error)
	ListAIProviders(context.Context) ([]models.AIProviderConfig, error)
}

type GenerationMeta struct {
	Provider             string `json:"provider"`
	GeneratedAt          string `json:"generatedAt"`
	RedactionCount       int    `json:"redactionCount"`
	InputChars           int    `json:"inputChars"`
	OutputBytes          int    `json:"outputBytes"`
	DurationMilliseconds int64  `json:"durationMilliseconds"`
	CostStatus           string `json:"costStatus"`
}

type Service struct {
	Timeout        time.Duration
	MaxInputChars  int
	MaxOutputBytes int
	Configs        ProviderConfigStore
	Secrets        SecretStore
	mu             sync.Mutex
	slots          chan struct{}
}

func NewService() *Service {
	maxInput := 120000
	if runtime.GOOS == "windows" {
		maxInput = 24000
	}
	return &Service{Timeout: 2 * time.Minute, MaxInputChars: maxInput, MaxOutputBytes: 2 << 20}
}

func (s *Service) Providers() []ProviderStatus {
	definitions := []struct{ id, name, binary string }{{"kimi", "KIMI", "kimi"}, {"codex", "Codex", "codex"}, {"claude", "Claude", "claude"}}
	out := make([]ProviderStatus, 0, len(definitions))
	for _, definition := range definitions {
		path, err := exec.LookPath(definition.binary)
		status := ProviderStatus{ID: definition.id, Name: definition.name, Kind: "cli", Transport: "cli", Available: err == nil, Configured: err == nil, Enabled: true, Binary: path}
		if err != nil {
			status.Reason = definition.binary + " executable not found"
		}
		out = append(out, status)
	}
	return out
}

func (s *Service) AllProviders(ctx context.Context) ([]ProviderStatus, error) {
	providers := s.Providers()
	if s.Configs == nil {
		return providers, nil
	}
	configured, err := s.Configs.ListAIProviders(ctx)
	if err != nil {
		return nil, err
	}
	for _, config := range configured {
		status := ProviderStatus{ID: config.ID, Name: config.Name, Kind: config.Kind, Transport: "api", Configured: true, Enabled: config.Enabled, BaseURL: config.BaseURL, Model: config.Model, APIPath: config.APIPath, APIKeyHeader: config.APIKeyHeader, APIKeyPrefix: config.APIKeyPrefix, Headers: config.Headers, TimeoutSeconds: config.TimeoutSeconds, RequiresSecret: ProviderRequiresSecret(config)}
		if strings.HasPrefix(config.SecretRef, "env:") {
			status.SecretSource = "environment"
		} else if strings.HasPrefix(config.SecretRef, "keyring:") {
			status.SecretSource = "keyring"
		}
		if !config.Enabled {
			status.Reason = "provider is disabled"
			providers = append(providers, status)
			continue
		}
		if _, err = NormalizeProviderConfig(config); err != nil {
			status.Reason = err.Error()
			providers = append(providers, status)
			continue
		}
		status.SecretConfigured = !status.RequiresSecret
		if config.SecretRef != "" && s.Secrets != nil {
			if secret, secretErr := s.Secrets.Get(ctx, config.SecretRef); secretErr == nil && secret != "" {
				status.SecretConfigured = true
			} else if secretErr != nil {
				status.Reason = secretErr.Error()
			}
		}
		status.Available = status.SecretConfigured
		if !status.Available && status.Reason == "" {
			status.Reason = "provider credential is not configured"
		}
		providers = append(providers, status)
	}
	return providers, nil
}

func (s *Service) GenerateJSON(ctx context.Context, provider, purpose, prompt string, result any) (GenerationMeta, error) {
	if s == nil {
		return GenerationMeta{}, errors.New("AI service is not configured")
	}
	if !s.acquire() {
		return GenerationMeta{}, errors.New("AI generation capacity is busy; retry after the active request completes")
	}
	defer s.release()
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	maxInput := s.MaxInputChars
	if maxInput <= 0 {
		maxInput = 24000
		if runtime.GOOS != "windows" {
			maxInput = 120000
		}
	}
	maxOutput := s.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = 2 << 20
	}
	prompt, redactions := Redact(prompt)
	inputChars := utf8.RuneCountInString(prompt)
	if inputChars > maxInput {
		return GenerationMeta{}, fmt.Errorf("AI %s input exceeds %d characters", purpose, maxInput)
	}
	if s.Configs != nil {
		config, configErr := s.Configs.GetAIProvider(ctx, provider)
		if configErr == nil {
			started := time.Now()
			output, err := s.generateHTTP(ctx, config, prompt, maxOutput)
			if err != nil {
				return GenerationMeta{}, err
			}
			if err = ExtractJSONObject(output, result); err != nil {
				return GenerationMeta{}, fmt.Errorf("parse %s output: %w", provider, err)
			}
			return GenerationMeta{Provider: provider, GeneratedAt: time.Now().UTC().Format(time.RFC3339), RedactionCount: redactions, InputChars: inputChars, OutputBytes: len(output), DurationMilliseconds: time.Since(started).Milliseconds(), CostStatus: "not_reported_by_provider"}, nil
		}
		if !errors.Is(configErr, sql.ErrNoRows) {
			return GenerationMeta{}, fmt.Errorf("load AI provider: %w", configErr)
		}
	}
	binary, args, err := providerCommand(provider, prompt)
	if err != nil {
		return GenerationMeta{}, err
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return GenerationMeta{}, fmt.Errorf("%s executable not found", binary)
	}
	workingDir, err := os.MkdirTemp("", "devcycle-ai-*")
	if err != nil {
		return GenerationMeta{}, err
	}
	defer os.RemoveAll(workingDir)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, path, args...)
	command.Dir = workingDir
	prepareManagedCommand(command)
	output := &limitedBuffer{maximum: maxOutput}
	command.Stdout = output
	command.Stderr = output
	started := time.Now()
	if err := command.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return GenerationMeta{}, fmt.Errorf("%s generation timed out after %s", provider, timeout)
		}
		return GenerationMeta{}, fmt.Errorf("%s generation failed: %w: %s", provider, err, truncate(output.String(), 2000))
	}
	if output.exceeded {
		return GenerationMeta{}, fmt.Errorf("%s output exceeded %d bytes", provider, maxOutput)
	}
	if err := ExtractJSONObject(output.String(), result); err != nil {
		return GenerationMeta{}, fmt.Errorf("parse %s output: %w", provider, err)
	}
	return GenerationMeta{Provider: provider, GeneratedAt: time.Now().UTC().Format(time.RFC3339), RedactionCount: redactions, InputChars: inputChars, OutputBytes: output.Len(), DurationMilliseconds: time.Since(started).Milliseconds(), CostStatus: "not_reported_by_cli"}, nil
}

func (s *Service) acquire() bool {
	s.mu.Lock()
	if s.slots == nil {
		s.slots = make(chan struct{}, 2)
	}
	slots := s.slots
	s.mu.Unlock()
	select {
	case slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Service) release() {
	s.mu.Lock()
	slots := s.slots
	s.mu.Unlock()
	<-slots
}

func providerCommand(provider, prompt string) (string, []string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "kimi":
		return "kimi", []string{"-p", prompt}, nil
	case "codex":
		return "codex", []string{"exec", "--sandbox", "read-only", prompt}, nil
	case "claude":
		return "claude", []string{"-p", prompt}, nil
	default:
		return "", nil, errors.New("provider must be kimi, codex, or claude")
	}
}

var (
	privateKeyPattern = regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	secretPattern     = regexp.MustCompile(`(?i)(password|passwd|token|secret|api[_-]?key)(\s*[=:]\s*|\s+)([^\s,;"']+)`)
	urlSecretPattern  = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^:/\s]+:)[^@/\s]+(@)`)
)

func Redact(input string) (string, int) {
	count := 0
	input = privateKeyPattern.ReplaceAllStringFunc(input, func(string) string {
		count++
		return "[REDACTED PRIVATE KEY]"
	})
	input = secretPattern.ReplaceAllStringFunc(input, func(value string) string {
		count++
		match := secretPattern.FindStringSubmatch(value)
		return match[1] + match[2] + "[REDACTED]"
	})
	input = urlSecretPattern.ReplaceAllStringFunc(input, func(value string) string {
		count++
		match := urlSecretPattern.FindStringSubmatch(value)
		return match[1] + "[REDACTED]" + match[2]
	})
	return input, count
}

func ExtractJSONObject(output string, result any) error {
	trimmed := strings.TrimSpace(output)
	if candidateDecodes(trimmed, result) {
		return decodeStrictJSON(trimmed, result)
	}
	best := ""
	for start := 0; start < len(output); start++ {
		if output[start] != '{' {
			continue
		}
		depth, inString, escaped := 0, false, false
	searchCandidate:
		for end := start; end < len(output); end++ {
			character := output[end]
			if inString {
				if escaped {
					escaped = false
				} else if character == '\\' {
					escaped = true
				} else if character == '"' {
					inString = false
				}
				continue
			}
			switch character {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					candidate := output[start : end+1]
					if len(candidate) > len(best) && json.Valid([]byte(candidate)) && candidateDecodes(candidate, result) {
						best = candidate
					}
					break searchCandidate
				}
			}
		}
	}
	if best != "" {
		return decodeStrictJSON(best, result)
	}
	return errors.New("response did not contain a valid JSON object")
}

func candidateDecodes(value string, result any) bool {
	target := reflect.ValueOf(result)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return false
	}
	fresh := reflect.New(target.Elem().Type()).Interface()
	return decodeStrictJSON(value, fresh) == nil
}

func decodeStrictJSON(value string, result any) error {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("response contains trailing JSON")
		}
		return err
	}
	return nil
}

type limitedBuffer struct {
	bytes.Buffer
	maximum  int
	exceeded bool
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	remaining := buffer.maximum - buffer.Len()
	if remaining <= 0 {
		buffer.exceeded = true
		return len(data), nil
	}
	if len(data) > remaining {
		buffer.exceeded = true
		_, _ = buffer.Buffer.Write(data[:remaining])
		return len(data), nil
	}
	return buffer.Buffer.Write(data)
}

func truncate(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum] + " [truncated]"
}
