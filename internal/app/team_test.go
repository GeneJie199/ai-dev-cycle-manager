package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	input := ""
	if os.Args[len(os.Args)-1] == "-" {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		if err != nil || len(data) == 0 {
			os.Exit(2)
		}
		input = string(data)
	}
	switch mode := os.Getenv("DEVCYCLE_TEAM_FAILURE_MODE"); mode {
	case "timeout":
		time.Sleep(3 * time.Second)
	case "invalidcredentials", "ratelimit", "toolfailure":
		println("injected CLI failure: " + mode)
		os.Exit(1)
	case "partialexecution":
		if err := os.WriteFile("partial.txt", []byte("unfinished work\n"), 0o600); err != nil {
			os.Exit(2)
		}
		println("injected failure after a real partial filesystem effect")
		os.Exit(1)
	case "malformed":
		if strings.Contains(input, "Role: review.") {
			println("unstructured review with no verdict")
			os.Exit(0)
		}
	}
	if file := os.Getenv("DEVCYCLE_TEAM_WRITE"); file != "" && strings.Contains(input, "Role: lead.") {
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			os.Exit(2)
		}
		content := os.Getenv("DEVCYCLE_TEAM_WRITE_CONTENT")
		if content == "" {
			content = "# Real controller commit fixture\n"
		}
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			os.Exit(2)
		}
	}
	if os.Getenv("DEVCYCLE_TEAM_FRONTEND") == "1" && strings.Contains(input, "Role: frontend.") {
		if err := os.MkdirAll("frontend", 0o700); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile("frontend/proof.txt", []byte("frontend implemented\n"), 0o600); err != nil {
			os.Exit(2)
		}
	}
	if os.Getenv("DEVCYCLE_TEAM_FAIL") == "1" {
		os.Exit(1)
	}
	if os.Getenv("DEVCYCLE_TEAM_SLOW") == "1" {
		time.Sleep(400 * time.Millisecond)
	}
	if os.Getenv("DEVCYCLE_TEAM_REVIEW_FAIL") == "1" {
		println(`{"review":"fail","findings":["missing regression coverage"]}`)
		os.Exit(0)
	}
	println(`{"review":"pass","findings":[]}`)
	os.Exit(0)
}

