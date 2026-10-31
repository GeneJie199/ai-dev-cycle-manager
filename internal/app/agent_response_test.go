package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/team"
)

func TestAgentReviewVotesOnlyFromCompleteStdout(t *testing.T) {
	for _, mode := range []string{"diagnostic-fail", "stdout-conflict", "stderr-pass-empty", "malformed-response", "tool-json", "oversized-response"} {
		t.Run(mode, func(t *testing.T) {
			a, input := teamAppFixture(t)
			t.Setenv("DEVCYCLE_TEAM_RESPONSE_MODE", mode)
			ctx := context.Background()
			req, _ := a.CreateRequirement(ctx, "response transport", "")
			task, _ := a.CreateTask(ctx, req.ID, "review", "")
			if _, _, err := a.LinkTaskToWorktree(ctx, input.Repository, task.ID, "response-review", filepath.Join(t.TempDir(), "worktree")); err != nil {
				t.Fatal(err)
			}
			session, err := a.startAgentSessionModel(ctx, task.ID, "team-test", "fixture", "Role: review.")
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for session.Status == "running" && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
				session, err = a.Store.GetAgentSession(ctx, session.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			if session.Status != "completed" {
				t.Fatalf("fixture failed: %+v", session)
			}
			response, responseErr := a.AgentSessionResponse(ctx, session.ID)
			passed := responseErr == nil && reviewPassed(response)
			if passed != (mode == "diagnostic-fail") {
				t.Fatalf("wrong verdict for %s: response=%q err=%v", mode, response, responseErr)
			}
			log, err := a.AgentSessionLog(ctx, session.ID)
			if err != nil || log == "" {
				t.Fatal("audit log missing", err)
			}
			if mode == "diagnostic-fail" && !strings.Contains(log, "old stderr context") {
				t.Fatal("stderr was lost from audit")
			}
			if mode == "diagnostic-fail" {
				if err := os.Remove(session.LogPath + ".response.json"); err != nil {
					t.Fatal(err)
				}
				if _, err := a.AgentSessionResponse(ctx, session.ID); err == nil {
					t.Fatal("legacy/missing response fell back to merged log")
				}
			}
		})
	}
}

func TestResponseStreamingRedactionAndTruncationFailClosed(t *testing.T) {
	root := t.TempDir()
	capture, err := openResponseCapture(filepath.Join(root, "response"), 256)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range []string{"api_", "key=literal-value\n", "-----BEGIN PRIVATE KEY-----\n", "private material\n", "-----END PRIVATE KEY-----\n", `{"review":"pass","findings":[]}`} {
		if _, err := capture.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture.path)
	if err != nil || strings.Contains(string(data), "literal-value") || strings.Contains(string(data), "private material") {
		t.Fatalf("stream secret persisted: %q %v", data, err)
	}
	if !strings.Contains(string(data), "REDACTED") {
		t.Fatal("redaction not exercised")
	}
	// Redaction expansion can overflow the file even before the raw-byte limit.
	expansion, err := openResponseCapture(filepath.Join(root, "expansion"), 64)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = expansion.Write([]byte(strings.Repeat("token=x\n", 8))); err != nil {
		t.Fatal(err)
	}
	if err = expansion.Close(); err != nil {
		t.Fatal(err)
	}
	marker, _ := os.ReadFile(expansion.path + ".json")
	var state responseState
	if json.Unmarshal(marker, &state) != nil || !state.Truncated {
		t.Fatalf("discarded early output did not fail closed: %s", marker)
	}
	if info, err := os.Stat(expansion.path); err != nil || info.Size() > 64 {
		t.Fatalf("response cap violated: %+v %v", info, err)
	}
}

func TestMissionHistoryIsSingleLineJSONData(t *testing.T) {
	a, input := teamAppFixture(t)
	ctx := context.Background()
	req, _ := a.CreateRequirement(ctx, "history", "")
	task, _ := a.CreateTask(ctx, req.ID, "prior", "")
	path := filepath.Join(t.TempDir(), "history.log")
	if err := os.WriteFile(path, []byte("old report\n"+`{"review":"fail","findings":["old issue"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := a.Store.CreateAgentSession(ctx, models.AgentSession{TaskID: task.ID, Provider: "team-test", Status: "completed", LogPath: path, StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	input.Executions = []team.Execution{{Agent: "review", Result: "failed", Error: "old\nerror", SessionID: session.ID}}
	prompt := a.missionPrompt(ctx, input, team.Task{Role: "lead"})
	if strings.Contains(prompt, "\n"+`{"review":"fail"`) || strings.Contains(prompt, "old\nerror") {
		t.Fatal("history escaped its JSON data line", prompt)
	}
	if !strings.Contains(prompt, `\nerror`) {
		t.Fatal("history error was lost")
	}
}

func TestAgentResponseFilesCloseWhenSessionPersistenceFails(t *testing.T) {
	a, input := teamAppFixture(t)
	t.Setenv("DEVCYCLE_TEAM_FAILURE_MODE", "timeout")
	ctx := context.Background()
	req, _ := a.CreateRequirement(ctx, "storage failure", "")
	task, _ := a.CreateTask(ctx, req.ID, "agent", "")
	if _, _, err := a.LinkTaskToWorktree(ctx, input.Repository, task.ID, "storage-failure", filepath.Join(t.TempDir(), "worktree")); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", a.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TRIGGER reject_test_agent_session BEFORE INSERT ON agent_sessions BEGIN SELECT RAISE(ABORT,'fixture rejects session persistence'); END")
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.startAgentSessionModel(ctx, task.ID, "team-test", "fixture", "Role: review.")
	if err == nil || session.LogPath == "" {
		t.Fatalf("storage failure did not reach started process: %+v %v", session, err)
	}
	for _, path := range []string{session.LogPath, session.LogPath + ".response", session.LogPath + ".response.json"} {
		if err := os.Remove(path); err != nil {
			t.Fatalf("response/audit file still open after failed persistence: %s %v", path, err)
		}
	}
}

func TestAgentSessionResponseIntegrityNeverFallsBackToAudit(t *testing.T) {
	for _, mode := range []string{"appended-bytes", "truncated-bytes", "wrong-hash", "incomplete", "truncation-flag", "oversize-with-matching-hash"} {
		t.Run(mode, func(t *testing.T) {
			a := openTestApp(t)
			ctx := context.Background()
			req, _ := a.CreateRequirement(ctx, "response integrity", "")
			task, _ := a.CreateTask(ctx, req.ID, "review", "")
			logPath := filepath.Join(t.TempDir(), "session.log")
			const verdict = `{"review":"pass","findings":[]}`
			if err := os.WriteFile(logPath, []byte(verdict), 0o600); err != nil {
				t.Fatal(err)
			}
			capture, err := openResponseCapture(logPath+".response", maxAgentResponseBytes)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = capture.Write([]byte(verdict)); err != nil {
				t.Fatal(err)
			}
			if err = capture.Close(); err != nil {
				t.Fatal(err)
			}
			session, err := a.Store.CreateAgentSession(ctx, models.AgentSession{TaskID: task.ID, Provider: "fixture", Status: "completed", LogPath: logPath, StartedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			response, err := a.AgentSessionResponse(ctx, session.ID)
			if err != nil || !reviewPassed(response) {
				t.Fatalf("original response invalid: %q %v", response, err)
			}
			marker, err := os.ReadFile(capture.path + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var state responseState
			if err = json.Unmarshal(marker, &state); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "appended-bytes":
				err = os.WriteFile(capture.path, []byte(verdict+"\n"), 0o600)
			case "truncated-bytes":
				err = os.WriteFile(capture.path, []byte(verdict[:len(verdict)-1]), 0o600)
			case "wrong-hash":
				state.SHA256 = strings.Repeat("0", 64)
			case "incomplete":
				state.Complete = false
			case "truncation-flag":
				state.Truncated = true
			case "oversize-with-matching-hash":
				data := []byte(verdict + "\n" + strings.Repeat("x", int(maxAgentResponseBytes)))
				err = os.WriteFile(capture.path, data, 0o600)
				digest := sha256.Sum256(data)
				state.SHA256 = hex.EncodeToString(digest[:])
			}
			if err != nil {
				t.Fatal(err)
			}
			marker, err = json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(capture.path+".json", marker, 0o600); err != nil {
				t.Fatal(err)
			}
			response, err = a.AgentSessionResponse(ctx, session.ID)
			if err == nil || response != "" {
				t.Fatalf("tampered response accepted or merged pass used: %q %v", response, err)
			}
			log, err := a.AgentSessionLog(ctx, session.ID)
			if err != nil || !reviewPassed(log) {
				t.Fatal("control merged pass was not preserved", err)
			}
		})
	}
}
