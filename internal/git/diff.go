package git

import (
	"context"
	"fmt"
	"strings"
)

// DiffOptions configures git diff.
type DiffOptions struct {
	// Stat requests `git diff --stat` instead of full patch.
	Stat bool
	// Staged limits the diff to the index (cached).
	Staged bool
	// Paths optionally limit the diff to these paths.
	Paths []string
	// From / To are optional revisions (e.g. HEAD~1, main). If only From is set, diffs From vs working tree / To.
	From string
	To   string
}

// DiffResult holds raw diff output suitable for UI display.
type DiffResult struct {
	Stat    bool   `json:"stat"`
	Staged  bool   `json:"staged"`
	Content string `json:"content"`
}

// Diff runs git diff (or git diff --stat) and returns the text output.
func (c *Client) Diff(ctx context.Context, opts DiffOptions) (DiffResult, error) {
	args := []string{"diff"}
	if opts.Stat {
		args = append(args, "--stat")
	}
	if opts.Staged {
		args = append(args, "--cached")
	}
	switch {
	case opts.From != "" && opts.To != "":
		args = append(args, opts.From, opts.To)
	case opts.From != "":
		args = append(args, opts.From)
	}
	if len(opts.Paths) > 0 {
		args = append(args, "--")
		args = append(args, opts.Paths...)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return DiffResult{}, fmt.Errorf("diff: %w", err)
	}
	return DiffResult{
		Stat:    opts.Stat,
		Staged:  opts.Staged,
		Content: out,
	}, nil
}

// DiffStat is a convenience wrapper for `git diff --stat`.
func (c *Client) DiffStat(ctx context.Context) (DiffResult, error) {
	return c.Diff(ctx, DiffOptions{Stat: true})
}

// HasDiff returns true when there is any unstaged or staged change.
func (c *Client) HasDiff(ctx context.Context) (bool, error) {
	unstaged, err := c.Diff(ctx, DiffOptions{})
	if err != nil {
		return false, err
	}
	staged, err := c.Diff(ctx, DiffOptions{Staged: true})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(unstaged.Content) != "" || strings.TrimSpace(staged.Content) != "", nil
}
