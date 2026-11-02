package ai

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The helper is a local stand-in, never the installed authenticated Codex CLI.
func TestMain(m *testing.M) {
	if os.Getenv("DEVCYCLE_CLI_PROBE_FIXTURE") == "1" {
		args := os.Args[1:]
		joined := " " + strings.Join(args, " ") + " "
		if len(args) < 2 || args[0] != "exec" || !strings.Contains(joined, " --skip-git-repo-check ") || !strings.Contains(joined, " --sandbox read-only ") || !strings.Contains(joined, " --ephemeral ") || !strings.Contains(joined, " --model fixture-model ") || args[len(args)-2] != "--" || args[len(args)-1] != "--fixture prompt" {
			fmt.Fprintln(os.Stderr, "fixture rejected unsafe or incomplete probe arguments")
			os.Exit(2)
		}
		if _, err := os.Stat(".git"); !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "fixture did not execute in a fresh non-Git directory")
			os.Exit(2)
		}
		fmt.Println(`{"ok":true}`)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCodexProbeRunsReadOnlyInDisposableNonGitDirectory(t *testing.T) {
	fixtureDir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "codex"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(filepath.Join(fixtureDir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal(copyErr, closeErr)
	}
	t.Setenv("PATH", fixtureDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DEVCYCLE_CLI_PROBE_FIXTURE", "1")
	var response struct {
		OK bool `json:"ok"`
	}
	if _, err := NewService().GenerateJSON(WithCLIModel(context.Background(), "fixture-model"), "codex", "connection fixture", "--fixture prompt", &response); err != nil || !response.OK {
		t.Fatalf("non-Git CLI fixture failed: result=%+v error=%v", response, err)
	}
}
