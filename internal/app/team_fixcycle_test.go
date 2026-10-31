package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTeamFixCyclePreservesIdentityAndProducesReadyCandidate(t *testing.T) {
	for _, name := range []string{"review-fix", "qa-failure-then-review-fix"} {
		t.Run(name, func(t *testing.T) {
			a, input := teamAppFixture(t)
			t.Setenv("DEVCYCLE_TEAM_ECHO_PROMPT", "1")
			input.Policy.CommitTaskChanges = true
			input.Checks = []string{`if [ "$DEVCYCLE_MACHINE_CHECK_FAIL" = "1" ]; then exit 1; fi; git status --porcelain`}
			if runtime.GOOS == "windows" {
				input.Checks = []string{`if ($env:DEVCYCLE_MACHINE_CHECK_FAIL -eq '1') { exit 1 }; git status --porcelain`}
			}
			ctx := context.Background()
			m, err := a.CreateMission(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			ids := map[string]string{}
			advance := func() {
				t.Helper()
				m, err = a.AdvanceMission(ctx, m.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, task := range m.Plan {
					if task.TaskID == "" {
						continue
					}
					if previous := ids[task.ID]; previous != "" && previous != task.TaskID {
						t.Fatal("logical task identity changed across attempts", task.ID)
					}
					ids[task.ID] = task.TaskID
				}
			}
			advance()
			advance()
			if name == "qa-failure-then-review-fix" {
				t.Setenv("DEVCYCLE_MACHINE_CHECK_FAIL", "1")
				advance()
				if m.Executions[len(m.Executions)-1].TestResult != "failed" {
					t.Fatal("real machine check failure was not injected")
				}
				t.Setenv("DEVCYCLE_MACHINE_CHECK_FAIL", "")
			}
			advance()
			t.Setenv("DEVCYCLE_TEAM_REVIEW_FAIL", "1")
			advance()
			if m.FixCycles != 1 {
				t.Fatal("review did not trigger repair")
			}
			t.Setenv("DEVCYCLE_TEAM_REVIEW_FAIL", "")
			t.Setenv("DEVCYCLE_TEAM_WRITE", "README.md")
			t.Setenv("DEVCYCLE_TEAM_WRITE_CONTENT", "# actually repaired source\n")
			for i := 0; i < 5 && m.State != "completed"; i++ {
				advance()
			}
			if m.State != "completed" {
				t.Fatalf("repair did not complete: %+v", m)
			}
			candidate, err := a.ExportReleaseCandidate(ctx, m.RequirementID)
			if err != nil || !candidate.Readiness.Ready || candidate.Readiness.TasksTotal != 4 || candidate.Readiness.TasksDone != 4 {
				t.Fatalf("completed repair candidate not ready: %+v %v", candidate, err)
			}
			worktrees := map[string]bool{}
			failed := 0
			for _, execution := range m.Executions {
				if execution.Worktree == "" || worktrees[execution.Worktree] {
					t.Fatal("attempt reused an artifact worktree", execution)
				}
				worktrees[execution.Worktree] = true
				if execution.Result == "failed" {
					failed++
				}
			}
			if failed < 1 {
				t.Fatal("failed execution history was removed")
			}
			logicalIDs := map[string]bool{}
			attempts := map[string]bool{}
			failedMachine := 0
			for _, proof := range candidate.Evidence {
				if proof.Metadata["verificationRunId"] == "" {
					continue
				}
				if proof.TaskID != ids["tests"] || proof.Metadata["missionId"] != m.ID || proof.Metadata["planTaskId"] != "tests" || proof.Metadata["executionId"] == "" {
					t.Fatal("machine proof lost controller/task identity", proof)
				}
				logicalIDs[proof.Metadata["logicalCheckId"]] = true
				attempts[proof.Metadata["verificationAttempt"]] = true
				if proof.Status == "failed" {
					failedMachine++
				}
			}
			if len(logicalIDs) != 1 || len(attempts) < 2 || (name == "qa-failure-then-review-fix" && failedMachine != 1) {
				t.Fatalf("attempt linkage/history missing: checks=%v attempts=%v failedMachine=%d", logicalIDs, attempts, failedMachine)
			}
			if root := os.Getenv("DEVCYCLE_TEAM_FIX_EVIDENCE"); root != "" {
				root = filepath.Join(root, name)
				if err = os.MkdirAll(root, 0o700); err != nil {
					t.Fatal(err)
				}
				save := func(name string, value any) {
					t.Helper()
					data, err := json.MarshalIndent(value, "", "  ")
					if err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				save("candidate.json", candidate)
				save("mission.json", m)
				if binary := os.Getenv("DEVCYCLE_TEAM_FIX_GUARD"); binary != "" {
					executable, err := os.Executable()
					if err != nil {
						t.Fatal(err)
					}
					save("guard-plan.json", map[string]any{"release_id": "fixed-" + name, "version": "2.0.0-rc.1", "candidate_file": filepath.Join(root, "candidate.json"), "repository": input.Repository, "base_ref": m.Executions[0].Head, "target_ref": m.Executions[len(m.Executions)-1].Head, "expected_files": []string{"README.md"}, "checks": []any{}, "recovery_checks": []any{map[string]any{"name": "executable retained", "type": "file", "path": executable}}, "rollback": []string{"Restore retained executable"}})
					command := exec.Command(binary, "check", "--plan", filepath.Join(root, "guard-plan.json"), "--state", filepath.Join(root, "guard.db"), "--out", filepath.Join(root, "guard-report.json"))
					output, err := command.CombinedOutput()
					if logErr := os.WriteFile(filepath.Join(root, "guard-command.log"), output, 0o600); logErr != nil {
						t.Fatal(logErr)
					}
					if err != nil {
						t.Fatalf("repaired candidate Guard rejected: %v %s", err, output)
					}
				}
			}
		})
	}
}
