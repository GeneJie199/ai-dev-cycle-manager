// Package models defines domain entities for requirements, tasks, and criteria.
package models

import "time"

// Requirement is a product or engineering need tracked by the app.
type Requirement struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AcceptanceCriterion is a testable condition for a requirement.
type AcceptanceCriterion struct {
	ID            string    `json:"id"`
	RequirementID string    `json:"requirementId"`
	Description   string    `json:"description"`
	Satisfied     bool      `json:"satisfied"`
	CreatedAt     time.Time `json:"createdAt"`
}

// TaskStatus is the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusTodo       TaskStatus = "todo"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusDone       TaskStatus = "done"
)

// Task is a unit of work linked to a requirement and optionally a git branch/worktree.
// Branch = 独立工作版本; WorktreePath = 隔离开发目录.
type Task struct {
	ID            string     `json:"id"`
	RequirementID string     `json:"requirementId"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        TaskStatus `json:"status"`
	DependsOn     []string   `json:"dependsOn"`
	Branch        string     `json:"branch"`       // 独立工作版本
	WorktreePath  string     `json:"worktreePath"` // 隔离开发目录
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// Repository is a locally registered git repository.
type Repository struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// Evidence is a durable proof attached to a requirement and optionally one criterion or task.
type Evidence struct {
	ID            string            `json:"id"`
	RequirementID string            `json:"requirementId"`
	CriterionID   string            `json:"criterionId,omitempty"`
	TaskID        string            `json:"taskId,omitempty"`
	Kind          string            `json:"kind"`
	Title         string            `json:"title"`
	Status        string            `json:"status"`
	URI           string            `json:"uri,omitempty"`
	Inline        string            `json:"inline,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
}

// VerificationRun records one deterministic build, test, lint, or smoke command.
type VerificationRun struct {
	ID                   string    `json:"id"`
	RequirementID        string    `json:"requirementId"`
	CriterionID          string    `json:"criterionId,omitempty"`
	Name                 string    `json:"name"`
	Command              string    `json:"command"`
	WorkingDir           string    `json:"workingDir"`
	Status               string    `json:"status"`
	ExitCode             int       `json:"exitCode"`
	Output               string    `json:"output"`
	StartedAt            time.Time `json:"startedAt"`
	CompletedAt          time.Time `json:"completedAt"`
	EvidenceID           string    `json:"evidenceId"`
	DurationMilliseconds int64     `json:"durationMilliseconds"`
}

// AgentSession links an external coding agent process to a development task.
type AgentSession struct {
	ID                   string    `json:"id"`
	TaskID               string    `json:"taskId"`
	Provider             string    `json:"provider"`
	Prompt               string    `json:"-"`
	WorkingDir           string    `json:"-"`
	Status               string    `json:"status"`
	PID                  int       `json:"pid,omitempty"`
	LogPath              string    `json:"-"`
	StartedAt            time.Time `json:"startedAt"`
	EndedAt              time.Time `json:"endedAt,omitempty"`
	DurationMilliseconds int64     `json:"durationMilliseconds"`
	LogLimitBytes        int64     `json:"logLimitBytes"`
	CostStatus           string    `json:"costStatus"`
}
