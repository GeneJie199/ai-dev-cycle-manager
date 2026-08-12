// Package app exposes high-level services intended for Wails method binding.
package app

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/agent"
	devai "github.com/GeneJie199/ai-dev-cycle-manager/internal/ai"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/git"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/store"
)

// App is the backend façade. Wails can bind exported methods on this type later.
type App struct {
	Store          *store.Store
	Git            *git.CLIRunner
	Codex          agent.CodexAdapter // optional; nil until a real adapter is injected
	AI             *devai.Service
	DBPath         string
	mu             sync.Mutex
	processes      map[string]*runningAgent
	agentWG        sync.WaitGroup
	verifications  map[string]*runningVerification
	verificationWG sync.WaitGroup
}

type runningAgent struct {
	cmd         *exec.Cmd
	finalStatus string
}

type runningVerification struct {
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	finalStatus string
}

// New opens the store at dbPath and returns an App ready for API calls.
func New(dbPath string) (*App, error) {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(abs)
	if err != nil {
		return nil, err
	}
	if err = st.RecoverRunningAgentSessions(context.Background()); err != nil {
		_ = st.Close()
		return nil, err
	}
	if err = st.RecoverRunningVerificationRuns(context.Background()); err != nil {
		_ = st.Close()
		return nil, err
	}
	return &App{
		Store:         st,
		Git:           git.NewCLIRunner(),
		AI:            devai.NewService(),
		DBPath:        abs,
		processes:     map[string]*runningAgent{},
		verifications: map[string]*runningVerification{},
	}, nil
}

// Close releases resources.
func (a *App) Close() error {
	a.mu.Lock()
	for _, process := range a.processes {
		process.finalStatus = "stopped"
		_ = stopManagedCommand(process.cmd)
	}
	for _, process := range a.verifications {
		process.finalStatus = "stopped"
		process.cancel()
	}
	a.mu.Unlock()
	a.agentWG.Wait()
	a.verificationWG.Wait()
	if a.Store != nil {
		return a.Store.Close()
	}
	return nil
}

// SetCodexAdapter injects a Codex CLI adapter implementation (interface only in-repo).
func (a *App) SetCodexAdapter(c agent.CodexAdapter) {
	a.Codex = c
}

// ImportRepository validates path as a git repo and registers it in SQLite.
func (a *App) ImportRepository(ctx context.Context, path string) (models.Repository, error) {
	res, err := git.ImportRepo(ctx, a.Git, path)
	if err != nil {
		return models.Repository{}, err
	}
	existing, err := a.Store.GetRepositoryByPath(ctx, res.Path)
	if err == nil {
		return existing, nil
	}
	return a.Store.CreateRepository(ctx, res.Path, res.Name)
}

// ListRepositories returns registered local repositories.
func (a *App) ListRepositories(ctx context.Context) ([]models.Repository, error) {
	return a.Store.ListRepositories(ctx)
}

func (a *App) DeleteRepository(ctx context.Context, id string) error {
	return a.Store.DeleteRepository(ctx, id)
}

func (a *App) gitClient(repoPath string) *git.Client {
	c := git.NewClient(repoPath)
	c.Runner = a.Git
	return c
}

// GitStatus returns status for a registered (or absolute) repo path.
func (a *App) GitStatus(ctx context.Context, repoPath string) (git.StatusResult, error) {
	return a.gitClient(repoPath).Status(ctx)
}

// GitBranches lists branches.
func (a *App) GitBranches(ctx context.Context, repoPath string, includeRemote bool) ([]git.BranchInfo, error) {
	return a.gitClient(repoPath).Branches(ctx, includeRemote)
}

// GitLog returns recent commits.
func (a *App) GitLog(ctx context.Context, repoPath string, n int) ([]git.CommitInfo, error) {
	return a.gitClient(repoPath).Log(ctx, n)
}

// GitDiff returns diff or diff --stat text.
func (a *App) GitDiff(ctx context.Context, repoPath string, opts git.DiffOptions) (git.DiffResult, error) {
	return a.gitClient(repoPath).Diff(ctx, opts)
}

func (a *App) GitStructuredDiff(ctx context.Context, repoPath string, opts git.DiffOptions) (git.StructuredDiff, error) {
	return a.gitClient(repoPath).StructuredDiff(ctx, opts)
}

// ListWorktrees lists worktrees (隔离开发目录).
func (a *App) ListWorktrees(ctx context.Context, repoPath string) ([]git.WorktreeInfo, error) {
	return a.gitClient(repoPath).ListWorktrees(ctx)
}

// AddWorktree creates a worktree/branch and optionally links a task.
func (a *App) AddWorktree(ctx context.Context, repoPath string, opts git.WorktreeAddOptions) (git.WorktreeInfo, error) {
	return a.gitClient(repoPath).AddWorktree(ctx, opts)
}

// RemoveWorktree removes a worktree directory registration.
func (a *App) RemoveWorktree(ctx context.Context, repoPath, worktreePath string, force bool) error {
	return a.gitClient(repoPath).RemoveWorktree(ctx, worktreePath, force)
}

