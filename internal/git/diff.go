package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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

type FileDiff struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary"`
	Untracked bool   `json:"untracked"`
}

type StructuredDiff struct {
	From           string     `json:"from,omitempty"`
	To             string     `json:"to,omitempty"`
	Staged         bool       `json:"staged"`
	Files          []FileDiff `json:"files"`
	TotalAdditions int        `json:"totalAdditions"`
	TotalDeletions int        `json:"totalDeletions"`
	BinaryFiles    int        `json:"binaryFiles"`
	UntrackedFiles int        `json:"untrackedFiles"`
}

// Diff runs git diff (or git diff --stat) and returns the text output.
func (c *Client) Diff(ctx context.Context, opts DiffOptions) (DiffResult, error) {
	args, err := diffArgs(opts)
	if err != nil {
		return DiffResult{}, err
	}
	if opts.Stat {
		args = append(args, "--stat")
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

// StructuredDiff returns bounded file-level metadata without loading patch bodies.
func (c *Client) StructuredDiff(ctx context.Context, opts DiffOptions) (StructuredDiff, error) {
	base, err := diffArgs(opts)
	if err != nil {
		return StructuredDiff{}, err
	}
	nameArgs := append(append([]string{}, base...), "--name-status", "-z")
	numArgs := append(append([]string{}, base...), "--numstat", "-z")
	if len(opts.Paths) > 0 {
		nameArgs = append(nameArgs, "--")
		nameArgs = append(nameArgs, opts.Paths...)
		numArgs = append(numArgs, "--")
		numArgs = append(numArgs, opts.Paths...)
	}
	names, err := c.run(ctx, nameArgs...)
	if err != nil {
		return StructuredDiff{}, fmt.Errorf("structured diff names: %w", err)
	}
	numbers, err := c.run(ctx, numArgs...)
	if err != nil {
		return StructuredDiff{}, fmt.Errorf("structured diff stats: %w", err)
	}
	files, err := parseStructuredDiff(names, numbers)
	if err != nil {
		return StructuredDiff{}, err
	}
	if !opts.Staged && opts.To == "" {
		untrackedArgs := []string{"ls-files", "--others", "--exclude-standard", "-z"}
		if len(opts.Paths) > 0 {
			untrackedArgs = append(untrackedArgs, "--")
			untrackedArgs = append(untrackedArgs, opts.Paths...)
		}
		untracked, runErr := c.run(ctx, untrackedArgs...)
		if runErr != nil {
			return StructuredDiff{}, fmt.Errorf("structured diff untracked files: %w", runErr)
		}
		for _, path := range nulTokens(untracked) {
			files = append(files, FileDiff{Path: path, Status: "??", Untracked: true})
		}
	}
	result := StructuredDiff{From: opts.From, To: opts.To, Staged: opts.Staged, Files: files}
	for _, file := range files {
		result.TotalAdditions += file.Additions
		result.TotalDeletions += file.Deletions
		if file.Binary {
			result.BinaryFiles++
		}
		if file.Untracked {
			result.UntrackedFiles++
		}
	}
	return result, nil
}

func diffArgs(opts DiffOptions) ([]string, error) {
	if opts.To != "" && opts.From == "" {
		return nil, errors.New("from revision is required when to revision is set")
	}
	for _, revision := range []string{opts.From, opts.To} {
		if revision != "" && (strings.HasPrefix(revision, "-") || strings.ContainsRune(revision, 0) || len(revision) > 256) {
			return nil, fmt.Errorf("invalid revision %q", revision)
		}
	}
	args := []string{"diff", "--no-ext-diff", "--no-color"}
	if opts.Staged {
		args = append(args, "--cached")
	}
	if opts.From != "" {
		args = append(args, opts.From)
	}
	if opts.To != "" {
		args = append(args, opts.To)
	}
	return args, nil
}

type numberStat struct {
	additions int
	deletions int
	binary    bool
}

func parseStructuredDiff(names, numbers string) ([]FileDiff, error) {
	nameTokens := nulTokens(names)
	files := make([]FileDiff, 0, len(nameTokens)/2)
	for index := 0; index < len(nameTokens); {
		status := nameTokens[index]
		index++
		if status == "" || index >= len(nameTokens) {
			return nil, errors.New("malformed git name-status output")
		}
		file := FileDiff{Status: status}
		if status[0] == 'R' || status[0] == 'C' {
			if index+1 >= len(nameTokens) {
				return nil, errors.New("malformed git rename output")
			}
			file.OldPath, file.Path = nameTokens[index], nameTokens[index+1]
			index += 2
		} else {
			file.Path = nameTokens[index]
			index++
		}
		files = append(files, file)
	}
	stats, err := parseNumberStats(numbers)
	if err != nil {
		return nil, err
	}
	for index := range files {
		if stat, ok := stats[files[index].Path]; ok {
			files[index].Additions, files[index].Deletions, files[index].Binary = stat.additions, stat.deletions, stat.binary
		}
	}
	return files, nil
}

func parseNumberStats(output string) (map[string]numberStat, error) {
	tokens := nulTokens(output)
	stats := make(map[string]numberStat, len(tokens))
	for index := 0; index < len(tokens); index++ {
		parts := strings.SplitN(tokens[index], "\t", 3)
		if len(parts) != 3 {
			return nil, errors.New("malformed git numstat output")
		}
		stat := numberStat{binary: parts[0] == "-" || parts[1] == "-"}
		if !stat.binary {
			var err error
			stat.additions, err = strconv.Atoi(parts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid additions %q", parts[0])
			}
			stat.deletions, err = strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid deletions %q", parts[1])
			}
		}
		path := parts[2]
		if path == "" {
			if index+2 >= len(tokens) {
				return nil, errors.New("malformed git rename numstat output")
			}
			path = tokens[index+2]
			index += 2
		}
		stats[path] = stat
	}
	return stats, nil
}

func nulTokens(value string) []string {
	if value == "" {
		return []string{}
	}
	tokens := strings.Split(value, "\x00")
	if tokens[len(tokens)-1] == "" {
		tokens = tokens[:len(tokens)-1]
	}
	return tokens
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
