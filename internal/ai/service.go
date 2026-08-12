package ai

import (
	"bytes"
	"context"
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
)

type ProviderStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Binary    string `json:"-"`
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
		out = append(out, ProviderStatus{ID: definition.id, Name: definition.name, Available: err == nil, Binary: path})
	}
	return out
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
