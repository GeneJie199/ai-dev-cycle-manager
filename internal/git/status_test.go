package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStatusBranchesLog(t *testing.T) {
	dir := initTempRepo(t)
	c := NewClient(dir)
	ctx := context.Background()

	st, err := c.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Clean {
		t.Fatalf("expected clean status, got %+v", st.Files)
	}
	if st.Branch == "" {
		t.Fatal("expected branch name")
	}

	// dirty working tree
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err = c.Status(ctx)
	if err != nil {
		t.Fatalf("Status dirty: %v", err)
	}
	if st.Clean || len(st.Files) == 0 {
		t.Fatal("expected dirty status")
	}

	branches, err := c.Branches(ctx, false)
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if len(branches) == 0 {
		t.Fatal("expected at least one branch")
	}
	var hasCurrent bool
	for _, b := range branches {
		if b.Current {
			hasCurrent = true
		}
	}
	if !hasCurrent {
		t.Fatal("expected a current branch")
	}

	commits, err := c.Log(ctx, 5)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) == 0 {
		t.Fatal("expected commits")
	}
	if commits[0].Subject != "initial commit" {
		t.Fatalf("subject=%q", commits[0].Subject)
	}
	if commits[0].Hash == "" || commits[0].ShortHash == "" {
		t.Fatal("expected hashes")
	}
}

func TestDiff(t *testing.T) {
	dir := initTempRepo(t)
	c := NewClient(dir)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := c.Diff(ctx, DiffOptions{})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if diff.Content == "" {
		t.Fatal("expected diff content")
	}
	stat, err := c.DiffStat(ctx)
	if err != nil {
		t.Fatalf("DiffStat: %v", err)
	}
	if stat.Content == "" || !stat.Stat {
		t.Fatalf("unexpected stat result: %+v", stat)
	}
	has, err := c.HasDiff(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected HasDiff true")
	}
}

func TestBranchesDoNotTreatSlashedLocalNameAsRemote(t *testing.T) {
	dir := initTempRepo(t)
	client := NewClient(dir)
	if _, err := client.run(context.Background(), "branch", "feature/api-client"); err != nil {
		t.Fatal(err)
	}

	branches, err := client.Branches(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, branch := range branches {
		if branch.Name == "feature/api-client" && branch.Remote {
			t.Fatalf("local branch with slash classified as remote: %+v", branch)
		}
	}
}
