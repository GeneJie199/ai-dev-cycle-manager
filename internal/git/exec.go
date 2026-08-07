package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes git commands. Tests may substitute a fake runner; production uses CLIRunner.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (stdout string, stderr string, err error)
}

// CLIRunner invokes the system git binary.
type CLIRunner struct {
	GitPath string
}

// NewCLIRunner returns a runner that uses `git` from PATH (or GitPath if set).
func NewCLIRunner() *CLIRunner {
	return &CLIRunner{GitPath: "git"}
}

// Run executes git with args in dir.
func (r *CLIRunner) Run(ctx context.Context, dir string, args ...string) (string, string, error) {
	bin := r.GitPath
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimRight(stdout.String(), "\r\n")
	errOut := strings.TrimRight(stderr.String(), "\r\n")
	if err != nil {
		if errOut != "" {
			return out, errOut, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, errOut)
		}
		return out, errOut, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, errOut, nil
}

// Client wraps git CLI operations against a repository path.
type Client struct {
	Path   string
	Runner Runner
}

// NewClient creates a git client for path using the system git CLI.
func NewClient(path string) *Client {
	return &Client{Path: path, Runner: NewCLIRunner()}
}

func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	out, _, err := c.Runner.Run(ctx, c.Path, args...)
	return out, err
}