// CreateRequirement creates a requirement.
func (a *App) CreateRequirement(ctx context.Context, title, description string) (models.Requirement, error) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(title) < 2 || utf8.RuneCountInString(title) > 300 {
		return models.Requirement{}, validationf("title must contain 2 to 300 characters")
	}
	if utf8.RuneCountInString(description) > 20000 {
		return models.Requirement{}, validationf("description must not exceed 20000 characters")
	}
	return a.Store.CreateRequirement(ctx, title, description)
}

// ListRequirements lists all requirements.
func (a *App) ListRequirements(ctx context.Context) ([]models.Requirement, error) {
	return a.Store.ListRequirements(ctx)
}

func (a *App) UpdateRequirement(ctx context.Context, id, title, description string) (models.Requirement, error) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(title) < 2 || utf8.RuneCountInString(title) > 300 {
		return models.Requirement{}, validationf("title must contain 2 to 300 characters")
	}
	if utf8.RuneCountInString(description) > 20000 {
		return models.Requirement{}, validationf("description must not exceed 20000 characters")
	}
	return a.Store.UpdateRequirement(ctx, id, title, description)
}

func (a *App) DeleteRequirement(ctx context.Context, id string) error {
	tasks, err := a.Store.ListTasksByRequirement(ctx, id)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err = a.ensureTaskSessionsStopped(ctx, task.ID); err != nil {
			return err
		}
	}
	return a.Store.DeleteRequirement(ctx, id)
}

func (a *App) DeleteCriterion(ctx context.Context, id string) error {
	return a.Store.DeleteCriterion(ctx, id)
}

func (a *App) DeleteTask(ctx context.Context, id string) error {
	if err := a.ensureTaskSessionsStopped(ctx, id); err != nil {
		return err
	}
	return a.Store.DeleteTask(ctx, id)
}

func (a *App) ensureTaskSessionsStopped(ctx context.Context, taskID string) error {
	sessions, err := a.Store.ListAgentSessions(ctx, taskID)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if session.Status == "running" {
			return fmt.Errorf("stop running agent session %s before deleting the task", session.ID)
		}
	}
	return nil
}

// CreateCriterion adds an acceptance criterion.
func (a *App) CreateCriterion(ctx context.Context, requirementID, description string) (models.AcceptanceCriterion, error) {
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) < 5 || utf8.RuneCountInString(description) > 2000 {
		return models.AcceptanceCriterion{}, validationf("criterion must contain 5 to 2000 characters")
	}
	if _, err := a.Store.GetRequirement(ctx, requirementID); err != nil {
		return models.AcceptanceCriterion{}, fmt.Errorf("requirement: %w", err)
	}
	return a.Store.CreateCriterion(ctx, requirementID, description)
}

// CreateTask creates a task under a requirement.
func (a *App) CreateTask(ctx context.Context, requirementID, title, description string) (models.Task, error) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(title) < 2 || utf8.RuneCountInString(title) > 300 {
		return models.Task{}, validationf("task title must contain 2 to 300 characters")
	}
	if utf8.RuneCountInString(description) > 8000 {
		return models.Task{}, validationf("task description must not exceed 8000 characters")
	}
	if _, err := a.Store.GetRequirement(ctx, requirementID); err != nil {
		return models.Task{}, fmt.Errorf("requirement: %w", err)
	}
	return a.Store.CreateTask(ctx, requirementID, title, description)
}

// LinkTaskToWorktree creates a branch+worktree and links them on the task.
// Branch = 独立工作版本; Worktree = 隔离开发目录.
func (a *App) LinkTaskToWorktree(ctx context.Context, repoPath, taskID, branch, worktreePath string) (models.Task, git.WorktreeInfo, error) {
	existing, err := a.Store.GetTask(ctx, taskID)
	if err != nil {
		return models.Task{}, git.WorktreeInfo{}, err
	}
	if existing.WorktreePath != "" {
		return models.Task{}, git.WorktreeInfo{}, fmt.Errorf("task already has a linked worktree")
	}
	wt, err := a.AddWorktree(ctx, repoPath, git.WorktreeAddOptions{
		Path:         worktreePath,
		Branch:       branch,
		CreateBranch: true,
		StartPoint:   "HEAD",
	})
	if err != nil {
		return models.Task{}, wt, fmt.Errorf("add worktree: %w", err)
	}
	task, err := a.Store.LinkTaskGit(ctx, taskID, branch, wt.Path)
	if err != nil {
		_ = a.RemoveWorktree(context.Background(), repoPath, wt.Path, true)
		return models.Task{}, wt, fmt.Errorf("link task: %w", err)
	}
	return task, wt, nil
}

// GetTask returns a task by id.
func (a *App) GetTask(ctx context.Context, id string) (models.Task, error) {
	return a.Store.GetTask(ctx, id)
}

// ListTasks lists all tasks.
func (a *App) ListTasks(ctx context.Context) ([]models.Task, error) {
	return a.Store.ListTasks(ctx)
}
