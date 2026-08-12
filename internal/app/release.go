package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	devgit "github.com/GeneJie199/ai-dev-cycle-manager/internal/git"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

// ReleaseCandidateSpec identifies the export format (lifecycle-spec style).
const ReleaseCandidateSpec = "lifecycle-spec/release-candidate/v1"

// ReleaseCandidate is a lifecycle-spec style export of one requirement:
// what was asked, how it is verified, and the work items that implement it.
type ReleaseCandidate struct {
	Spec               string                       `json:"spec"`
	Kind               string                       `json:"kind"`
	Generated          time.Time                    `json:"generatedAt"`
	Requirement        models.Requirement           `json:"requirement"`
	AcceptanceCriteria []models.AcceptanceCriterion `json:"acceptanceCriteria"`
	Tasks              []models.Task                `json:"tasks"`
	Evidence           []models.Evidence            `json:"evidence"`
	Sources            []SourceSnapshot             `json:"sources"`
	Readiness          Readiness                    `json:"readiness"`
}

// SourceSnapshot ties one task worktree to an exact Git commit.
type SourceSnapshot struct {
	TaskID         string              `json:"taskId"`
	TaskTitle      string              `json:"taskTitle"`
	RepositoryPath string              `json:"repositoryPath"`
	WorktreePath   string              `json:"worktreePath"`
	Branch         string              `json:"branch"`
	HeadCommit     string              `json:"headCommit"`
	Clean          bool                `json:"clean"`
	DirtyFiles     []devgit.FileStatus `json:"dirtyFiles"`
	CapturedAt     time.Time           `json:"capturedAt"`
}

// Readiness summarizes whether the release candidate is ready to ship.
type Readiness struct {
	CriteriaTotal        int  `json:"criteriaTotal"`
	CriteriaSatisfied    int  `json:"criteriaSatisfied"`
	TasksTotal           int  `json:"tasksTotal"`
	TasksDone            int  `json:"tasksDone"`
	CriteriaWithEvidence int  `json:"criteriaWithEvidence"`
	SourcesTotal         int  `json:"sourcesTotal"`
	SourcesClean         int  `json:"sourcesClean"`
	Ready                bool `json:"ready"`
}

// ListCriteriaByRequirement lists acceptance criteria for a requirement.
func (a *App) ListCriteriaByRequirement(ctx context.Context, requirementID string) ([]models.AcceptanceCriterion, error) {
	return a.Store.ListCriteriaByRequirement(ctx, requirementID)
}

// ListTasksByRequirement lists tasks for a requirement.
func (a *App) ListTasksByRequirement(ctx context.Context, requirementID string) ([]models.Task, error) {
	return a.Store.ListTasksByRequirement(ctx, requirementID)
}

// UpdateTaskStatus moves a task to a new lifecycle status.
func (a *App) UpdateTaskStatus(ctx context.Context, taskID string, status models.TaskStatus) (models.Task, error) {
	task, err := a.Store.GetTask(ctx, taskID)
	if err != nil {
		return task, err
	}
	return a.UpdateTask(ctx, taskID, task.Title, task.Description, status, task.DependsOn)
}

// UpdateTask validates and atomically persists editable task fields and dependency edges.
func (a *App) UpdateTask(ctx context.Context, taskID, title, description string, status models.TaskStatus, dependsOn []string) (models.Task, error) {
	switch status {
	case models.TaskStatusTodo, models.TaskStatusInProgress, models.TaskStatusDone:
	default:
		return models.Task{}, fmt.Errorf("invalid task status: %q", status)
	}
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(title) < 2 || utf8.RuneCountInString(title) > 300 {
		return models.Task{}, validationf("task title must contain 2 to 300 characters")
	}
	if utf8.RuneCountInString(description) > 8000 {
		return models.Task{}, validationf("task description must not exceed 8000 characters")
	}
	task, err := a.Store.GetTask(ctx, taskID)
	if err != nil {
		return task, err
	}
	all, err := a.Store.ListTasksByRequirement(ctx, task.RequirementID)
	if err != nil {
		return task, err
	}
	byID := make(map[string]models.Task, len(all))
	for _, item := range all {
		byID[item.ID] = item
	}
	unique := map[string]bool{}
	normalized := make([]string, 0, len(dependsOn))
	for _, dependencyID := range dependsOn {
		dependencyID = strings.TrimSpace(dependencyID)
		if dependencyID == "" || unique[dependencyID] {
			continue
		}
		if dependencyID == taskID {
			return task, validationf("task cannot depend on itself")
		}
		dependency, exists := byID[dependencyID]
		if !exists || dependency.RequirementID != task.RequirementID {
			return task, validationf("dependency %s does not belong to this requirement", dependencyID)
		}
		unique[dependencyID] = true
		normalized = append(normalized, dependencyID)
	}
	task.DependsOn = normalized
	byID[task.ID] = task
	if taskGraphHasCycle(byID) {
		return task, validationf("task dependencies contain a cycle")
	}
	if status == models.TaskStatusInProgress || status == models.TaskStatusDone {
		for _, dependencyID := range normalized {
			if byID[dependencyID].Status != models.TaskStatusDone {
				return task, validationf("complete prerequisite task %q before changing this task to %s", byID[dependencyID].Title, status)
			}
		}
	}
	if task.Status == models.TaskStatusDone && status != models.TaskStatusDone {
		for _, dependent := range all {
			if dependent.Status == models.TaskStatusTodo {
				continue
			}
			for _, dependencyID := range dependent.DependsOn {
				if dependencyID == task.ID {
					return task, validationf("task is required by active or completed task %q", dependent.Title)
				}
			}
		}
	}
	if status == models.TaskStatusDone {
		if err = a.ensureTaskSessionsStopped(ctx, task.ID); err != nil {
			return task, err
		}
	}
	task.Title = title
	task.Description = description
	task.Status = status
	return a.Store.UpdateTaskWithDependencies(ctx, task)
}

