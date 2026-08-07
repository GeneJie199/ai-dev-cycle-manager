// Package agent defines AI agent adapter contracts.
// Implementations that invoke Codex (or other tools) live outside this file;
// only the interface is provided here — no fake "completed" runtime.
package agent

import (
	"context"
	"time"
)

// SessionStatus describes the lifecycle of an agent session.
type SessionStatus string

const (
	SessionStatusUnknown  SessionStatus = "unknown"
	SessionStatusStarting SessionStatus = "starting"
	SessionStatusRunning  SessionStatus = "running"
	SessionStatusStopped  SessionStatus = "stopped"
	SessionStatusFailed   SessionStatus = "failed"
)

// StartOptions configures a Codex CLI session start.
type StartOptions struct {
	// WorkingDir is the directory the agent should operate in (typically a worktree path).
	WorkingDir string
	// Prompt is the initial instruction passed to Codex CLI.
	Prompt string
	// ExtraArgs are additional CLI arguments forwarded to the Codex binary.
	ExtraArgs []string
	// Env are extra environment variables for the process.
	Env map[string]string
}

// Session is a handle for a started Codex CLI process.
type Session struct {
	ID        string
	WorkingDir string
	StartedAt time.Time
}

// StatusInfo is a point-in-time view of a session.
type StatusInfo struct {
	SessionID string
	Status    SessionStatus
	Message   string
	UpdatedAt time.Time
}

// CodexAdapter is the contract for driving the Codex CLI from the app.
// Wails (or other hosts) can inject a concrete implementation later.
// This package intentionally does not ship a production runner that claims
// Codex completed work without actually invoking the CLI.
type CodexAdapter interface {
	// Start launches a Codex CLI session with the given options.
	Start(ctx context.Context, opts StartOptions) (Session, error)
	// Stop terminates a previously started session.
	Stop(ctx context.Context, sessionID string) error
	// Status returns the current status of a session.
	Status(ctx context.Context, sessionID string) (StatusInfo, error)
}
