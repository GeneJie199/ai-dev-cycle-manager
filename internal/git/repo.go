package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ImportResult is returned when a local path is registered as a git repository.
type ImportResult struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	IsGitRepo  bool   `json:"isGitRepo"`
	HeadBranch string `json:"headBranch,omitempty"`
}

// SourceState pins a worktree to the exact commit represented by a release candidate.
type SourceState struct {
	RepositoryPath string       `json:"repositoryPath"`
	WorktreePath   string       `json:"worktreePath"`
	Branch         string       `json:"branch"`
	HeadCommit     string       `json:"headCommit"`
	Clean          bool         `json:"clean"`
	Files          []FileStatus `json:"files"`
}

// InspectSource resolves a worktree and captures its immutable HEAD plus dirty state.
func InspectSource(ctx context.Context, runner Runner, path string) (SourceState, error) {
	if runner == nil {
		runner = NewCLIRunner()
	}
	worktree, err := filepath.Abs(path)
	if err != nil {
		return SourceState{}, fmt.Errorf("resolve worktree: %w", err)
	}
	if err = ValidateRepo(ctx, runner, worktree); err != nil {
		return SourceState{}, err
	}
	top, _, err := runner.Run(ctx, worktree, "rev-parse", "--show-toplevel")
	if err != nil {
		return SourceState{}, fmt.Errorf("show-toplevel: %w", err)
	}
	repositoryPath := top
	if common, _, commonErr := runner.Run(ctx, worktree, "rev-parse", "--path-format=absolute", "--git-common-dir"); commonErr == nil && filepath.Base(common) == ".git" {
		repositoryPath = filepath.Dir(common)
	}
	branch, _, err := runner.Run(ctx, worktree, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return SourceState{}, fmt.Errorf("branch: %w", err)
	}
	head, _, err := runner.Run(ctx, worktree, "rev-parse", "HEAD^{commit}")
	if err != nil {
		return SourceState{}, fmt.Errorf("head commit: %w", err)
	}
	porcelain, _, err := runner.Run(ctx, worktree, "status", "--porcelain")
	if err != nil {
		return SourceState{}, fmt.Errorf("status: %w", err)
	}
	files := parsePorcelain(porcelain)
	return SourceState{RepositoryPath: filepath.Clean(repositoryPath), WorktreePath: filepath.Clean(top), Branch: branch, HeadCommit: head, Clean: len(files) == 0, Files: files}, nil
}

// ValidateRepo checks that path exists and is a git repository via `git rev-parse`.
func ValidateRepo(ctx context.Context, runner Runner, path string) error {
	if runner == nil {
		runner = NewCLIRunner()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", abs)
	}
	out, _, err := runner.Run(ctx, abs, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return fmt.Errorf("not a git repository: %s: %w", abs, err)
	}
	if out != "true" {
		return fmt.Errorf("not a git repository: %s", abs)
	}
	return nil
}

// ImportRepo validates path as a git repo and returns metadata for registration.
// Path is registered by the caller (typically the SQLite store / app service).
func ImportRepo(ctx context.Context, runner Runner, path string) (ImportResult, error) {
	if runner == nil {
		runner = NewCLIRunner()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ImportResult{}, fmt.Errorf("resolve path: %w", err)
	}
	if err := ValidateRepo(ctx, runner, abs); err != nil {
		return ImportResult{}, err
	}
	top, _, err := runner.Run(ctx, abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return ImportResult{}, fmt.Errorf("show-toplevel: %w", err)
	}
	topAbs, err := filepath.Abs(top)
	if err != nil {
		topAbs = top
	}
	branch, _, _ := runner.Run(ctx, topAbs, "rev-parse", "--abbrev-ref", "HEAD")
	name := filepath.Base(topAbs)
	return ImportResult{
		Path:       topAbs,
		Name:       name,
		IsGitRepo:  true,
		HeadBranch: branch,
	}, nil
}
