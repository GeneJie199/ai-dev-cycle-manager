package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// WorktreeInfo describes one entry from `git worktree list`.
type WorktreeInfo struct {
	Path   string `json:"path"`
	HEAD   string `json:"head"`
	Branch string `json:"branch"` // 独立工作版本; empty if detached
	Bare   bool   `json:"bare"`
	Locked bool   `json:"locked"`
}

// WorktreeAddOptions configures `git worktree add`.
type WorktreeAddOptions struct {
	// Path is the new worktree directory (隔离开发目录).
	Path string
	// Branch is the branch to check out (独立工作版本). Created if CreateBranch is true.
	Branch string
	// CreateBranch creates Branch from StartPoint when true.
	CreateBranch bool
	// StartPoint is the ref to base a new branch on (default HEAD).
	StartPoint string
}

// ListWorktrees returns all worktrees for the repository.
func (c *Client) ListWorktrees(ctx context.Context) ([]WorktreeInfo, error) {
	out, err := c.run(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktreePorcelain(out), nil
}

func parseWorktreePorcelain(out string) []WorktreeInfo {
	var result []WorktreeInfo
	var cur WorktreeInfo
	flush := func() {
		if cur.Path != "" {
			result = append(result, cur)
			cur = WorktreeInfo{}
		}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "bare":
			cur.Bare = true
		case line == "locked" || strings.HasPrefix(line, "locked "):
			cur.Locked = true
		case line == "detached":
			// leave Branch empty
		}
	}
	flush()
	return result
}

// AddWorktree creates a worktree (and optionally a new branch).
func (c *Client) AddWorktree(ctx context.Context, opts WorktreeAddOptions) (WorktreeInfo, error) {
	if opts.Path == "" {
		return WorktreeInfo{}, fmt.Errorf("worktree path is required")
	}
	abs, err := filepath.Abs(opts.Path)
	if err != nil {
		return WorktreeInfo{}, err
	}
	args := []string{"worktree", "add"}
	if opts.CreateBranch {
		if opts.Branch == "" {
			return WorktreeInfo{}, fmt.Errorf("branch name required when CreateBranch is true")
		}
		args = append(args, "-b", opts.Branch)
		args = append(args, abs)
		if opts.StartPoint != "" {
			args = append(args, opts.StartPoint)
		}
	} else if opts.Branch != "" {
		args = append(args, abs, opts.Branch)
	} else {
		args = append(args, abs)
	}
	if _, err := c.run(ctx, args...); err != nil {
		return WorktreeInfo{}, err
	}
	list, err := c.ListWorktrees(ctx)
	if err != nil {
		return WorktreeInfo{Path: abs, Branch: opts.Branch}, nil
	}
	for _, wt := range list {
		if samePath(wt.Path, abs) {
			return wt, nil
		}
	}
	return WorktreeInfo{Path: abs, Branch: opts.Branch}, nil
}

// RemoveWorktree removes a worktree at path. force maps to --force.
func (c *Client) RemoveWorktree(ctx context.Context, path string, force bool) error {
	if path == "" {
		return fmt.Errorf("worktree path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, abs)
	_, err = c.run(ctx, args...)
	return err
}

func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}
