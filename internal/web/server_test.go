package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/app"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	a, err := app.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	srv, err := NewServer(a, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		ts.Close()
		_ = a.Close()
	})
	return ts
}

func doJSON(t *testing.T, method, url string, body any, wantStatus int) map[string]any {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	if res.StatusCode != wantStatus {
		t.Fatalf("%s %s: status=%d want=%d body=%v", method, url, res.StatusCode, wantStatus, out)
	}
	return out
}

func getJSONArray(t *testing.T, url string) []any {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status=%d", url, res.StatusCode)
	}
	var out []any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStaticIndex(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if res.Header.Get("X-Frame-Options") != "DENY" || res.Header.Get("Content-Security-Policy") == "" || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("static response missing security headers")
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(res.Body)
	if !bytes.Contains(buf.Bytes(), []byte("DevCycle")) {
		t.Fatal("index page missing title")
	}
	for _, asset := range []string{"/app.js", "/style.css", "/suite.js"} {
		res, err := http.Get(ts.URL + asset)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status=%d", asset, res.StatusCode)
		}
	}
}

func TestRequirementCriteriaTaskFlow(t *testing.T) {
	ts := newTestServer(t)

	// Validation errors.
	doJSON(t, "POST", ts.URL+"/api/requirements", map[string]any{"title": ""}, http.StatusBadRequest)
	doJSON(t, "POST", ts.URL+"/api/requirements", map[string]any{"title": strings.Repeat("x", 301)}, http.StatusBadRequest)
	doJSON(t, "POST", ts.URL+"/api/tasks", map[string]any{"title": "x"}, http.StatusBadRequest)

	req := doJSON(t, "POST", ts.URL+"/api/requirements",
		map[string]any{"title": "REQ-1", "description": "first"}, http.StatusCreated)
	reqID := req["id"].(string)

	reqs := getJSONArray(t, ts.URL+"/api/requirements")
	if len(reqs) != 1 {
		t.Fatalf("requirements len=%d", len(reqs))
	}

	crit := doJSON(t, "POST", ts.URL+"/api/requirements/"+reqID+"/criteria",
		map[string]any{"description": "it works"}, http.StatusCreated)
	critID := crit["id"].(string)
	if crit["satisfied"].(bool) {
		t.Fatal("criterion should start unsatisfied")
	}

	crit = doJSON(t, "PATCH", ts.URL+"/api/criteria/"+critID,
		map[string]any{"satisfied": true, "evidenceTitle": "manual browser verification", "evidenceNote": "verified in test"}, http.StatusOK)
	if !crit["satisfied"].(bool) {
		t.Fatal("criterion not updated")
	}

	task := doJSON(t, "POST", ts.URL+"/api/tasks",
		map[string]any{"requirementId": reqID, "title": "do it"}, http.StatusCreated)
	taskID := task["id"].(string)
	if task["status"].(string) != string(models.TaskStatusTodo) {
		t.Fatalf("status=%v", task["status"])
	}

	doJSON(t, "PATCH", ts.URL+"/api/tasks/"+taskID,
		map[string]any{"status": "bogus"}, http.StatusBadRequest)
	task = doJSON(t, "PATCH", ts.URL+"/api/tasks/"+taskID,
		map[string]any{"status": "done"}, http.StatusOK)
	if task["status"].(string) != "done" {
		t.Fatalf("status=%v", task["status"])
	}

	tasks := getJSONArray(t, ts.URL+"/api/tasks?requirementId="+reqID)
	if len(tasks) != 1 {
		t.Fatalf("tasks len=%d", len(tasks))
	}

	// Release candidate should be ready now.
	rc := doJSON(t, "GET", ts.URL+"/api/requirements/"+reqID+"/release-candidate", nil, http.StatusOK)
	if rc["spec"] != app.ReleaseCandidateSpec {
		t.Fatalf("spec=%v", rc["spec"])
	}
	readiness := rc["readiness"].(map[string]any)
	if !readiness["ready"].(bool) {
		t.Fatalf("readiness=%v", readiness)
	}

	// Unknown requirement -> 404.
	doJSON(t, "GET", ts.URL+"/api/requirements/nope/release-candidate", nil, http.StatusNotFound)

	updated := doJSON(t, "PATCH", ts.URL+"/api/requirements/"+reqID,
		map[string]any{"title": "REQ-1 updated", "description": "revised"}, http.StatusOK)
	if updated["title"] != "REQ-1 updated" {
		t.Fatalf("updated requirement = %v", updated)
	}
	doJSON(t, "DELETE", ts.URL+"/api/tasks/"+taskID, nil, http.StatusNoContent)
	doJSON(t, "DELETE", ts.URL+"/api/criteria/"+critID, nil, http.StatusNoContent)
	doJSON(t, "DELETE", ts.URL+"/api/requirements/"+reqID, nil, http.StatusNoContent)
	if got := getJSONArray(t, ts.URL+"/api/requirements"); len(got) != 0 {
		t.Fatalf("requirements after delete = %v", got)
	}
}