func TestTeamConcurrentAdvanceAndStoreOwnership(t *testing.T) {
	a, input := teamAppFixture(t)
	ctx := context.Background()
	t.Setenv("DEVCYCLE_TEAM_SLOW", "1")
	m, err := a.CreateMission(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := team.Open(a.DBPath + ".team.db"); err == nil {
		_ = other.Close()
		t.Fatal("concurrent runtime opened team store")
	}
	finished := make(chan error, 1)
	go func() { _, err := a.AdvanceMission(ctx, m.ID); finished <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := a.Team.Get(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.State == "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = a.AdvanceMission(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
	m, err = a.Team.Get(ctx, m.ID)
	if err != nil || len(m.Executions) != 1 {
		t.Fatal("duplicate dispatch", m, err)
	}
}

func TestTeamReviewFixRegressionCycle(t *testing.T) {
	a, input := teamAppFixture(t)
	ctx := context.Background()
	m, err := a.CreateMission(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		m, err = a.AdvanceMission(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DEVCYCLE_TEAM_REVIEW_FAIL", "1")
	m, err = a.AdvanceMission(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.FixCycles != 1 || m.Plan[1].State != "retry" || m.State != "retry" {
		t.Fatalf("review did not hand back to lead: %+v", m)
	}
	t.Setenv("DEVCYCLE_TEAM_REVIEW_FAIL", "")
	for i := 0; i < 4 && m.State != "completed"; i++ {
		m, err = a.AdvanceMission(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if m.State != "completed" || len(m.Executions) != 7 {
		t.Fatalf("repair not regression-verified: %s, %+v", m.State, m.Executions)
	}
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
	_, err = a.ConfigureAgentAdapter(ctx, models.AgentAdapterConfig{ID: "team-test", Name: "Process fixture", Command: binary, Args: []string{"-test.run=^TestTeamAgentProcess$", "--", "{{model}}", "{{prompt_stdin}}"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	check := "git status --porcelain"
	m := team.Mission{Repository: repo, Goal: "Document the team workflow", Mode: "simple", Checks: []string{check}, Models: []team.Model{{ID: "fixture", Provider: "local", Adapter: "team-test", Capabilities: team.Capabilities, ContextLimit: 100000, Tier: 2, Available: true, Tools: true}}, Policy: team.Policy{TrustedLocalCLI: true, SourceExport: true, AllowedProviders: []string{"local"}}, Budget: team.Budget{MaxExecutions: 10, TimeoutSeconds: 10}}
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
	if m.State != "escalated" || m.Plan[0].State != "escalated" {
		t.Fatal("ambiguous worker would replay automatically")
	}
}

// These are controlled local process failures, not real-provider success tests.
func TestMissionInjectedCLIFailuresNeverBecomeSuccess(t *testing.T) {
	for _, mode := range []string{"invalidcredentials", "ratelimit", "timeout", "toolfailure", "partialexecution", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			a, input := teamAppFixture(t)
			t.Setenv("DEVCYCLE_TEAM_FAILURE_MODE", mode)
			if mode == "timeout" {
				input.Budget.TimeoutSeconds = 1
			}
			ctx := context.Background()
			m, err := a.CreateMission(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			steps := 1
			if mode == "malformed" {
				steps = len(m.Plan)
			}
			for i := 0; i < steps; i++ {
				m, err = a.AdvanceMission(ctx, m.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			last := m.Executions[len(m.Executions)-1]
			if m.State == "completed" || last.Result != "failed" || len(last.EvidenceIDs) == 0 {
				t.Fatalf("injected %s became success or lost evidence: %+v", mode, m)
			}
			if mode == "partialexecution" {
				if _, err := os.Stat(filepath.Join(last.Worktree, "partial.txt")); err != nil {
					t.Fatal("partial execution fixture produced no effect", err)
				}
			}
			criteria, err := a.ListCriteriaByRequirement(ctx, m.RequirementID)
			if err != nil {
				t.Fatal(err)
			}
			for _, criterion := range criteria {
				if criterion.Satisfied {
					t.Fatal("failure marked acceptance satisfied")
				}
			}
		})
	}
}

func TestMissionUnavailableContextAndBudgetNeverDispatch(t *testing.T) {
	for _, mode := range []string{"providerunavailable", "contextoverflow", "budgetexhausted"} {
		t.Run(mode, func(t *testing.T) {
			a, input := teamAppFixture(t)
			switch mode {
			case "providerunavailable":
				input.Models[0].Available = false
			case "contextoverflow":
				input.Models[0].ContextLimit = 1
			case "budgetexhausted":
				input.Budget.MaxExecutions = 1
			}
			ctx := context.Background()
			m, err := a.CreateMission(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			m, err = a.AdvanceMission(ctx, m.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "budgetexhausted" {
				m, err = a.AdvanceMission(ctx, m.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			last := m.Executions[len(m.Executions)-1]
			if m.State != "escalated" || last.Result != "escalated" || last.SessionID != "" {
				t.Fatalf("%s dispatched a worker: %+v", mode, m)
			}
		})
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

func TestMissionRiskIncludesEveryInstructionSource(t *testing.T) {
	for _, source := range []string{"task", "acceptance", "checks", "integration", "security", "destructive command"} {
		t.Run(source, func(t *testing.T) {
			a, input := teamAppFixture(t)
			input.Mode = "expert"
			input.Plan = team.Simple(input.Goal)
			switch source {
			case "task":
				input.Plan[1].Prompt = "Change production configuration"
			case "acceptance":
				input.Acceptance = []string{"Production deployment succeeds"}
			case "checks":
				input.Checks = []string{"echo production"}
			case "integration":
				input.IntegrationChecks = []string{"echo production"}
			case "security":
				input.SecurityChecks = []string{"echo production"}
			case "destructive command":
				input.Checks = []string{"Remove-Item -Recurse -Force ../outside"}
			}
			ctx := context.Background()
			m, err := a.CreateMission(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if m.Risk != 4 {
				t.Fatalf("%s escaped risk assessment: R%d", source, m.Risk)
			}
			// Simulate an older binary that persisted the incorrectly low risk.
			m.Risk = 1
			if err = a.Team.Save(ctx, &m); err != nil {
				t.Fatal(err)
			}
			m, err = a.AdvanceMission(ctx, m.ID)
			if err != nil || m.State != "escalated" || len(m.Executions) != 0 {
				t.Fatalf("unsafe dispatch: mission=%+v err=%v", m, err)
			}
			if _, err = a.RetryMission(ctx, m.ID); err == nil {
				t.Fatal("R4 retry accepted")
			}
		})
	}
}

func TestMissionSecretsAreNotPersistedInInstructions(t *testing.T) {
	for _, source := range []string{"checks", "integration", "security"} {
		t.Run(source, func(t *testing.T) {
			a, input := teamAppFixture(t)
			secretCheck := []string{"echo api_key=review-secret-literal"}
			switch source {
			case "checks":
				input.Checks = secretCheck
			case "integration":
				input.IntegrationChecks = secretCheck
			case "security":
				input.SecurityChecks = secretCheck
			}
			if _, err := a.CreateMission(context.Background(), input); err == nil {
				t.Fatal("secret-bearing command accepted")
			}
			missions, err := a.Team.List(context.Background())
			if err != nil || len(missions) != 0 {
				t.Fatalf("rejected input persisted: %+v %v", missions, err)
			}
		})
	}
	a, input := teamAppFixture(t)
	input.Goal += " api_key=review-secret-literal"
	input.Mode = "expert"
	input.Plan = team.Simple(input.Goal)
	input.Acceptance = []string{"Verify api_key=review-secret-literal"}
	m, err := a.CreateMission(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := a.Team.Get(context.Background(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saved.Goal, "review-secret-literal") || strings.Contains(saved.Acceptance[0], "review-secret-literal") {
		t.Fatal("goal or acceptance secret persisted")
	}
	for _, task := range saved.Plan {
		if strings.Contains(task.Prompt, "review-secret-literal") {
			t.Fatal("task secret persisted")
		}
	}
}

func TestAdapterRejectsCredentialLiteralsBeforePersistence(t *testing.T) {
	a, _ := teamAppFixture(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config := models.AgentAdapterConfig{ID: "credential-test", Name: "Environment fixture", Command: binary, Args: []string{"--api-key", "literal-value", "{{prompt}}"}, Enabled: true}
	if _, err = a.ConfigureAgentAdapter(context.Background(), config); err == nil {
		t.Fatal("credential argument accepted")
	}
	if _, err = a.Store.GetAgentAdapter(context.Background(), config.ID); err == nil {
		t.Fatal("rejected configuration persisted")
	}
	config.Args[1] = "$env:OPENAI_API_KEY"
	if _, err = a.ConfigureAgentAdapter(context.Background(), config); err != nil {
		t.Fatalf("environment reference rejected: %v", err)
	}
}

func TestExhaustedRetryStaysEscalated(t *testing.T) {
	a, input := teamAppFixture(t)
	ctx := context.Background()
	m, err := a.CreateMission(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	m.State = "escalated"
	m.Plan[0].State = "escalated"
	m.Plan[0].Attempts = m.Plan[0].MaxAttempts
	if err = a.Team.Save(ctx, &m); err != nil {
		t.Fatal(err)
	}
	if _, err = a.RetryMission(ctx, m.ID); err == nil {
		t.Fatal("exhausted retry became an advancing loop")
	}
	stored, err := a.Team.Get(ctx, m.ID)
	if err != nil || stored.State != "escalated" {
		t.Fatal("retry mutated terminal state", err)
	}
}
func TestTeamReviewVerdict(t *testing.T) {
	if reviewPassed("{\"review\":\"pass\",\"findings\":[]}\n{\"review\":\"fail\",\"findings\":[{\"file\":\"main.go\",\"issue\":\"bug\"}]}") {
		t.Fatal("object findings bypassed conflicting verdict")
	}
	if !reviewPassed("{\"review\":\"pass\",\"findings\":[]}\n{\"review\":\"pass\",\"findings\":[]}") {
		t.Fatal("transport replay rejected")
	}
	for _, v := range []struct {
		log  string
		want bool
	}{{`{"review":"pass","findings":[]}`, true}, {`{"review":"pass","findings":["bug"]}`, false}, {"all done", false}, {"{\"review\":\"pass\",\"findings\":[]}\n{\"review\":\"fail\",\"findings\":[\"bug\"]}", false}} {
		if reviewPassed(v.log) != v.want {
			t.Fatal(v)
		}
	}
}

func TestRuntimeCommitsOnlyValidatedTaskChanges(t *testing.T) {
	a, input := teamAppFixture(t)
	input.Policy.CommitTaskChanges = true
	ctx := context.Background()
	m, err := a.CreateMission(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	m, err = a.AdvanceMission(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVCYCLE_TEAM_WRITE", "docs/commit-proof.md")
	m, err = a.AdvanceMission(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	task := m.Plan[1]
	if task.State != "completed" || task.Head == task.ArtifactHead {
		t.Fatalf("controller did not commit allowed change: %+v", m.Executions)
	}
	dirty, err := missionGit(ctx, task.Worktree, "status", "--porcelain")
	if err != nil || dirty != "" {
		t.Fatal("controller left dirty worktree", err)
	}
}

func TestExpertOrderAndFrontendRepairPreserveIntegratedSource(t *testing.T) {
	a, input := teamAppFixture(t)
	ctx := context.Background()
	input.Mode = "expert"
	input.Goal = "Add frontend and documentation"
	plan := team.Simple(input.Goal)
	input.Plan = []team.Task{plan[2], plan[1], plan[0], plan[3], plan[4]}
	input.Policy.CommitTaskChanges = true
	input.Budget.MaxExecutions = 15
	input.Budget.MaxFixes = 1
	t.Setenv("DEVCYCLE_TEAM_WRITE", "docs/backend.md")
	t.Setenv("DEVCYCLE_TEAM_FRONTEND", "1")
	m, err := a.CreateMission(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		m, err = a.AdvanceMission(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = os.Stat(filepath.Join(m.Plan[3].Worktree, "frontend/proof.txt")); err != nil {
		t.Fatal("QA omitted frontend branch", err)
	}
	t.Setenv("DEVCYCLE_TEAM_REVIEW_FAIL", "1")
	m, err = a.AdvanceMission(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.FixCycles != 1 {
		t.Fatal("fix was not scheduled")
	}
	t.Setenv("DEVCYCLE_TEAM_REVIEW_FAIL", "")
	t.Setenv("DEVCYCLE_TEAM_WRITE_CONTENT", "repair verified\n")
	for i := 0; i < 4 && m.State != "completed"; i++ {
		m, err = a.AdvanceMission(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if m.State != "completed" {
		t.Fatalf("repair failed: %+v", m.Executions)
	}
	qa := m.Plan[3]
	contents, err := os.ReadFile(filepath.Join(qa.Worktree, "docs/backend.md"))
	if err != nil || strings.TrimSpace(string(contents)) != "repair verified" {
		t.Fatal("QA omitted repaired backend", err, string(contents))
	}
	if _, err = os.Stat(filepath.Join(qa.Worktree, "frontend/proof.txt")); err != nil {
		t.Fatal("repair lost frontend", err)
	}
}

func TestPromptCannotDeclareModelArgument(t *testing.T) {
	a, input := teamAppFixture(t)
	ctx := context.Background()
	req, err := a.CreateRequirement(ctx, "Model template test", "")
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(ctx, req.ID, "Model template", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = a.LinkTaskToWorktree(ctx, input.Repository, task.ID, "model-template", filepath.Join(t.TempDir(), "worktree"))
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.ConfigureAgentAdapter(ctx, models.AgentAdapterConfig{ID: "no-model", Name: "Unmodeled fixture", Command: binary, Args: []string{"-test.run=^TestTeamAgentProcess$", "--", "{{prompt}}"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.startAgentSessionModel(ctx, task.ID, "no-model", "declared-model", "This untrusted prompt contains {{model}}"); err == nil || !strings.Contains(err.Error(), "explicit {{model}}") {
		t.Fatal("prompt bypassed adapter model template", err)
	}
}
