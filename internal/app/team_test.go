package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/team"
)

// The helper is a process adapter fixture, not the real AI mission acceptance.
func TestTeamAgentProcess(t *testing.T) {
	if os.Getenv("DEVCYCLE_TEAM_HELPER") != "1" {
		return
	}
	if os.Getenv("DEVCYCLE_TEAM_FAIL") == "1" {
		os.Exit(1)
	}
	println(`{"review":"pass","findings":[]}`)
	os.Exit(0)
}
func teamAppFixture(t *testing.T) (*App, team.Mission) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "team@example.invalid"}, {"config", "user.name", "Team Test"}} {
		if _, err := missionGit(ctx, repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# team test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-q", "-m", "baseline"}} {
		if _, err := missionGit(ctx, repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(filepath.Join(root, "devcycle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVCYCLE_TEAM_HELPER", "1")
	_, err = a.ConfigureAgentAdapter(ctx, models.AgentAdapterConfig{ID: "team-test", Name: "Process fixture", Command: binary, Args: []string{"-test.run=^TestTeamAgentProcess$", "--", "{{model}}", "{{prompt}}"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	check := "git status --porcelain"
	m := team.Mission{Repository: repo, Goal: "Document the team workflow", Mode: "simple", Checks: []string{check}, Models: []team.Model{{ID: "fixture", Provider: "local", Adapter: "team-test", Capabilities: team.Capabilities, ContextLimit: 100000, Tier: 2, Available: true, Tools: true}}, Policy: team.Policy{TrustedLocalCLI: true, AllowedProviders: []string{"local"}}, Budget: team.Budget{MaxExecutions: 10, TimeoutSeconds: 10}}
	return a, m
}
func TestTeamMissionProcessIntegration(t *testing.T) {
	a, input := teamAppFixture(t)
	ctx := context.Background()
	m, err := a.CreateMission(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5 && m.State != "completed"; i++ {
		m, err = a.AdvanceMission(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if m.State != "completed" || len(m.Executions) != 4 {
		t.Fatalf("mission state=%s executions=%+v", m.State, m.Executions)
	}
	worktrees := map[string]bool{}
	for _, task := range m.Plan {
		if worktrees[task.Worktree] {
			t.Fatal("worktree shared")
		}
		worktrees[task.Worktree] = true
	}
	for _, e := range m.Executions {
		if e.Result != "completed" || e.SessionID == "" || len(e.EvidenceIDs) == 0 || e.Tokens != nil || e.Cost != nil {
			t.Fatalf("missing proof or fabricated usage: %+v", e)
		}
	}
}
func TestTeamAgentFailureAndRecovery(t *testing.T) {
	a, input := teamAppFixture(t)
	ctx := context.Background()
	t.Setenv("DEVCYCLE_TEAM_FAIL", "1")
	m, err := a.CreateMission(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	m, err = a.AdvanceMission(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Plan[0].State != "retry" || m.Executions[0].Result != "failed" {
		t.Fatal(m)
	}
	m.Plan[0].State = "running"
	m.Executions[0].Result = "running"
	m.Executions[0].SessionID = "missing"
	m.Executions[0].Started = time.Now()
	if err = a.Team.Save(ctx, &m); err != nil {
		t.Fatal(err)
	}
	m, err = a.AdvanceMission(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Plan[0].State == "completed" || m.Executions[0].Result != "failed" {
		t.Fatal("recovery fabricated completion")
	}
}
func TestTeamRiskAndConsentFailClosed(t *testing.T) {
	a, input := teamAppFixture(t)
	ctx := context.Background()
	input.Policy.TrustedLocalCLI = false
	if _, err := a.CreateMission(ctx, input); err == nil {
		t.Fatal("implicit shell permission")
	}
	input.Policy.TrustedLocalCLI = true
	input.Policy.RequiredEnforcement = []string{"no_network"}
	if _, err := a.CreateMission(ctx, input); err == nil {
		t.Fatal("prompt-only sandbox accepted")
	}
	input.Policy.RequiredEnforcement = nil
	input.Goal = "Modify production credentials"
	m, err := a.CreateMission(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	m, err = a.AdvanceMission(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.State != "escalated" || len(m.Executions) != 0 {
		t.Fatal("R4 ran automatically")
	}
}
func TestTeamReviewVerdict(t *testing.T) {
	for _, v := range []struct {
		log  string
		want bool
	}{{`{"review":"pass","findings":[]}`, true}, {`{"review":"pass","findings":["bug"]}`, false}, {"all done", false}, {"{\"review\":\"pass\",\"findings\":[]}\n{\"review\":\"fail\",\"findings\":[\"bug\"]}", false}} {
		if reviewPassed(v.log) != v.want {
			t.Fatal(v)
		}
	}
}