func TestCriterionAndTaskEditingAPI(t *testing.T) {
	ts := newTestServer(t)
	requirement := doJSON(t, "POST", ts.URL+"/api/requirements", map[string]any{"title": "editable workflow"}, http.StatusCreated)
	requirementID := requirement["id"].(string)
	criterion := doJSON(t, "POST", ts.URL+"/api/requirements/"+requirementID+"/criteria", map[string]any{"description": "original acceptance"}, http.StatusCreated)
	updatedCriterion := doJSON(t, "PATCH", ts.URL+"/api/criteria/"+criterion["id"].(string), map[string]any{"description": "updated acceptance evidence"}, http.StatusOK)
	if updatedCriterion["description"] != "updated acceptance evidence" {
		t.Fatalf("criterion = %v", updatedCriterion)
	}
	first := doJSON(t, "POST", ts.URL+"/api/tasks", map[string]any{"requirementId": requirementID, "title": "first task"}, http.StatusCreated)
	second := doJSON(t, "POST", ts.URL+"/api/tasks", map[string]any{"requirementId": requirementID, "title": "second task"}, http.StatusCreated)
	updatedTask := doJSON(t, "PATCH", ts.URL+"/api/tasks/"+second["id"].(string), map[string]any{"title": "second task edited", "description": "depends on first", "dependsOn": []string{first["id"].(string)}}, http.StatusOK)
	if updatedTask["title"] != "second task edited" || len(updatedTask["dependsOn"].([]any)) != 1 {
		t.Fatalf("task = %v", updatedTask)
	}
	doJSON(t, "PATCH", ts.URL+"/api/tasks/"+second["id"].(string), map[string]any{"status": "in_progress"}, http.StatusBadRequest)
}

func TestAIPlanProviderAndApplyAPI(t *testing.T) {
	ts := newTestServer(t)
	providers := getJSONArray(t, ts.URL+"/api/ai/providers")
	if len(providers) != 3 {
		t.Fatalf("providers=%v", providers)
	}
	for _, value := range providers {
		provider := value.(map[string]any)
		if _, leaked := provider["binary"]; leaked {
			t.Fatalf("provider leaked executable path: %v", provider)
		}
	}
	requirement := doJSON(t, "POST", ts.URL+"/api/requirements", map[string]any{"title": "AI plan"}, http.StatusCreated)
	requirementID := requirement["id"].(string)
	applied := doJSON(t, "POST", ts.URL+"/api/requirements/"+requirementID+"/ai-plan/apply", map[string]any{
		"criteria": []map[string]any{{"description": "接口响应包含可核验版本号", "rationale": "API test"}},
		"tasks": []map[string]any{
			{"title": "实现版本接口", "description": "返回版本"},
			{"title": "测试版本接口", "description": "断言响应", "dependsOn": []string{"实现版本接口"}},
		},
	}, http.StatusCreated)
	tasks := applied["tasks"].([]any)
	firstID := tasks[0].(map[string]any)["id"]
	dependencies := tasks[1].(map[string]any)["dependsOn"].([]any)
	if len(dependencies) != 1 || dependencies[0] != firstID {
		t.Fatalf("tasks=%v", tasks)
	}
	doJSON(t, "POST", ts.URL+"/api/requirements/"+requirementID+"/ai-plan/apply", map[string]any{}, http.StatusBadRequest)
}

// initTempRepo creates a real git repository with one commit; skips if git is missing.
func initTempRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial commit")
	return dir
}

