package app

import (
	"context"
	devai "github.com/GeneJie199/ai-dev-cycle-manager/internal/ai"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/team"
	"strings"
)

type DeliveryView struct {
	ChangedFiles  []string          `json:"changedFiles"`
	Mission       team.Mission      `json:"mission"`
	Summary       string            `json:"summary"`
	Completed     []string          `json:"completed"`
	Incomplete    []string          `json:"incomplete"`
	Risks         []string          `json:"risks"`
	Report        DevelopmentReport `json:"report"`
	ReleaseStatus string            `json:"releaseStatus"`
	PreviewStatus string            `json:"previewStatus"`
	StartGuidance string            `json:"startGuidance"`
	Diffs         map[string]string `json:"diffs"`
}

func (a *App) Delivery(ctx context.Context, id string) (DeliveryView, error) {
	if _, err := a.TeamStore(); err != nil {
		return DeliveryView{}, err
	}
	m, err := a.Team.Get(ctx, id)
	if err != nil {
		return DeliveryView{}, err
	}
	report, err := a.DevelopmentReport(ctx, m.RequirementID)
	if err != nil {
		return DeliveryView{}, err
	}
	v := DeliveryView{ChangedFiles: []string{}, Mission: m, Summary: m.Goal, Report: report, Completed: []string{}, Incomplete: []string{}, Risks: append([]string{}, report.Warnings...), ReleaseStatus: "Not ready: unfinished work or stale/missing machine evidence", PreviewStatus: "Unavailable: this mission did not produce a verified running preview", StartGuidance: "Inspect the delivered task branch and the project's README before starting it. No deployment or start command was inferred or executed.", Diffs: map[string]string{}}
	for _, task := range m.Plan {
		if task.State == "completed" {
			v.Completed = append(v.Completed, task.Role+": "+task.ID)
		} else {
			v.Incomplete = append(v.Incomplete, task.Role+": "+task.State)
		}
		if task.Worktree != "" {
			changed, ce := missionGit(ctx, task.Worktree, "diff", "--no-ext-diff", "--no-textconv", "--name-only", task.Head, "HEAD")
			if ce == nil {
				for _, file := range strings.Split(changed, "\n") {
					if file != "" {
						v.ChangedFiles = append(v.ChangedFiles, task.Role+": "+file)
					}
				}
			}
			diff, e := missionGit(ctx, task.Worktree, "diff", "--no-ext-diff", "--no-textconv", task.Head, "HEAD")
			if e == nil && len(diff) < 256000 {
				diff, _ = devai.Redact(diff)
				v.Diffs[task.ID] = diff
			}
		}
	}
	if len(m.Executions) > 0 && m.Executions[0].Head != "" {
		if source, e := a.missionSource(ctx, m); e == nil {
			if diff, e := missionGit(ctx, source, "diff", "--no-ext-diff", "--no-textconv", m.Executions[0].Head, "HEAD"); e == nil && len(diff) < 256000 {
				diff, _ = devai.Redact(diff)
				v.Diffs["delivery-baseline-to-current"] = diff
			}
		}
	}
	for _, e := range m.Executions {
		if e.Result == "failed" || e.Result == "escalated" {
			v.Risks = append(v.Risks, e.Agent+": "+e.Result+" "+e.Error)
		}
	}
	if m.State == "completed" && report.Candidate.Readiness.Ready && report.EvidenceComplete {
		v.ReleaseStatus = "Release Candidate ready for independent release validation; no production approval or deployment granted"
	}
	if !strings.EqualFold(m.State, "completed") {
		v.Risks = append(v.Risks, "Mission has not completed; partial output is not a successful delivery")
	}
	return v, nil
}
