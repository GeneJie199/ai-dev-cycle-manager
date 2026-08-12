package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStructuredDiffTracksRenameAndLineCounts(t *testing.T) {
	repo := initTempRepo(t)
	readme := filepath.Join(repo, "README.md")
	if err := os.Rename(readme, filepath.Join(repo, "GUIDE.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}

	result, err := NewClient(repo).StructuredDiff(context.Background(), DiffOptions{Staged: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 2 {
		t.Fatalf("files=%+v", result.Files)
	}
	file := result.Files[0]
	if file.Path != "GUIDE.md" || file.OldPath != "README.md" || file.Status[0] != 'R' {
		t.Fatalf("file=%+v", file)
	}
	if result.Files[1].Path != "notes.txt" || result.Files[1].Additions != 2 || result.TotalAdditions != 2 {
		t.Fatalf("line counts=%+v", result)
	}
}

func TestDiffRejectsOptionLikeRevision(t *testing.T) {
	client := NewClient(initTempRepo(t))
	if _, err := client.Diff(context.Background(), DiffOptions{From: "--output=bad"}); err == nil {
		t.Fatal("expected invalid revision error")
	}
	if _, err := client.StructuredDiff(context.Background(), DiffOptions{To: "HEAD"}); err == nil {
		t.Fatal("expected missing from error")
	}
}

func TestParseStructuredDiffWithSpacesAndBinary(t *testing.T) {
	files, err := parseStructuredDiff("M\x00path with spaces.bin\x00", "-\t-\tpath with spaces.bin\x00")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !files[0].Binary || files[0].Path != "path with spaces.bin" {
		t.Fatalf("files=%+v", files)
	}
}

func TestStructuredDiffIncludesUntrackedFiles(t *testing.T) {
	repo := initTempRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "new file.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := NewClient(repo).StructuredDiff(context.Background(), DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Files[0].Path != "new file.txt" || !result.Files[0].Untracked || result.Files[0].Status != "??" {
		t.Fatalf("files=%+v", result.Files)
	}
}
