// Package devcycle re-exports backend types that a Wails/React host may import.
//
// Prefer binding methods on internal/app.App from the Wails main package.
// This package exists so external modules have a stable import path for shared DTOs.
package devcycle

import (
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/agent"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/git"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

// Domain aliases for hosts that should not import internal/ directly in the future.
// Note: Go's internal/ visibility still applies within this module; Wails main
// typically lives in this same module and can bind app.App directly.

type (
	Requirement         = models.Requirement
	AcceptanceCriterion = models.AcceptanceCriterion
	Task                = models.Task
	TaskStatus          = models.TaskStatus
	Repository          = models.Repository

	StatusResult = git.StatusResult
	BranchInfo   = git.BranchInfo
	CommitInfo   = git.CommitInfo
	WorktreeInfo = git.WorktreeInfo
	DiffResult   = git.DiffResult
	DiffOptions  = git.DiffOptions

	CodexAdapter = agent.CodexAdapter
	StartOptions = agent.StartOptions
	Session      = agent.Session
	StatusInfo   = agent.StatusInfo
)
