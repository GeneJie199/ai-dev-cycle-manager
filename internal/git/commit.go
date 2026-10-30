package git

import (
	"context"
	"errors"
	"strings"
)

// CommitTaskChanges records a policy-validated task change set in its existing worktree.
// It does not merge, amend, reset, force-push or remove a worktree.
func (c *Client) CommitTaskChanges(ctx context.Context, message string, files []string) error {
	if strings.TrimSpace(message) == "" || len(files) == 0 {
		return errors.New("commit message and validated task paths required")
	}
	if _, err := c.run(ctx, append([]string{"add", "--all", "--"}, files...)...); err != nil {
		return err
	}
	_, err := c.run(ctx, append([]string{"commit", "-m", message, "--only", "--"}, files...)...)
	return err
}
