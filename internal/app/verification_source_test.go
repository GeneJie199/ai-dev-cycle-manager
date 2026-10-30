package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

func TestVerificationCommandsCannotChangeTheProvenSource(t *testing.T) {
	for _, mode := range []string{"sync-dirty", "sync-commit", "async-dirty", "async-commit"} {
		t.Run(mode, func(t *testing.T) {
			a := openTestApp(t)
			ctx := context.Background()
			repo := initReleaseRepo(t)
			req, _ := a.CreateRequirement(ctx, "bound source", "")
			command := "printf changed > main.go"
			if runtime.GOOS == "windows" {
				command = "Set-Content main.go changed"
			}
			commit := mode == "sync-commit" || mode == "async-commit"
			if commit {
				command += "; git add main.go; git -c user.name=Test -c user.email=test@example.invalid commit -qm changed"
			}
			var run models.VerificationRun
			var err error
			if mode == "async-dirty" || mode == "async-commit" {
				run, err = a.StartVerification(ctx, req.ID, "", "source mutation", command, repo, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				run = waitForVerificationStatus(t, a, req.ID, run.ID, map[string]bool{"failed": true, "passed": true})
			} else {
				run, err = a.RunVerification(ctx, req.ID, "", "source mutation", command, repo, time.Minute)
				if err == nil {
					t.Fatal("source mutation returned success")
				}
			}
			if run.Status != "failed" || run.ExitCode != 0 {
				t.Fatalf("source-changing command passed: %+v", run)
			}
			proofs, err := a.ListEvidence(ctx, req.ID)
			if err != nil || len(proofs) != 1 {
				t.Fatalf("proofs=%+v err=%v", proofs, err)
			}
			proof := proofs[0]
			meta := proof.Metadata
			if proof.Status != "failed" || meta["sourceVerified"] != "false" || meta["sourceClean"] != "true" || len(meta["sourceCommit"]) != 40 || meta["verificationRunId"] != run.ID {
				t.Fatalf("false source proof: %+v", proof)
			}
			if commit && (meta["sourceCommit"] == meta["sourceCommitAfter"] || meta["sourceCleanAfter"] != "true") {
				t.Fatal("fixture did not commit a real source change", meta)
			}
			if !commit && meta["sourceCleanAfter"] != "false" {
				t.Fatal("fixture did not leave a real source change", meta)
			}
		})
	}
}

func TestCandidateRejectsStaleMachineProofAndRecoversAfterFreshCheck(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()
	repo := initReleaseRepo(t)
	req, _ := a.CreateRequirement(ctx, "source-bound command", "")
	criterion, _ := a.CreateCriterion(ctx, req.ID, "main.go exists at the candidate commit")
	task, _ := a.CreateTask(ctx, req.ID, "implementation", "")
	if _, err := a.Store.LinkTaskGit(ctx, task.ID, "main", repo); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateTaskStatus(ctx, task.ID, models.TaskStatusDone); err != nil {
		t.Fatal(err)
	}
	check := "git cat-file -e HEAD:main.go"
	run, err := a.RunVerification(ctx, req.ID, criterion.ID, "source test", check, repo, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.UpdateCriterionResult(ctx, criterion.ID, true); err != nil {
		t.Fatal(err)
	}
	assertReady := func(expected bool) {
		t.Helper()
		candidate, err := a.ExportReleaseCandidate(ctx, req.ID)
		if err != nil || candidate.Readiness.Ready != expected || !candidate.Sources[0].Clean {
			t.Fatalf("ready expected=%t candidate=%+v err=%v", expected, candidate, err)
		}
	}
	assertReady(true)
	for _, args := range [][]string{{"rm", "main.go"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "remove checked source"}} {
		if _, err = missionGit(ctx, repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	assertReady(false) // Earlier command passed, current clean HEAD is a different commit.
	if _, err = a.RunVerification(ctx, req.ID, criterion.ID, "source test", check, repo, time.Minute); err == nil {
		t.Fatal("removed file check unexpectedly passed")
	}
	if err = os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n// restored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "main.go"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "restore checked source"}} {
		if _, err = missionGit(ctx, repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	fresh, err := a.RunVerification(ctx, req.ID, criterion.ID, "source test", check, repo, time.Minute)
	if err != nil || fresh.ID == run.ID {
		t.Fatalf("fresh check=%+v err=%v", fresh, err)
	}
	if _, err = a.UpdateCriterionResult(ctx, criterion.ID, true); err != nil {
		t.Fatal(err)
	}
	assertReady(true) // Latest same command supersedes older failed/stale proofs.
}

func TestNonGitVerificationDoesNotClaimSourceBindingAndManualProofCannotForgeIt(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()
	req, _ := a.CreateRequirement(ctx, "non-Git check", "")
	criterion, _ := a.CreateCriterion(ctx, req.ID, "plain command succeeds")
	run, err := a.RunVerification(ctx, req.ID, criterion.ID, "plain command", "echo ok", t.TempDir(), time.Minute)
	if err != nil || run.Status != "passed" {
		t.Fatalf("non-Git command=%+v err=%v", run, err)
	}
	proofs, err := a.ListEvidence(ctx, req.ID)
	if err != nil || len(proofs) != 1 || proofs[0].Metadata["sourceVerified"] != "false" || proofs[0].Metadata["sourceCommit"] != "" {
		t.Fatalf("unbound command fabricated source: %+v %v", proofs, err)
	}
	if _, err = a.AddEvidence(ctx, models.Evidence{RequirementID: req.ID, Kind: "test", Title: "forged proof", Status: "passed", Metadata: map[string]string{"sourceVerified": "true", "verificationRunId": run.ID}}); err == nil {
		t.Fatal("caller forged runtime source attestation")
	}
	if _, err = a.UpdateCriterionResult(ctx, criterion.ID, true); err != nil {
		t.Fatal(err)
	}
	candidate, err := a.ExportReleaseCandidate(ctx, req.ID)
	if err != nil || candidate.Readiness.Ready {
		t.Fatalf("unbound machine check fabricated release readiness: %+v %v", candidate, err)
	}
}

func TestOldExplorerProofCannotCoverNewImplementationHead(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()
	repo := initReleaseRepo(t)
	req, _ := a.CreateRequirement(ctx, "cumulative source coverage", "")
	criterion, _ := a.CreateCriterion(ctx, req.ID, "current source checks pass")
	explorer, _ := a.CreateTask(ctx, req.ID, "explorer baseline", "")
	if _, err := a.Store.LinkTaskGit(ctx, explorer.ID, "main", repo); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateTaskStatus(ctx, explorer.ID, models.TaskStatusDone); err != nil {
		t.Fatal(err)
	}
	check := "git cat-file -e HEAD:main.go"
	if _, err := a.RunVerification(ctx, req.ID, criterion.ID, "source test", check, repo, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateCriterionResult(ctx, criterion.ID, true); err != nil {
		t.Fatal(err)
	}
	implementation, _ := a.CreateTask(ctx, req.ID, "new implementation", "")
	worktree := filepath.Join(t.TempDir(), "implementation")
	if _, _, err := a.LinkTaskToWorktree(ctx, repo, implementation.ID, "source-followup", worktree); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "README.md"), []byte("new implementation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "new implementation"}} {
		if _, err := missionGit(ctx, worktree, args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.UpdateTaskStatus(ctx, implementation.ID, models.TaskStatusDone); err != nil {
		t.Fatal(err)
	}
	candidate, err := a.ExportReleaseCandidate(ctx, req.ID)
	if err != nil || candidate.Readiness.Ready {
		t.Fatalf("old proof covered a newer untested implementation: %+v %v", candidate, err)
	}
	if _, err = a.RunVerification(ctx, req.ID, criterion.ID, "source test", check, worktree, time.Minute); err != nil {
		t.Fatal(err)
	}
	candidate, err = a.ExportReleaseCandidate(ctx, req.ID)
	if err != nil || !candidate.Readiness.Ready {
		t.Fatalf("new final proof did not cover ancestor explorer and final head: %+v %v", candidate, err)
	}
}
