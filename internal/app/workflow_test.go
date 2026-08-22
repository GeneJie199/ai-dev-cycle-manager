package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

func TestVerificationCreatesEvidenceAndReport(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()
	req, _ := a.CreateRequirement(ctx, "verified feature", "")
	criterion, _ := a.CreateCriterion(ctx, req.ID, "command succeeds")
	command := "printf ok"
	if runtime.GOOS == "windows" {
		command = "Write-Output ok"
	}
	run, err := a.RunVerification(ctx, req.ID, criterion.ID, "test suite", command, t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "passed" || run.EvidenceID == "" {
		t.Fatalf("run=%+v", run)
	}
	if _, err = a.UpdateCriterionResult(ctx, criterion.ID, true); err != nil {
		t.Fatal(err)
	}
	report, err := a.DevelopmentReport(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.EvidenceComplete || len(report.Evidence) != 1 || len(report.VerificationRuns) != 1 {
		t.Fatalf("report=%+v", report)
	}
}

func waitForVerificationStatus(t *testing.T, a *App, requirementID, runID string, terminal map[string]bool) models.VerificationRun {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := a.ListVerificationRuns(context.Background(), requirementID)
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range runs {
			if run.ID == runID && terminal[run.Status] {
				return run
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("verification %s did not reach a terminal status", runID)
	return models.VerificationRun{}
}

func TestAsyncVerificationPersistsOutputAndEvidence(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()
	requirement, _ := a.CreateRequirement(ctx, "async verification", "")
	criterion, _ := a.CreateCriterion(ctx, requirement.ID, "async command succeeds")
	command := "sleep 0.2; printf async-ok"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Milliseconds 200; Write-Output async-ok"
	}
	run, err := a.StartVerification(ctx, requirement.ID, criterion.ID, "async test", command, t.TempDir(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" || run.ID == "" {
		t.Fatalf("started run = %+v", run)
	}
	completed := waitForVerificationStatus(t, a, requirement.ID, run.ID, map[string]bool{"passed": true})
	if completed.ExitCode != 0 || completed.EvidenceID == "" || !strings.Contains(completed.Output, "async-ok") {
		t.Fatalf("completed run = %+v", completed)
	}
}

func TestAsyncVerificationCanBeStopped(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()
	requirement, _ := a.CreateRequirement(ctx, "stoppable verification", "")
	command := "printf started; sleep 30"
	if runtime.GOOS == "windows" {
		command = "Write-Output started; Start-Sleep -Seconds 30"
	}
	run, err := a.StartVerification(ctx, requirement.ID, "", "long smoke", command, t.TempDir(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	stopStarted := time.Now()
	stopping, err := a.StopVerification(ctx, run.ID)
	if err != nil || stopping.Status != "stopping" {
		t.Fatalf("stop = %+v, %v", stopping, err)
	}
	completed := waitForVerificationStatus(t, a, requirement.ID, run.ID, map[string]bool{"stopped": true})
	if elapsed := time.Since(stopStarted); elapsed > 5*time.Second {
		t.Fatalf("stopping verification took %s", elapsed)
	}
	if completed.EvidenceID == "" {
		t.Fatalf("stopped run missing evidence: %+v", completed)
	}
}

func TestNewRecoversRunningVerificationRuns(t *testing.T) {
	path := t.TempDir() + "/verification-recovery.db"
	a, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	requirement, _ := a.CreateRequirement(ctx, "recover verification", "")
	run, err := a.Store.CreateVerificationRun(ctx, models.VerificationRun{RequirementID: requirement.ID, Name: "orphan", Command: "test", WorkingDir: t.TempDir(), Status: "running", ExitCode: -1, StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	a.Store = nil

	reopened, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := reopened.Store.GetVerificationRun(ctx, run.ID)
	if err != nil || recovered.Status != "interrupted" || recovered.CompletedAt.IsZero() {
		t.Fatalf("recovered run = %+v, %v", recovered, err)
	}
}
func TestEvidenceValidation(t *testing.T) {
	a := openTestApp(t)
	if _, err := a.AddEvidence(context.Background(), models.Evidence{Kind: "unknown"}); err == nil {
		t.Fatal("expected validation error")
	}
	ctx := context.Background()
	first, _ := a.CreateRequirement(ctx, "first", "")
	second, _ := a.CreateRequirement(ctx, "second", "")
	criterion, _ := a.CreateCriterion(ctx, second.ID, "belongs to second")
	if _, err := a.AddEvidence(ctx, models.Evidence{RequirementID: first.ID, CriterionID: criterion.ID, Kind: "manual", Title: "wrong link"}); err == nil {
		t.Fatal("expected cross-requirement criterion rejection")
	}
}

func TestProviderCommand(t *testing.T) {
	tests := []struct {
		provider string
		binary   string
		args     []string
	}{
		{provider: "codex", binary: "codex", args: []string{"exec", "do the work"}},
		{provider: "kimi", binary: "kimi", args: []string{"-p", "do the work"}},
		{provider: "claude", binary: "claude", args: []string{"-p", "do the work"}},
		{provider: "gemini", binary: "gemini", args: []string{"-p", "do the work"}},
		{provider: "opencode", binary: "opencode", args: []string{"run", "do the work"}},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			binary, args, err := providerCommand(tt.provider, "do the work")
			if err != nil || binary != tt.binary || !reflect.DeepEqual(args, tt.args) {
				t.Fatalf("providerCommand() = %q, %v, %v", binary, args, err)
			}
		})
	}
	if _, _, err := providerCommand("unknown", "x"); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestEvidenceKindsAreCanonicalAndContentIsRedacted(t *testing.T) {
	a := openTestApp(t)
	requirement, _ := a.CreateRequirement(context.Background(), "Safe evidence", "")
	evidence, err := a.AddEvidence(context.Background(), models.Evidence{RequirementID: requirement.ID, Kind: "manual", Title: "Reviewed token=title-secret", Status: "passed", Inline: "password=hunter2", URI: "https://user:url-secret@example.com/result", Metadata: map[string]string{"command": "api_key=metadata-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(evidence)
	for _, secret := range []string{"title-secret", "hunter2", "url-secret", "metadata-secret"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("evidence leaked %q: %s", secret, encoded)
		}
	}
	if evidence.Kind != models.EvidenceKindHumanConfirmation {
		t.Fatalf("kind=%q", evidence.Kind)
	}
}

func TestTaskHandoffValidatesSessionOwnershipAndAcceptance(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()
	requirement, _ := a.CreateRequirement(ctx, "Agent handoff", "")
	task, _ := a.CreateTask(ctx, requirement.ID, "Implement feature", "")
	otherTask, _ := a.CreateTask(ctx, requirement.ID, "Review feature", "")
	source, _ := a.Store.CreateAgentSession(ctx, models.AgentSession{TaskID: task.ID, Provider: "kimi", Prompt: "implement", WorkingDir: t.TempDir(), Status: "completed"})
	wrong, _ := a.Store.CreateAgentSession(ctx, models.AgentSession{TaskID: otherTask.ID, Provider: "codex", Prompt: "review", WorkingDir: t.TempDir(), Status: "completed"})
	if _, err := a.CreateTaskHandoff(ctx, task.ID, TaskHandoffInput{FromSessionID: wrong.ID, ToAdapter: "codex", Summary: "Continue the implementation", RemainingWork: []string{"Review changes"}}); err == nil {
		t.Fatal("expected cross-task source session rejection")
	}
	handoff, err := a.CreateTaskHandoff(ctx, task.ID, TaskHandoffInput{FromSessionID: source.ID, ToAdapter: "codex", Summary: "Implementation complete; review next", CompletedWork: []string{"Added the implementation"}, RemainingWork: []string{"Review the diff"}, Validation: []string{"Run go test"}})
	if err != nil || handoff.FromAdapter != "kimi" || handoff.ToAdapter != "codex" {
		t.Fatalf("handoff=%+v err=%v", handoff, err)
	}
	wrongTarget, _ := a.Store.CreateAgentSession(ctx, models.AgentSession{TaskID: task.ID, Provider: "kimi", Prompt: "accept", WorkingDir: t.TempDir(), Status: "completed"})
	if _, err = a.AcceptTaskHandoff(ctx, handoff.ID, wrongTarget.ID); err == nil {
		t.Fatal("expected target adapter rejection")
	}
	accepting, _ := a.Store.CreateAgentSession(ctx, models.AgentSession{TaskID: task.ID, Provider: "codex", Prompt: "accept", WorkingDir: t.TempDir(), Status: "completed"})
	accepted, err := a.AcceptTaskHandoff(ctx, handoff.ID, accepting.ID)
	if err != nil || accepted.Status != models.HandoffStatusAccepted || accepted.AcceptedSession != accepting.ID {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
}

func TestNewRecoversRunningAgentSessions(t *testing.T) {
	path := t.TempDir() + "/recovery.db"
	a, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	requirement, _ := a.CreateRequirement(ctx, "recover", "")
	task, _ := a.CreateTask(ctx, requirement.ID, "agent", "")
	session, err := a.Store.CreateAgentSession(ctx, models.AgentSession{TaskID: task.ID, Provider: "kimi", Prompt: "x", WorkingDir: t.TempDir(), Status: "running", PID: 123})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	a.Store = nil

	reopened, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Store.GetAgentSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "interrupted" || got.PID != 0 || got.EndedAt.IsZero() {
		t.Fatalf("recovered session = %+v", got)
	}
}

func TestImpactClassificationUsesPathSegments(t *testing.T) {
	impact := ChangeImpact{}
	classifyImpactPath(&impact, "docs/capitol-building.md")
	if impact.APIImpact {
		t.Fatal("an incidental api substring must not be classified as an API change")
	}
	classifyImpactPath(&impact, "internal/api/orders.go")
	classifyImpactPath(&impact, "deploy/compose.yaml")
	classifyImpactPath(&impact, "web/static/app.css")
	classifyImpactPath(&impact, "migrations/001_orders.sql")
	classifyImpactPath(&impact, "internal/auth/session.go")
	if !impact.APIImpact || !impact.ConfigurationImpact || !impact.UserImpact || !impact.DatabaseImpact || !impact.SecurityImpact {
		t.Fatalf("impact = %+v", impact)
	}
}

func TestHumanizedGitImpactIncludesChangesRisksAndVerification(t *testing.T) {
	a := openTestApp(t)
	repo := initReleaseRepo(t)
	files := map[string]string{
		"main.go":                  "package main\n\nfunc main() {}\n",
		"web/login.html":           "<main>Login</main>\n",
		"migrations/001_login.sql": "ALTER TABLE users ADD COLUMN phone TEXT;\n",
		"internal/auth/session.go": "package auth\n",
		"config/app.yaml":          "login: true\n",
	}
	for name, content := range files {
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	impact, err := a.AnalyzeChanges(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(impact.AddedFiles) != 4 || len(impact.ModifiedFiles) != 1 || impact.Risk != "high" || !impact.RawDiffAvailable {
		t.Fatalf("impact=%+v", impact)
	}
	if !impact.UserImpact || !impact.DatabaseImpact || !impact.ConfigurationImpact || !impact.SecurityImpact || len(impact.RiskReasons) < 3 || len(impact.SuggestedVerification) < 5 {
		t.Fatalf("humanized impact incomplete: %+v", impact)
	}
}

func TestAgentSessionMetricsAndPrivateFields(t *testing.T) {
	started := time.Now().UTC().Add(-2 * time.Second)
	ended := started.Add(1500 * time.Millisecond)
	session := withAgentSessionMetrics(models.AgentSession{Prompt: "secret prompt", WorkingDir: "C:/secret", LogPath: "C:/secret/log", StartedAt: started, EndedAt: ended})
	if session.DurationMilliseconds != 1500 || session.LogLimitBytes != maxAgentLogBytes || session.CostStatus != "not_reported_by_cli" {
		t.Fatalf("metrics = %+v", session)
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret prompt", "C:/secret"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("session JSON leaked %q: %s", secret, encoded)
		}
	}
}