func taskGraphHasCycle(tasks map[string]models.Task) bool {
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return true
		}
		if visited[id] {
			return false
		}
		visiting[id] = true
		for _, dependency := range tasks[id].DependsOn {
			if visit(dependency) {
				return true
			}
		}
		visiting[id] = false
		visited[id] = true
		return false
	}
	for id := range tasks {
		if visit(id) {
			return true
		}
	}
	return false
}

// UpdateCriterionResult records whether an acceptance criterion is satisfied.
func (a *App) UpdateCriterionResult(ctx context.Context, criterionID string, satisfied bool) (models.AcceptanceCriterion, error) {
	c, err := a.Store.GetCriterion(ctx, criterionID)
	if err != nil {
		return c, err
	}
	if satisfied {
		latest, err := a.Store.LatestEvidenceForCriterion(ctx, criterionID)
		if errors.Is(err, sql.ErrNoRows) {
			return c, fmt.Errorf("acceptance criterion requires passing evidence before it can be satisfied")
		}
		if err != nil {
			return c, err
		}
		if latest.Status != "passed" {
			return c, fmt.Errorf("latest acceptance evidence is %s; a newer passing result is required", latest.Status)
		}
	}
	return a.Store.UpdateCriterion(ctx, criterionID, c.Description, satisfied)
}

// ExportReleaseCandidate builds a lifecycle-spec style release candidate JSON
// document for a requirement from the store.
func (a *App) ExportReleaseCandidate(ctx context.Context, requirementID string) (ReleaseCandidate, error) {
	req, err := a.Store.GetRequirement(ctx, requirementID)
	if err != nil {
		return ReleaseCandidate{}, fmt.Errorf("requirement: %w", err)
	}
	criteria, err := a.Store.ListCriteriaByRequirement(ctx, requirementID)
	if err != nil {
		return ReleaseCandidate{}, fmt.Errorf("criteria: %w", err)
	}
	tasks, err := a.Store.ListTasksByRequirement(ctx, requirementID)
	if err != nil {
		return ReleaseCandidate{}, fmt.Errorf("tasks: %w", err)
	}
	evidence, err := a.Store.ListEvidence(ctx, requirementID)
	if err != nil {
		return ReleaseCandidate{}, fmt.Errorf("evidence: %w", err)
	}
	if criteria == nil {
		criteria = []models.AcceptanceCriterion{}
	}
	if tasks == nil {
		tasks = []models.Task{}
	}

	r := Readiness{CriteriaTotal: len(criteria), TasksTotal: len(tasks)}
	latestEvidence := map[string]models.Evidence{}
	for _, item := range evidence {
		if item.CriterionID != "" {
			if _, exists := latestEvidence[item.CriterionID]; !exists {
				latestEvidence[item.CriterionID] = item
			}
		}
	}
	for _, c := range criteria {
		if c.Satisfied {
			r.CriteriaSatisfied++
		}
		if latest, exists := latestEvidence[c.ID]; exists && latest.Status == "passed" {
			r.CriteriaWithEvidence++
		}
	}
	for _, t := range tasks {
		if t.Status == models.TaskStatusDone {
			r.TasksDone++
		}
	}
	sources := []SourceSnapshot{}
	for _, task := range tasks {
		if task.WorktreePath == "" {
			continue
		}
		state, sourceErr := devgit.InspectSource(ctx, a.Git, task.WorktreePath)
		if sourceErr != nil {
			return ReleaseCandidate{}, fmt.Errorf("task %q source: %w", task.Title, sourceErr)
		}
		source := SourceSnapshot{TaskID: task.ID, TaskTitle: task.Title, RepositoryPath: state.RepositoryPath, WorktreePath: state.WorktreePath, Branch: state.Branch, HeadCommit: state.HeadCommit, Clean: state.Clean, DirtyFiles: state.Files, CapturedAt: time.Now().UTC()}
		sources = append(sources, source)
		if source.Clean {
			r.SourcesClean++
		}
	}
	r.SourcesTotal = len(sources)
	r.Ready = r.CriteriaTotal > 0 && r.CriteriaSatisfied == r.CriteriaTotal && r.CriteriaWithEvidence == r.CriteriaTotal && r.TasksDone == r.TasksTotal && r.SourcesClean == r.SourcesTotal

	return ReleaseCandidate{
		Spec:               ReleaseCandidateSpec,
		Kind:               "release-candidate",
		Generated:          time.Now().UTC().Truncate(time.Second),
		Requirement:        req,
		AcceptanceCriteria: criteria,
		Tasks:              tasks,
		Evidence:           evidence,
		Sources:            sources,
		Readiness:          r,
	}, nil
}
