package models

import "time"

const (
	AgentAdapterCodex    = "codex"
	AgentAdapterClaude   = "claude"
	AgentAdapterGemini   = "gemini"
	AgentAdapterKimi     = "kimi"
	AgentAdapterOpenCode = "opencode"
)

// AgentAdapterConfig describes a directly executed coding-agent CLI. It never
// contains environment variables or credential values.
type AgentAdapterConfig struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	Command      string    `json:"command"`
	Args         []string  `json:"args"`
	Capabilities []string  `json:"capabilities"`
	Enabled      bool      `json:"enabled"`
	BuiltIn      bool      `json:"builtIn"`
	CreatedAt    time.Time `json:"createdAt,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}

// AgentAdapterStatus adds local availability without persisting machine paths.
type AgentAdapterStatus struct {
	AgentAdapterConfig
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

const (
	HandoffStatusOpen     = "open"
	HandoffStatusAccepted = "accepted"
)

// TaskHandoff preserves the context passed between coding-agent sessions.
type TaskHandoff struct {
	ID              string    `json:"id"`
	RequirementID   string    `json:"requirementId"`
	TaskID          string    `json:"taskId"`
	FromSessionID   string    `json:"fromSessionId,omitempty"`
	FromAdapter     string    `json:"fromAdapter,omitempty"`
	ToAdapter       string    `json:"toAdapter"`
	Summary         string    `json:"summary"`
	CompletedWork   []string  `json:"completedWork"`
	RemainingWork   []string  `json:"remainingWork"`
	Risks           []string  `json:"risks"`
	Validation      []string  `json:"validation"`
	ChangedFiles    []string  `json:"changedFiles"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
	AcceptedAt      time.Time `json:"acceptedAt,omitempty"`
	AcceptedSession string    `json:"acceptedSessionId,omitempty"`
}