func TestRepositoryAndGitEndpoints(t *testing.T) {
	ts := newTestServer(t)
	repoDir := initTempRepo(t)

	// Importing a non-repo path fails with 400.
	doJSON(t, "POST", ts.URL+"/api/repositories",
		map[string]any{"path": t.TempDir()}, http.StatusBadRequest)

	repo := doJSON(t, "POST", ts.URL+"/api/repositories",
		map[string]any{"path": repoDir}, http.StatusCreated)
	repoPath := repo["path"].(string)

	repos := getJSONArray(t, ts.URL+"/api/repositories")
	if len(repos) != 1 {
		t.Fatalf("repos len=%d", len(repos))
	}

	q := url.QueryEscape(repoPath)
	st := doJSON(t, "GET", ts.URL+"/api/git/status?repo="+q, nil, http.StatusOK)
	if st["branch"] == "" || !st["clean"].(bool) {
		t.Fatalf("status=%v", st)
	}

	// Make a change so the diff is non-empty.
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# test\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff := doJSON(t, "GET", ts.URL+"/api/git/diff?repo="+q+"&stat=1", nil, http.StatusOK)
	if diff["content"] == "" {
		t.Fatal("expected non-empty diff --stat")
	}
	structured := doJSON(t, "GET", ts.URL+"/api/git/structured-diff?repo="+q, nil, http.StatusOK)
	if structured["totalAdditions"].(float64) != 1 || len(structured["files"].([]any)) != 1 {
		t.Fatalf("structured diff=%v", structured)
	}
	doJSON(t, "GET", ts.URL+"/api/git/structured-diff?repo="+q+"&to=HEAD", nil, http.StatusBadRequest)

	branches := getJSONArray(t, ts.URL+"/api/git/branches?repo="+q)
	if len(branches) == 0 {
		t.Fatal("expected at least one branch")
	}

	commits := getJSONArray(t, ts.URL+"/api/git/log?repo="+q)
	if len(commits) != 1 {
		t.Fatalf("commits len=%d", len(commits))
	}

	// Missing repo param -> 400.
	doJSON(t, "GET", ts.URL+"/api/git/status", nil, http.StatusBadRequest)

	requirement := doJSON(t, "POST", ts.URL+"/api/requirements",
		map[string]any{"title": "worktree flow"}, http.StatusCreated)
	task := doJSON(t, "POST", ts.URL+"/api/tasks",
		map[string]any{"requirementId": requirement["id"], "title": "isolated change"}, http.StatusCreated)
	worktreePath := filepath.Join(t.TempDir(), "feature-worktree")
	linked := doJSON(t, "POST", ts.URL+"/api/tasks/"+task["id"].(string)+"/worktree",
		map[string]any{"repositoryPath": repoPath, "branch": "feature/web-test", "worktreePath": worktreePath}, http.StatusCreated)
	linkedTask := linked["task"].(map[string]any)
	if linkedTask["branch"] != "feature/web-test" || linkedTask["worktreePath"] == "" {
		t.Fatalf("linked task = %v", linkedTask)
	}
	doJSON(t, "DELETE", ts.URL+"/api/repositories/"+repo["id"].(string), nil, http.StatusNoContent)
	if got := getJSONArray(t, ts.URL+"/api/repositories"); len(got) != 0 {
		t.Fatalf("repositories after delete = %v", got)
	}
}

func TestLoopbackEnforcement(t *testing.T) {
	a, err := app.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	srv, err := NewServer(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.ListenAndServe("0.0.0.0:0"); err == nil {
		t.Fatal("expected refusal for non-loopback address")
	}
	// Bad addr should also error.
	if err := srv.ListenAndServe("not-an-addr"); err == nil {
		t.Fatal("expected error for malformed address")
	}
}

func TestVerificationRunAPIStartsAndStopsAsynchronously(t *testing.T) {
	ts := newTestServer(t)
	requirement := doJSON(t, "POST", ts.URL+"/api/requirements", map[string]any{"title": "async API verification"}, http.StatusCreated)
	command := "sleep 30"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 30"
	}
	run := doJSON(t, "POST", ts.URL+"/api/requirements/"+requirement["id"].(string)+"/runs", map[string]any{
		"name": "long smoke", "command": command, "workingDir": t.TempDir(), "timeoutSeconds": 60,
	}, http.StatusAccepted)
	if run["status"] != "running" || run["id"] == "" {
		t.Fatalf("started run = %v", run)
	}
	stopping := doJSON(t, "POST", ts.URL+"/api/verification-runs/"+run["id"].(string)+"/stop", map[string]any{}, http.StatusAccepted)
	if stopping["status"] != "stopping" {
		t.Fatalf("stopping run = %v", stopping)
	}
}
