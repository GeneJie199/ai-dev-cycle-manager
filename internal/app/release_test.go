package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

func openTestApp(t *testing.T) *App {
	t.Helper()
	a, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func initReleaseRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		command := exec.Command("git", args...)
		command.Dir = dir
		command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "main.go")
	run("commit", "-m", "initial")
	return dir
}

func TestUpdateTaskStatusValidation(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()

	req, err := a.CreateRequirement(ctx, "REQ", "")
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(ctx, req.ID, "T1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateTaskStatus(ctx, task.ID, "bogus"); err == nil {
		t.Fatal("expected error for invalid status")
	}
	task, err = a.UpdateTaskStatus(ctx, task.ID, models.TaskStatusInProgress)
	if err != nil || task.Status != models.TaskStatusInProgress {
		t.Fatalf("UpdateTaskStatus: %+v %v", task, err)
	}
}

func TestTaskDependenciesBlockInvalidTransitionsAndCycles(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()
	requirement, _ := a.CreateRequirement(ctx, "dependency workflow", "")
	first, _ := a.CreateTask(ctx, requirement.ID, "prepare schema", "")
	second, _ := a.CreateTask(ctx, requirement.ID, "implement API", "")
	if _, err := a.UpdateTask(ctx, second.ID, second.Title, second.Description, models.TaskStatusTodo, []string{first.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateTaskStatus(ctx, second.ID, models.TaskStatusInProgress); err == nil {
		t.Fatal("blocked task should not start before its prerequisite")
	}
	if _, err := a.UpdateTaskStatus(ctx, first.ID, models.TaskStatusDone); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateTaskStatus(ctx, second.ID, models.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateTaskStatus(ctx, first.ID, models.TaskStatusTodo); err == nil {
		t.Fatal("active dependent should prevent prerequisite rollback")
	}
	third, _ := a.CreateTask(ctx, requirement.ID, "write docs", "")
	fourth, _ := a.CreateTask(ctx, requirement.ID, "review docs", "")
	if _, err := a.UpdateTask(ctx, third.ID, third.Title, "", models.TaskStatusTodo, []string{fourth.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateTask(ctx, fourth.ID, fourth.Title, "", models.TaskStatusTodo, []string{third.ID}); err == nil {
		t.Fatal("dependency cycle should be rejected")
	}
}

func TestUpdateCriterionResult(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()

	req, _ := a.CreateRequirement(ctx, "REQ", "")
	c, err := a.CreateCriterion(ctx, req.ID, "works")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.UpdateCriterionResult(ctx, c.ID, true); err == nil {
		t.Fatal("criterion must reject satisfaction without evidence")
	}
	if _, err = a.AddEvidence(ctx, models.Evidence{RequirementID: req.ID, CriterionID: c.ID, Kind: "manual", Title: "reviewed", Status: "passed"}); err != nil {
		t.Fatal(err)
	}
	c, err = a.UpdateCriterionResult(ctx, c.ID, true)
	if err != nil || !c.Satisfied {
		t.Fatalf("UpdateCriterionResult: %+v %v", c, err)
	}
}

func TestLatestEvidenceControlsCriterionAndReadiness(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()
	requirement, _ := a.CreateRequirement(ctx, "evidence ordering", "")
	criterion, _ := a.CreateCriterion(ctx, requirement.ID, "latest check passes")
	if _, err := a.AddEvidence(ctx, models.Evidence{RequirementID: requirement.ID, CriterionID: criterion.ID, Kind: "test", Title: "first pass", Status: "passed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateCriterionResult(ctx, criterion.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddEvidence(ctx, models.Evidence{RequirementID: requirement.ID, CriterionID: criterion.ID, Kind: "test", Title: "regression", Status: "failed"}); err != nil {
		t.Fatal(err)
	}
	criterion, err := a.Store.GetCriterion(ctx, criterion.ID)
	if err != nil || criterion.Satisfied {
		t.Fatalf("failed evidence should reopen criterion: %+v %v", criterion, err)
	}
	if _, err = a.UpdateCriterionResult(ctx, criterion.ID, true); err == nil {
		t.Fatal("historical pass must not override a newer failure")
	}
	candidate, err := a.ExportReleaseCandidate(ctx, requirement.ID)
	if err != nil || candidate.Readiness.CriteriaWithEvidence != 0 || candidate.Readiness.Ready {
		t.Fatalf("readiness = %+v, %v", candidate.Readiness, err)
	}
}

func TestExportReleaseCandidate(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()

	req, err := a.CreateRequirement(ctx, "Ship it", "desc")
	if err != nil {
		t.Fatal(err)
	}
	c1, _ := a.CreateCriterion(ctx, req.ID, "criterion one")
	c2, _ := a.CreateCriterion(ctx, req.ID, "criterion two")
	t1, _ := a.CreateTask(ctx, req.ID, "task one", "")
	t2, _ := a.CreateTask(ctx, req.ID, "task two", "")

	// Not ready: nothing satisfied/done.
	rc, err := a.ExportReleaseCandidate(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rc.Spec != ReleaseCandidateSpec || rc.Kind != "release-candidate" {
		t.Fatalf("unexpected header: %+v", rc)
	}
	if rc.Readiness.Ready {
		t.Fatal("expected not ready")
	}
	if rc.Readiness.CriteriaTotal != 2 || rc.Readiness.TasksTotal != 2 {
		t.Fatalf("unexpected readiness: %+v", rc.Readiness)
	}
	if len(rc.AcceptanceCriteria) != 2 || len(rc.Tasks) != 2 {
		t.Fatalf("unexpected lengths: %+v", rc)
	}

	// Satisfy all criteria and finish all tasks -> ready.
	for _, id := range []string{c1.ID, c2.ID} {
		if _, err := a.AddEvidence(ctx, models.Evidence{RequirementID: req.ID, CriterionID: id, Kind: "test", Title: "criterion proof", Status: "passed"}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.UpdateCriterionResult(ctx, id, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{t1.ID, t2.ID} {
		if _, err := a.UpdateTaskStatus(ctx, id, models.TaskStatusDone); err != nil {
			t.Fatal(err)
		}
	}
	rc, err = a.ExportReleaseCandidate(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rc.Readiness.Ready {
		t.Fatalf("expected ready: %+v", rc.Readiness)
	}
	if rc.Requirement.ID != req.ID {
		t.Fatalf("requirement mismatch: %+v", rc.Requirement)
	}
}

func TestExportReleaseCandidateNotFound(t *testing.T) {
	a := openTestApp(t)
	if _, err := a.ExportReleaseCandidate(context.Background(), "missing-id"); err == nil {
		t.Fatal("expected error for unknown requirement")
	}
}

func TestReleaseCandidatePinsCleanGitSources(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()
	repository := initReleaseRepo(t)
	requirement, _ := a.CreateRequirement(ctx, "source provenance", "")
	criterion, _ := a.CreateCriterion(ctx, requirement.ID, "source commit is pinned")
	if _, err := a.AddEvidence(ctx, models.Evidence{RequirementID: requirement.ID, CriterionID: criterion.ID, Kind: "test", Title: "source check", Status: "passed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateCriterionResult(ctx, criterion.ID, true); err != nil {
		t.Fatal(err)
	}
	task, _ := a.CreateTask(ctx, requirement.ID, "implement source", "")
	task, err := a.Store.LinkTaskGit(ctx, task.ID, "main", repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.UpdateTaskStatus(ctx, task.ID, models.TaskStatusDone); err != nil {
		t.Fatal(err)
	}
	candidate, err := a.ExportReleaseCandidate(ctx, requirement.ID)
	if err != nil || len(candidate.Sources) != 1 || len(candidate.Sources[0].HeadCommit) != 40 || !candidate.Sources[0].Clean || candidate.Sources[0].DirtyFiles == nil || !candidate.Readiness.Ready {
		t.Fatalf("clean candidate = %+v, %v", candidate, err)
	}
	if err = os.WriteFile(filepath.Join(repository, "main.go"), []byte("package main\n// dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidate, err = a.ExportReleaseCandidate(ctx, requirement.ID)
	if err != nil || candidate.Sources[0].Clean || candidate.Readiness.SourcesClean != 0 || candidate.Readiness.Ready {
		t.Fatalf("dirty candidate readiness = %+v, %v", candidate.Readiness, err)
	}
}
