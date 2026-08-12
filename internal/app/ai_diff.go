package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	devai "github.com/GeneJie199/ai-dev-cycle-manager/internal/ai"
	devgit "github.com/GeneJie199/ai-dev-cycle-manager/internal/git"
)

const maxAIDiffFiles = 100

type AICitedFinding struct {
	Text  string   `json:"text"`
	Paths []string `json:"paths"`
}

type AIDiffExplanation struct {
	Summary           string               `json:"summary"`
	UserImpact        []AICitedFinding     `json:"userImpact"`
	EngineeringImpact []AICitedFinding     `json:"engineeringImpact"`
	Risks             []AICitedFinding     `json:"risks"`
	TestFocus         []AICitedFinding     `json:"testFocus"`
	Uncertainties     []string             `json:"uncertainties"`
	Meta              devai.GenerationMeta `json:"meta"`
}

func (a *App) ExplainGitDiff(ctx context.Context, repoPath string, options devgit.DiffOptions, provider string) (AIDiffExplanation, error) {
	diff, err := a.GitStructuredDiff(ctx, repoPath, options)
	if err != nil {
		return AIDiffExplanation{}, err
	}
	if len(diff.Files) == 0 {
		return AIDiffExplanation{}, errors.New("diff contains no changed files")
	}
	input := diff
	omitted := 0
	if len(input.Files) > maxAIDiffFiles {
		omitted = len(input.Files) - maxAIDiffFiles
		input.Files = input.Files[:maxAIDiffFiles]
	}
	heuristic, err := a.AnalyzeChangesWithOptions(ctx, repoPath, options)
	if err != nil {
		return AIDiffExplanation{}, err
	}
	payload, _ := json.Marshal(map[string]any{"diff": input, "heuristicImpact": heuristic, "omittedFileCount": omitted})
	prompt := `You are a senior software change reviewer. Explain the supplied file-level Git diff metadata for an engineering team.
Constraints:
1. Use only the supplied data. Do not claim implementation details that cannot be inferred from paths, status, and line counts.
2. Every impact, risk, and test-focus finding must cite one or more exact changed paths in its paths array.
3. Put ambiguity caused by the absence of patch contents in uncertainties.
4. Return exactly one JSON object with no markdown or commentary, using this shape:
{"summary":"...","userImpact":[{"text":"...","paths":["path"]}],"engineeringImpact":[{"text":"...","paths":["path"]}],"risks":[{"text":"...","paths":["path"]}],"testFocus":[{"text":"...","paths":["path"]}],"uncertainties":["..."]}
Input:
` + string(payload)
	var raw struct {
		Summary           string           `json:"summary"`
		UserImpact        []AICitedFinding `json:"userImpact"`
		EngineeringImpact []AICitedFinding `json:"engineeringImpact"`
		Risks             []AICitedFinding `json:"risks"`
		TestFocus         []AICitedFinding `json:"testFocus"`
		Uncertainties     []string         `json:"uncertainties"`
	}
	meta, err := a.AI.GenerateJSON(ctx, provider, "diff explanation", prompt, &raw)
	if err != nil {
		return AIDiffExplanation{}, err
	}
	explanation := AIDiffExplanation{Summary: raw.Summary, UserImpact: raw.UserImpact, EngineeringImpact: raw.EngineeringImpact, Risks: raw.Risks, TestFocus: raw.TestFocus, Uncertainties: raw.Uncertainties, Meta: meta}
	if err := validateDiffExplanation(&explanation, diff.Files); err != nil {
		return AIDiffExplanation{}, fmt.Errorf("AI diff explanation failed validation: %w", err)
	}
	return explanation, nil
}

func validateDiffExplanation(explanation *AIDiffExplanation, files []devgit.FileDiff) error {
	explanation.Summary = strings.TrimSpace(explanation.Summary)
	if explanation.Summary == "" || len(explanation.Summary) > 4000 {
		return errors.New("summary is empty or too long")
	}
	changed := make(map[string]bool, len(files)*2)
	for _, file := range files {
		changed[file.Path] = true
		if file.OldPath != "" {
			changed[file.OldPath] = true
		}
	}
	groups := []struct {
		name  string
		items *[]AICitedFinding
	}{{"userImpact", &explanation.UserImpact}, {"engineeringImpact", &explanation.EngineeringImpact}, {"risks", &explanation.Risks}, {"testFocus", &explanation.TestFocus}}
	for _, group := range groups {
		if len(*group.items) > 30 {
			return fmt.Errorf("%s exceeds item limit", group.name)
		}
		for index := range *group.items {
			item := &(*group.items)[index]
			item.Text = strings.TrimSpace(item.Text)
			if item.Text == "" || len(item.Text) > 2000 || len(item.Paths) == 0 || len(item.Paths) > 20 {
				return fmt.Errorf("%s item %d is invalid", group.name, index+1)
			}
			for _, path := range item.Paths {
				if !changed[path] {
					return fmt.Errorf("%s cites unchanged path %q", group.name, path)
				}
			}
		}
	}
	if len(explanation.Uncertainties) > 30 {
		return errors.New("uncertainties exceeds item limit")
	}
	for index := range explanation.Uncertainties {
		explanation.Uncertainties[index] = strings.TrimSpace(explanation.Uncertainties[index])
		if explanation.Uncertainties[index] == "" || len(explanation.Uncertainties[index]) > 2000 {
			return fmt.Errorf("uncertainty %d is invalid", index+1)
		}
	}
	return nil
}
