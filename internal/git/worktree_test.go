package git

import (
	"context"
	"path/filepath"
	"testing"
)

func TestWorktreeCreateListRemove(t *testing.T) {
	dir := initTempRepo(t)
	c := NewClient(dir)
	ctx := context.Background()

	list, err := c.ListWorktrees(ctx)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 worktree (main), got %d", len(list))
	}

	wtPath := filepath.Join(t.TempDir(), "feature-wt")
	info, err := c.AddWorktree(ctx, WorktreeAddOptions{
		Path:         wtPath,
		Branch:       "feature/dev-004",
		CreateBranch: true,
		StartPoint:   "HEAD",
	})
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if info.Path == "" {
		t.Fatal("expected worktree path")
	}
	if info.Branch != "feature/dev-004" {
		t.Fatalf("branch=%q", info.Branch)
	}

	list, err = c.ListWorktrees(ctx)
	if err != nil {
		t.Fatalf("ListWorktrees after add: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 worktrees, got %d: %+v", len(list), list)
	}

	if err := c.RemoveWorktree(ctx, wtPath, true); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	list, err = c.ListWorktrees(ctx)
	if err != nil {
		t.Fatalf("ListWorktrees after remove: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 worktree after remove, got %d", len(list))
	}
}

func TestParseWorktreePorcelain(t *testing.T) {
	in := `worktree /repo
HEAD abcdef
branch refs/heads/main

worktree /repo-feature
HEAD 123456
branch refs/heads/feature
locked
`
	got := parseWorktreePorcelain(in)
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Branch != "main" || got[0].Path != "/repo" {
		t.Fatalf("got[0]=%+v", got[0])
	}
	if got[1].Branch != "feature" || !got[1].Locked {
		t.Fatalf("got[1]=%+v", got[1])
	}
}
