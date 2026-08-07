package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// FileStatus is one path from `git status --porcelain`.
type FileStatus struct {
	XY   string `json:"xy"`   // two-letter status code
	Path string `json:"path"`
}

// StatusResult is a summary of working tree status.
type StatusResult struct {
	Branch    string       `json:"branch"`
	Clean     bool         `json:"clean"`
	Files     []FileStatus `json:"files"`
	RawPorcelain string    `json:"rawPorcelain,omitempty"`
}

// BranchInfo describes a local or remote branch.
type BranchInfo struct {
	Name     string `json:"name"`
	Current  bool   `json:"current"`
	Remote   bool   `json:"remote"`
	Upstream string `json:"upstream,omitempty"`
}

// CommitInfo is one entry from `git log`.
type CommitInfo struct {
	Hash      string    `json:"hash"`
	ShortHash string    `json:"shortHash"`
	Subject   string    `json:"subject"`
	Author    string    `json:"author"`
	Email     string    `json:"email"`
	When      time.Time `json:"when"`
}

// Status returns branch and porcelain file status.
func (c *Client) Status(ctx context.Context) (StatusResult, error) {
	branch, err := c.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return StatusResult{}, err
	}
	porcelain, err := c.run(ctx, "status", "--porcelain")
	if err != nil {
		return StatusResult{}, err
	}
	files := parsePorcelain(porcelain)
	return StatusResult{
		Branch:       branch,
		Clean:        len(files) == 0,
		Files:        files,
		RawPorcelain: porcelain,
	}, nil
}

func parsePorcelain(out string) []FileStatus {
	if strings.TrimSpace(out) == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	files := make([]FileStatus, 0, len(lines))
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		xy := line[:2]
		path := strings.TrimSpace(line[3:])
		// rename: "R  old -> new"
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		files = append(files, FileStatus{XY: xy, Path: path})
	}
	return files
}

// Branches lists local branches (and optionally remotes).
func (c *Client) Branches(ctx context.Context, includeRemote bool) ([]BranchInfo, error) {
	args := []string{"branch", "-vv", "--format=%(refname:short)|%(HEAD)|%(upstream:short)|%(objectname:short)"}
	if includeRemote {
		args = []string{"branch", "-a", "-vv", "--format=%(refname:short)|%(HEAD)|%(upstream:short)|%(objectname:short)"}
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var result []BranchInfo
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		current := parts[1] == "*"
		upstream := ""
		if len(parts) > 2 {
			upstream = parts[2]
		}
		remote := strings.HasPrefix(name, "remotes/") || strings.Contains(name, "/")
		// Prefer treating refs/remotes style; git branch -a short names often look like origin/main
		if includeRemote && strings.HasPrefix(name, "origin/") {
			remote = true
		}
		if !includeRemote {
			remote = false
		}
		result = append(result, BranchInfo{
			Name:     name,
			Current:  current,
			Remote:   remote,
			Upstream: upstream,
		})
	}
	return result, nil
}

// Log returns recent commits (max n). Uses a stable custom format for parsing.
func (c *Client) Log(ctx context.Context, n int) ([]CommitInfo, error) {
	if n <= 0 {
		n = 20
	}
	const sep = "\x1f"
	format := strings.Join([]string{"%H", "%h", "%s", "%an", "%ae", "%aI"}, sep)
	out, err := c.run(ctx, "log", "-n", strconv.Itoa(n), "--format="+format)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var commits []CommitInfo
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, sep)
		if len(parts) < 6 {
			continue
		}
		when, err := time.Parse(time.RFC3339, parts[5])
		if err != nil {
			when = time.Time{}
		}
		commits = append(commits, CommitInfo{
			Hash:      parts[0],
			ShortHash: parts[1],
			Subject:   parts[2],
			Author:    parts[3],
			Email:     parts[4],
			When:      when,
		})
	}
	return commits, nil
}

// CurrentBranch returns the current branch name (or HEAD if detached).
func (c *Client) CurrentBranch(ctx context.Context) (string, error) {
	b, err := c.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("current branch: %w", err)
	}
	return b, nil
}
