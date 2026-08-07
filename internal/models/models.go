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
