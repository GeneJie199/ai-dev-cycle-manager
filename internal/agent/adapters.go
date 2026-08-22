package agent

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

const PromptPlaceholder = "{{prompt}}"

var adapterIDPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,63}$`)

func BuiltInAdapters() []models.AgentAdapterConfig {
	capabilities := func() []string { return []string{"code_editing", "non_interactive", "worktree"} }
	return []models.AgentAdapterConfig{
		{ID: models.AgentAdapterCodex, Name: "Codex", Description: "OpenAI Codex CLI", Command: "codex", Args: []string{"exec", PromptPlaceholder}, Capabilities: capabilities(), Enabled: true, BuiltIn: true},
		{ID: models.AgentAdapterClaude, Name: "Claude Code", Description: "Anthropic Claude Code CLI", Command: "claude", Args: []string{"-p", PromptPlaceholder}, Capabilities: capabilities(), Enabled: true, BuiltIn: true},
		{ID: models.AgentAdapterGemini, Name: "Gemini CLI", Description: "Google Gemini CLI", Command: "gemini", Args: []string{"-p", PromptPlaceholder}, Capabilities: capabilities(), Enabled: true, BuiltIn: true},
		{ID: models.AgentAdapterKimi, Name: "Kimi Code", Description: "Moonshot Kimi Code CLI", Command: "kimi", Args: []string{"-p", PromptPlaceholder}, Capabilities: capabilities(), Enabled: true, BuiltIn: true},
		{ID: models.AgentAdapterOpenCode, Name: "OpenCode", Description: "OpenCode CLI", Command: "opencode", Args: []string{"run", PromptPlaceholder}, Capabilities: capabilities(), Enabled: true, BuiltIn: true},
	}
}

func IsBuiltIn(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, adapter := range BuiltInAdapters() {
		if adapter.ID == id {
			return true
		}
	}
	return false
}

func BuiltIn(id string) (models.AgentAdapterConfig, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, adapter := range BuiltInAdapters() {
		if adapter.ID == id {
			return adapter, true
		}
	}
	return models.AgentAdapterConfig{}, false
}

func NormalizeAdapterConfig(config models.AgentAdapterConfig) (models.AgentAdapterConfig, error) {
	config.ID = strings.ToLower(strings.TrimSpace(config.ID))
	config.Name = strings.TrimSpace(config.Name)
	config.Description = strings.TrimSpace(config.Description)
	config.Command = strings.TrimSpace(config.Command)
	config.BuiltIn = false
	if !adapterIDPattern.MatchString(config.ID) || IsBuiltIn(config.ID) {
		return config, errors.New("adapter ID must be 2 to 64 safe lowercase characters and must not use a built-in ID")
	}
	if utf8.RuneCountInString(config.Name) < 2 || utf8.RuneCountInString(config.Name) > 100 {
		return config, errors.New("adapter name must contain 2 to 100 characters")
	}
	if utf8.RuneCountInString(config.Description) > 500 {
		return config, errors.New("adapter description must not exceed 500 characters")
	}
	if config.Command == "" || len(config.Command) > 512 || strings.ContainsAny(config.Command, "\x00\r\n") {
		return config, errors.New("adapter command is required and must be a single executable path or name")
	}
	if len(config.Args) == 0 || len(config.Args) > 30 {
		return config, errors.New("adapter must define 1 to 30 process arguments")
	}
	promptSlots := 0
	for index, argument := range config.Args {
		if len(argument) > 4096 || strings.ContainsRune(argument, 0) {
			return config, fmt.Errorf("adapter argument %d is invalid", index+1)
		}
		promptSlots += strings.Count(argument, PromptPlaceholder)
	}
	if promptSlots != 1 {
		return config, fmt.Errorf("adapter arguments must contain exactly one %s placeholder", PromptPlaceholder)
	}
	if len(config.Capabilities) > 20 {
		return config, errors.New("adapter has too many capabilities")
	}
	seen := map[string]bool{}
	normalizedCapabilities := make([]string, 0, len(config.Capabilities))
	for _, capability := range config.Capabilities {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability == "" || len(capability) > 64 || !adapterIDPattern.MatchString("a"+capability) {
			return config, fmt.Errorf("adapter capability %q is invalid", capability)
		}
		if !seen[capability] {
			seen[capability] = true
			normalizedCapabilities = append(normalizedCapabilities, capability)
		}
	}
	config.Capabilities = normalizedCapabilities
	return config, nil
}

// BuildCommand expands only the prompt placeholder and returns an argv vector.
// The caller executes it directly without involving a shell.
func BuildCommand(config models.AgentAdapterConfig, prompt string) (string, []string, error) {
	if !config.BuiltIn {
		var err error
		config, err = NormalizeAdapterConfig(config)
		if err != nil {
			return "", nil, err
		}
	}
	if strings.TrimSpace(prompt) == "" {
		return "", nil, errors.New("prompt is required")
	}
	args := make([]string, len(config.Args))
	for index, argument := range config.Args {
		args[index] = strings.ReplaceAll(argument, PromptPlaceholder, prompt)
	}
	return config.Command, args, nil
}
