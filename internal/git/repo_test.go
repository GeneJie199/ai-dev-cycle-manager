package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestImportRepo_Success(t *testing.T) {
	dir := initTempRepo(t)
	res, err := ImportRepo(context.Background(), NewCLIRunner(), dir)
	if err != nil {
		t.Fatalf("ImportRepo: %v", err)
	}
	if !res.IsGitRepo {
		t.Fatal("expected IsGitRepo")
	}
	if res.Path == "" {
		t.Fatal("expected path")
	}
	if res.Name != filepath.Base(res.Path) {
		t.Fatalf("name=%q want base of %q", res.Name, res.Path)
	}
}

func TestImportRepo_NotGit(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	_, err := ImportRepo(context.Background(), NewCLIRunner(), dir)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestValidateRepo_MissingPath(t *testing.T) {
	requireGit(t)
	err := ValidateRepo(context.Background(), NewCLIRunner(), filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateRepo_FileNotDir(t *testing.T) {
	requireGit(t)
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ValidateRepo(context.Background(), NewCLIRunner(), f)
	if err == nil {
		t.Fatal("expected error for file path")
	}
}
