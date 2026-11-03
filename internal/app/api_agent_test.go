package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAPISourcePathAndSnapshotAreBounded(t *testing.T) {
	a, m := teamAppFixture(t)
	_ = a
	ctx := context.Background()
	for _, path := range []string{"../escape", "/tmp/outside", "C:/outside", ".git/config", ".env", "config/credentials.json", "auth/token.json", "id_ed25519", "docs/../.git/config"} {
		if _, e := safeSourcePath(m.Repository, path); e == nil {
			t.Fatal("unsafe source path allowed", path)
		}
	}
	for i := 0; i < 130; i++ {
		name := strings.Repeat("x", 100) + strings.Repeat("a", i%20) + string(rune('a'+i/20)) + ".txt"
		if e := os.WriteFile(filepath.Join(m.Repository, name), nil, 0600); e != nil {
			t.Fatal(e)
		}
	}
	missionGit(ctx, m.Repository, "add", ".")
	snapshot, e := sourceSnapshot(ctx, m.Repository, 1800)
	if e != nil {
		t.Fatal(e)
	}
	var files map[string]string
	if e = json.Unmarshal([]byte(snapshot), &files); e != nil {
		t.Fatal(e)
	}
	if len(snapshot) > 1800 || len(files) > 100 {
		t.Fatalf("snapshot metadata escaped cap: bytes=%d files=%d", len(snapshot), len(files))
	}
	if runtime.GOOS != "windows" {
		outside := t.TempDir()
		link := filepath.Join(m.Repository, "linked")
		if e = os.Symlink(outside, link); e != nil {
			t.Fatal(e)
		}
		if _, e = safeSourcePath(m.Repository, "linked/stolen"); e == nil {
			t.Fatal("symlink write path escaped")
		}
	}
}
func TestAPIAgentRejectsWholeInvalidEditBatchBeforeWriting(t *testing.T) {
	a, input, _ := productFixture(t)
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := `{"summary":"try edits","edits":[{"path":"README.md","content":"changed"},{"path":"../escape.md","content":"outside"}],"findings":[]}`
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10}})
	}))
	defer server.Close()
	config, _ := a.Store.GetAIProvider(ctx, "fixture-api")
	config.BaseURL = server.URL
	config.APIPath = "/chat/completions"
	a.Store.SaveAIProvider(ctx, config)
	req, _ := a.CreateRequirement(ctx, "Batch validation", "test")
	task, _ := a.CreateTask(ctx, req.ID, "Lead task", "")
	task, _, e := a.LinkTaskToWorktree(ctx, input.Repository, task.ID, "api-edit-test", filepath.Join(t.TempDir(), "worktree"))
	if e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(filepath.Join(task.WorktreePath, "README.md"))
	if e != nil {
		t.Fatal(e)
	}
	session, e := a.startAgentSessionModel(context.WithValue(ctx, apiRoleKey{}, "lead"), task.ID, "api-fixture-api", "fixture-model", "Mission fixture. Role: lead. Inspect README")
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(10 * time.Second)
	for session.Status == "running" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		session, _ = a.Store.GetAgentSession(ctx, session.ID)
	}
	if session.Status != "failed" {
		t.Fatalf("invalid edit batch session %s", session.Status)
	}
	after, e := os.ReadFile(filepath.Join(task.WorktreePath, "README.md"))
	if e != nil || string(after) != string(before) {
		t.Fatal("earlier valid edit applied before later invalid edit refusal")
	}
	if _, e = a.AgentSessionResponse(ctx, session.ID); e == nil {
		t.Fatal("failed API edit published a complete response")
	}
}
func TestAPISessionWithoutControllerRoleCannotSendRequest(t *testing.T) {
	a, input, _ := productFixture(t)
	ctx := context.Background()
	req, _ := a.CreateRequirement(ctx, "No role", "test")
	task, _ := a.CreateTask(ctx, req.ID, "Test", "")
	task, _, e := a.LinkTaskToWorktree(ctx, input.Repository, task.ID, "no-role", filepath.Join(t.TempDir(), "worktree"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.startAgentSessionModel(ctx, task.ID, "api-fixture-api", "fixture-model", "Role: lead."); e == nil {
		t.Fatal("public prompt self-granted API controller role")
	}
	budget, _ := a.ProviderBudget(ctx, "fixture-api")
	if budget.Calls != 0 {
		t.Fatal("unauthorized role spent request")
	}
}
