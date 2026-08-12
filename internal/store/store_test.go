package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrationVersionAndInsertPlanRollback(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != SchemaVersion {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	requirement, err := s.CreateRequirement(ctx, "migration", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	err = s.InsertPlan(ctx,
		[]models.AcceptanceCriterion{{ID: "criterion-rollback", RequirementID: requirement.ID, Description: "must roll back", CreatedAt: now}},
		[]models.Task{{ID: "task-rollback", RequirementID: requirement.ID, Title: "invalid dependency", Status: models.TaskStatusTodo, DependsOn: []string{"missing-task"}, CreatedAt: now, UpdatedAt: now}},
	)
	if err == nil {
		t.Fatal("expected foreign-key failure")
	}
	criteria, listErr := s.ListCriteriaByRequirement(ctx, requirement.ID)
	if listErr != nil || len(criteria) != 0 {
		t.Fatalf("criteria after rollback=%v err=%v", criteria, listErr)
	}
	tasks, listErr := s.ListTasksByRequirement(ctx, requirement.ID)
	if listErr != nil || len(tasks) != 0 {
		t.Fatalf("tasks after rollback=%v err=%v", tasks, listErr)
	}
}

func TestRequirementCriteriaTaskCRUD(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	req, err := s.CreateRequirement(ctx, "DEV-005", "SQLite models + store")
	if err != nil {
		t.Fatalf("CreateRequirement: %v", err)
	}
	got, err := s.GetRequirement(ctx, req.ID)
	if err != nil || got.Title != "DEV-005" {
		t.Fatalf("GetRequirement: %+v %v", got, err)
	}

	req, err = s.UpdateRequirement(ctx, req.ID, "DEV-005 updated", "desc2")
	if err != nil || req.Title != "DEV-005 updated" {
		t.Fatalf("UpdateRequirement: %+v %v", req, err)
	}

	c1, err := s.CreateCriterion(ctx, req.ID, "CRUD works")
	if err != nil {
		t.Fatalf("CreateCriterion: %v", err)
	}
	c1, err = s.UpdateCriterion(ctx, c1.ID, "CRUD works", true)
	if err != nil || !c1.Satisfied {
		t.Fatalf("UpdateCriterion: %+v %v", c1, err)
	}
	criteria, err := s.ListCriteriaByRequirement(ctx, req.ID)
	if err != nil || len(criteria) != 1 {
		t.Fatalf("ListCriteria: %v len=%d", err, len(criteria))
	}

	task, err := s.CreateTask(ctx, req.ID, "Implement store", "unit tests")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.Status != models.TaskStatusTodo {
		t.Fatalf("status=%s", task.Status)
	}

	task, err = s.LinkTaskGit(ctx, task.ID, "feature/store", `C:\wt\feature-store`)
	if err != nil {
		t.Fatalf("LinkTaskGit: %v", err)
	}
	if task.Branch != "feature/store" || task.WorktreePath == "" {
		t.Fatalf("link fields: %+v", task)
	}
	if task.Status != models.TaskStatusInProgress {
		t.Fatalf("expected in_progress after link, got %s", task.Status)
	}

	tasks, err := s.ListTasksByRequirement(ctx, req.ID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("ListTasks: %v len=%d", err, len(tasks))
	}

	if err := s.DeleteTask(ctx, task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if err := s.DeleteCriterion(ctx, c1.ID); err != nil {
		t.Fatalf("DeleteCriterion: %v", err)
	}
	if err := s.DeleteRequirement(ctx, req.ID); err != nil {
		t.Fatalf("DeleteRequirement: %v", err)
	}
	list, err := s.ListRequirements(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("expected empty requirements, got %d %v", len(list), err)
	}
}

func TestRepositoryAndCascade(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	repo, err := s.CreateRepository(ctx, `/tmp/demo`, "demo")
	if err != nil {
		t.Fatalf("CreateRepository: %v", err)
	}
	got, err := s.GetRepositoryByPath(ctx, `/tmp/demo`)
	if err != nil || got.ID != repo.ID {
		t.Fatalf("GetRepositoryByPath: %+v %v", got, err)
	}

	req, err := s.CreateRequirement(ctx, "R1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCriterion(ctx, req.ID, "c"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(ctx, req.ID, "t", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRequirement(ctx, req.ID); err != nil {
		t.Fatal(err)
	}
	criteria, err := s.ListCriteriaByRequirement(ctx, req.ID)
	if err != nil || len(criteria) != 0 {
		t.Fatalf("cascade criteria: %v %d", err, len(criteria))
	}
	tasks, err := s.ListTasksByRequirement(ctx, req.ID)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("cascade tasks: %v %d", err, len(tasks))
	}

	repos, err := s.ListRepositories(ctx)
	if err != nil || len(repos) != 1 {
		t.Fatalf("repos: %v %d", err, len(repos))
	}
}

func TestDeletingCriterionAndTaskUnlinksHistoricalEvidence(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	req, err := s.CreateRequirement(ctx, "Preserve audit evidence", "")
	if err != nil {
		t.Fatal(err)
	}
	criterion, err := s.CreateCriterion(ctx, req.ID, "verification passes")
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask(ctx, req.ID, "Implement", "")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := s.CreateEvidence(ctx, models.Evidence{RequirementID: req.ID, CriterionID: criterion.ID, TaskID: task.ID, Kind: "test", Title: "go test", Status: "passed"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := s.CreateVerificationRun(ctx, models.VerificationRun{RequirementID: req.ID, CriterionID: criterion.ID, Name: "go test", Command: "go test ./...", Status: "passed", StartedAt: now, CompletedAt: now, EvidenceID: evidence.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAgentSession(ctx, models.AgentSession{TaskID: task.ID, Provider: "kimi", Prompt: "review", WorkingDir: t.TempDir(), Status: "completed"}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteCriterion(ctx, criterion.ID); err != nil {
		t.Fatal(err)
	}
	evidenceList, err := s.ListEvidence(ctx, req.ID)
	if err != nil || len(evidenceList) != 1 || evidenceList[0].CriterionID != "" || evidenceList[0].TaskID != task.ID {
		t.Fatalf("evidence after criterion deletion: %+v err=%v", evidenceList, err)
	}
	runs, err := s.ListVerificationRuns(ctx, req.ID)
	if err != nil || len(runs) != 1 || runs[0].CriterionID != "" {
		t.Fatalf("runs after criterion deletion: %+v err=%v", runs, err)
	}

	if err := s.DeleteTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	evidenceList, err = s.ListEvidence(ctx, req.ID)
	if err != nil || len(evidenceList) != 1 || evidenceList[0].TaskID != "" {
		t.Fatalf("evidence after task deletion: %+v err=%v", evidenceList, err)
	}
	sessions, err := s.ListAgentSessions(ctx, task.ID)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("sessions after task deletion: %+v err=%v", sessions, err)
	}
}
