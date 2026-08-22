package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	devagent "github.com/GeneJie199/ai-dev-cycle-manager/internal/agent"
	devai "github.com/GeneJie199/ai-dev-cycle-manager/internal/ai"
	devgit "github.com/GeneJie199/ai-dev-cycle-manager/internal/git"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/google/uuid"
)

func (a *App) AddEvidence(ctx context.Context, e models.Evidence) (models.Evidence, error) {
	if strings.TrimSpace(e.RequirementID) == "" || strings.TrimSpace(e.Kind) == "" || strings.TrimSpace(e.Title) == "" {
		return e, errors.New("requirementId, kind, and title are required")
	}
	kind, allowed := models.NormalizeEvidenceKind(strings.ToLower(strings.TrimSpace(e.Kind)))
	if !allowed {
		return e, fmt.Errorf("unsupported evidence kind %q", e.Kind)
	}
	e.Kind = kind
	e.Title, _ = devai.Redact(strings.TrimSpace(e.Title))
	e.URI, _ = devai.Redact(strings.TrimSpace(e.URI))
	e.Inline, _ = devai.Redact(e.Inline)
	if len(e.Metadata) > 50 {
		return e, errors.New("evidence metadata must not contain more than 50 entries")
	}
	for key, value := range e.Metadata {
		if len(key) > 100 || len(value) > 4096 {
			return e, errors.New("evidence metadata keys or values are too long")
		}
		e.Metadata[key], _ = devai.Redact(value)
	}
	if e.Status == "" {
		e.Status = "passed"
	}
	if e.Status != "passed" && e.Status != "failed" && e.Status != "informational" {
		return e, errors.New("evidence status must be passed, failed, or informational")
	}
	if len(e.Inline) > 16384 {
		e.Inline = e.Inline[:16384] + "\n[truncated]"
	}
	if _, err := a.Store.GetRequirement(ctx, e.RequirementID); err != nil {
		return e, fmt.Errorf("requirement: %w", err)
	}
	if e.CriterionID != "" {
		criterion, err := a.Store.GetCriterion(ctx, e.CriterionID)
		if err != nil {
			return e, fmt.Errorf("criterion: %w", err)
		}
		if criterion.RequirementID != e.RequirementID {
			return e, errors.New("criterion does not belong to requirement")
		}
	}
	if e.TaskID != "" {
		task, err := a.Store.GetTask(ctx, e.TaskID)
		if err != nil {
			return e, fmt.Errorf("task: %w", err)
		}
		if task.RequirementID != e.RequirementID {
			return e, errors.New("task does not belong to requirement")
		}
	}
	return a.Store.CreateEvidence(ctx, e)
}
func (a *App) ListEvidence(ctx context.Context, requirementID string) ([]models.Evidence, error) {
	return a.Store.ListEvidence(ctx, requirementID)
}
func (a *App) ListVerificationRuns(ctx context.Context, requirementID string) ([]models.VerificationRun, error) {
	runs, err := a.Store.ListVerificationRuns(ctx, requirementID)
	if err != nil {
		return nil, err
	}
	for index := range runs {
		runs[index] = a.withVerificationRuntime(runs[index])
	}
	return runs, nil
}

func (a *App) verificationLogPath(id string) string {
	return filepath.Join(filepath.Dir(a.DBPath), "devcycle-verifications", id+".log")
}

func (a *App) withVerificationRuntime(run models.VerificationRun) models.VerificationRun {
	end := run.CompletedAt
	if run.Status == "running" || end.IsZero() {
		end = time.Now().UTC()
		if output, err := readLogTail(a.verificationLogPath(run.ID), 32768); err == nil {
			run.Output = string(output)
		}
	}
	if !run.StartedAt.IsZero() {
		run.DurationMilliseconds = end.Sub(run.StartedAt).Milliseconds()
		if run.DurationMilliseconds < 0 {
			run.DurationMilliseconds = 0
		}
	}
	return run
}

func (a *App) StartVerification(ctx context.Context, requirementID, criterionID, name, command, workingDir string, timeout time.Duration) (models.VerificationRun, error) {
	if requirementID == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(command) == "" {
		return models.VerificationRun{}, errors.New("requirementId, name, and command are required")
	}
	if _, err := a.Store.GetRequirement(ctx, requirementID); err != nil {
		return models.VerificationRun{}, fmt.Errorf("requirement: %w", err)
	}
	if criterionID != "" {
		criterion, err := a.Store.GetCriterion(ctx, criterionID)
		if err != nil {
			return models.VerificationRun{}, fmt.Errorf("criterion: %w", err)
		}
		if criterion.RequirementID != requirementID {
			return models.VerificationRun{}, errors.New("criterion does not belong to requirement")
		}
	}
	abs, err := filepath.Abs(workingDir)
	if err != nil {
		return models.VerificationRun{}, err
	}
	if st, statErr := os.Stat(abs); statErr != nil || !st.IsDir() {
		return models.VerificationRun{}, errors.New("working directory does not exist")
	}
	if timeout <= 0 || timeout > 30*time.Minute {
		timeout = 10 * time.Minute
	}
	id := uuid.NewString()
	logPath := a.verificationLogPath(id)
	if err = os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return models.VerificationRun{}, err
	}
	logFile, err := openRedactingLog(logPath, maxAgentLogBytes)
	if err != nil {
		return models.VerificationRun{}, err
	}
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, "powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.CommandContext(runCtx, "sh", "-c", command)
	}
	cmd.Dir = abs
	prepareManagedCommand(cmd)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err = cmd.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		return models.VerificationRun{}, err
	}
	run := models.VerificationRun{ID: id, RequirementID: requirementID, CriterionID: criterionID, Name: strings.TrimSpace(name), Command: command, WorkingDir: abs, Status: "running", ExitCode: -1, StartedAt: time.Now().UTC().Truncate(time.Second)}
	if run, err = a.Store.CreateVerificationRun(ctx, run); err != nil {
		cancel()
		_ = stopManagedCommand(cmd)
		_ = logFile.Close()
		return run, err
	}
	a.mu.Lock()
	a.verifications[id] = &runningVerification{cmd: cmd, cancel: cancel}
	a.verificationWG.Add(1)
	a.mu.Unlock()
	go a.finishVerification(run, cmd, logFile, runCtx, cancel)
	return a.withVerificationRuntime(run), nil
}

func (a *App) finishVerification(run models.VerificationRun, cmd *exec.Cmd, logFile *redactingLogWriter, runCtx context.Context, cancel context.CancelFunc) {
	defer a.verificationWG.Done()
	defer cancel()
	runErr := cmd.Wait()
	_ = logFile.Close()
	outputBytes, readErr := readLogTail(a.verificationLogPath(run.ID), 32768)
	if readErr == nil {
		run.Output = string(outputBytes)
	}
	run.Status = "passed"
	run.ExitCode = 0
	if runErr != nil {
		run.Status = "failed"
		run.ExitCode = -1
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			run.ExitCode = exitErr.ExitCode()
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			run.Status = "timed_out"
		}
	}
	a.mu.Lock()
	if process := a.verifications[run.ID]; process != nil && process.finalStatus != "" {
		run.Status = process.finalStatus
	}
	delete(a.verifications, run.ID)
	a.mu.Unlock()
	run.CompletedAt = time.Now().UTC().Truncate(time.Second)
	evidenceStatus := "passed"
	if run.Status != "passed" {
		evidenceStatus = "failed"
	}
	evidence, evidenceErr := a.AddEvidence(context.Background(), models.Evidence{RequirementID: run.RequirementID, CriterionID: run.CriterionID, Kind: verificationKind(run.Name), Title: run.Name, Status: evidenceStatus, Inline: run.Output, Metadata: map[string]string{"command": run.Command, "workingDir": run.WorkingDir, "runStatus": run.Status, "exitCode": fmt.Sprintf("%d", run.ExitCode)}})
	if evidenceErr == nil {
		run.EvidenceID = evidence.ID
	} else {
		run.Output += "\n[evidence persistence failed: " + evidenceErr.Error() + "]"
	}
	_, _ = a.Store.UpdateVerificationRun(context.Background(), run)
}

func (a *App) StopVerification(ctx context.Context, id string) (models.VerificationRun, error) {
	a.mu.Lock()
	process := a.verifications[id]
	if process != nil {
		process.finalStatus = "stopped"
	}
	a.mu.Unlock()
	if process == nil {
		return models.VerificationRun{}, errors.New("verification run is not running in this process")
	}
	if err := stopManagedCommand(process.cmd); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return models.VerificationRun{}, fmt.Errorf("stop verification process tree: %w", err)
	}
	process.cancel()
	run, err := a.Store.GetVerificationRun(ctx, id)
	if err != nil {
		return run, err
	}
	run.Status = "stopping"
	return a.withVerificationRuntime(run), nil
}

func (a *App) RunVerification(ctx context.Context, requirementID, criterionID, name, command, workingDir string, timeout time.Duration) (models.VerificationRun, error) {
	if requirementID == "" || name == "" || strings.TrimSpace(command) == "" {
		return models.VerificationRun{}, errors.New("requirementId, name, and command are required")
	}
	if _, err := a.Store.GetRequirement(ctx, requirementID); err != nil {
		return models.VerificationRun{}, fmt.Errorf("requirement: %w", err)
	}
	if criterionID != "" {
		criterion, err := a.Store.GetCriterion(ctx, criterionID)
		if err != nil {
			return models.VerificationRun{}, fmt.Errorf("criterion: %w", err)
		}
		if criterion.RequirementID != requirementID {
			return models.VerificationRun{}, errors.New("criterion does not belong to requirement")
		}
	}
	abs, err := filepath.Abs(workingDir)
	if err != nil {
		return models.VerificationRun{}, err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return models.VerificationRun{}, errors.New("working directory does not exist")
	}
	if timeout <= 0 || timeout > 30*time.Minute {
		timeout = 10 * time.Minute
	}
	started := time.Now().UTC().Truncate(time.Second)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, "powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.CommandContext(runCtx, "sh", "-c", command)
	}
	cmd.Dir = abs
	prepareManagedCommand(cmd)
	commandOutput := &tailBuffer{maximum: 32768}
	cmd.Stdout = commandOutput
	cmd.Stderr = commandOutput
	runErr := cmd.Run()
	output, _ := devai.Redact(commandOutput.String())
	exit := 0
	status := "passed"
	if runErr != nil {
		status = "failed"
		exit = -1
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exit = ee.ExitCode()
		}
	}
	evidence, eErr := a.AddEvidence(ctx, models.Evidence{RequirementID: requirementID, CriterionID: criterionID, Kind: verificationKind(name), Title: name, Status: status, Inline: output, Metadata: map[string]string{"command": command, "workingDir": abs}})
	if eErr != nil {
		return models.VerificationRun{}, eErr
	}
	r := models.VerificationRun{ID: uuid.NewString(), RequirementID: requirementID, CriterionID: criterionID, Name: name, Command: command, WorkingDir: abs, Status: status, ExitCode: exit, Output: output, StartedAt: started, CompletedAt: time.Now().UTC().Truncate(time.Second), EvidenceID: evidence.ID}
	r, err = a.Store.CreateVerificationRun(ctx, r)
	if err != nil {
		return r, err
	}
	if runErr != nil {
		return r, fmt.Errorf("verification failed with exit code %d", exit)
	}
	return a.withVerificationRuntime(r), nil
}
func verificationKind(name string) string {
	n := strings.ToLower(name)
	if strings.Contains(n, "test") {
		return models.EvidenceKindTest
	}
	return models.EvidenceKindCommand
}

func (a *App) StartAgentSession(ctx context.Context, taskID, provider, prompt string) (models.AgentSession, error) {
	task, err := a.Store.GetTask(ctx, taskID)
	if err != nil {
		return models.AgentSession{}, err
	}
	if task.WorktreePath == "" {
		return models.AgentSession{}, errors.New("task must be linked to a worktree before starting an agent")
	}
	prompt, _ = devai.Redact(strings.TrimSpace(prompt))
	if prompt == "" {
		return models.AgentSession{}, errors.New("prompt is required")
	}
	adapter, err := a.GetAgentAdapter(ctx, provider)
	if err != nil {
		return models.AgentSession{}, fmt.Errorf("agent adapter: %w", err)
	}
	if !adapter.Enabled {
		return models.AgentSession{}, errors.New("agent adapter is disabled")
	}
	binary, args, err := devagent.BuildCommand(adapter, prompt)
	if err != nil {
		return models.AgentSession{}, err
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return models.AgentSession{}, fmt.Errorf("%s executable not found", binary)
	}
	id := uuid.NewString()
	logDir := filepath.Join(filepath.Dir(a.DBPath), "devcycle-sessions")
	if err = os.MkdirAll(logDir, 0o700); err != nil {
		return models.AgentSession{}, err
	}
	logPath := filepath.Join(logDir, id+".log")
	logFile, err := openRedactingLog(logPath, maxAgentLogBytes)
	if err != nil {
		return models.AgentSession{}, err
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = task.WorktreePath
	prepareManagedCommand(cmd)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err = cmd.Start(); err != nil {
		logFile.Close()
		return models.AgentSession{}, err
	}
	session := models.AgentSession{ID: id, TaskID: taskID, Provider: adapter.ID, Prompt: prompt, WorkingDir: task.WorktreePath, Status: "running", PID: cmd.Process.Pid, LogPath: logPath, StartedAt: time.Now().UTC().Truncate(time.Second)}
	if session, err = a.Store.CreateAgentSession(ctx, session); err != nil {
		_ = cmd.Process.Kill()
		logFile.Close()
		return session, err
	}
	a.mu.Lock()
	a.processes[id] = &runningAgent{cmd: cmd}
	a.agentWG.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.agentWG.Done()
		err := cmd.Wait()
		_ = logFile.Close()
		status := "completed"
		if err != nil {
			status = "failed"
		}
		a.mu.Lock()
		if process := a.processes[id]; process != nil && process.finalStatus != "" {
			status = process.finalStatus
		}
		delete(a.processes, id)
		a.mu.Unlock()
		ended := time.Now().UTC().Truncate(time.Second)
		_, _ = a.Store.UpdateAgentSession(context.Background(), id, status, 0, &ended)
	}()
	return withAgentSessionMetrics(session), nil
}

func providerCommand(provider, prompt string) (string, []string, error) {
	adapter, ok := devagent.BuiltIn(provider)
	if !ok {
		return "", nil, errors.New("provider must be a built-in agent adapter")
	}
	return devagent.BuildCommand(adapter, prompt)
}

func (a *App) StopAgentSession(ctx context.Context, id string) (models.AgentSession, error) {
	a.mu.Lock()
	process := a.processes[id]
	if process != nil {
		process.finalStatus = "stopped"
	}
	a.mu.Unlock()
	if process == nil || process.cmd.Process == nil {
		return models.AgentSession{}, errors.New("agent session is not running in this process")
	}
	if err := stopManagedCommand(process.cmd); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return models.AgentSession{}, fmt.Errorf("stop agent session: %w", err)
	}
	ended := time.Now().UTC().Truncate(time.Second)
	session, err := a.Store.UpdateAgentSession(ctx, id, "stopped", 0, &ended)
	return withAgentSessionMetrics(session), err
}
func (a *App) ListAgentSessions(ctx context.Context, taskID string) ([]models.AgentSession, error) {
	sessions, err := a.Store.ListAgentSessions(ctx, taskID)
	if err != nil {
		return nil, err
	}
	for index := range sessions {
		sessions[index] = withAgentSessionMetrics(sessions[index])
	}
	return sessions, nil
}

func withAgentSessionMetrics(session models.AgentSession) models.AgentSession {
	ended := session.EndedAt
	if ended.IsZero() {
		ended = time.Now().UTC()
	}
	if !session.StartedAt.IsZero() && ended.After(session.StartedAt) {
		session.DurationMilliseconds = ended.Sub(session.StartedAt).Milliseconds()
	}
	session.LogLimitBytes = maxAgentLogBytes
	session.CostStatus = "not_reported_by_cli"
	return session
}
func (a *App) AgentSessionLog(ctx context.Context, id string) (string, error) {
	session, err := a.Store.GetAgentSession(ctx, id)
	if err != nil {
		return "", err
	}
	b, err := readLogTail(session.LogPath, 65536)
	if err != nil {
		return "", err
	}
	redacted, _ := devai.Redact(string(b))
	return redacted, nil
}

type ChangeImpact struct {
	Files                 []string `json:"files"`
	AddedFiles            []string `json:"addedFiles"`
	ModifiedFiles         []string `json:"modifiedFiles"`
	DeletedFiles          []string `json:"deletedFiles"`
	RenamedFiles          []string `json:"renamedFiles"`
	UserImpact            bool     `json:"userImpact"`
	APIImpact             bool     `json:"apiImpact"`
	DatabaseImpact        bool     `json:"databaseImpact"`
	ConfigurationImpact   bool     `json:"configurationImpact"`
	SecurityImpact        bool     `json:"securityImpact"`
	Risk                  string   `json:"risk"`
	Summary               []string `json:"summary"`
	RiskReasons           []string `json:"riskReasons"`
	SuggestedVerification []string `json:"suggestedVerification"`
	RawDiffAvailable      bool     `json:"rawDiffAvailable"`
}

func (a *App) AnalyzeChanges(ctx context.Context, repoPath string) (ChangeImpact, error) {
	return a.AnalyzeChangesWithOptions(ctx, repoPath, devgit.DiffOptions{From: "HEAD"})
}

func (a *App) AnalyzeChangesWithOptions(ctx context.Context, repoPath string, options devgit.DiffOptions) (ChangeImpact, error) {
	diff, err := a.GitStructuredDiff(ctx, repoPath, options)
	if err != nil {
		return ChangeImpact{}, err
	}
	out := ChangeImpact{Files: []string{}, AddedFiles: []string{}, ModifiedFiles: []string{}, DeletedFiles: []string{}, RenamedFiles: []string{}, Risk: "low", Summary: []string{}, RiskReasons: []string{}, SuggestedVerification: []string{}}
	for _, file := range diff.Files {
		f := filepath.ToSlash(file.Path)
		out.Files = append(out.Files, f)
		classifyImpactPath(&out, f)
		switch {
		case file.Untracked || strings.HasPrefix(file.Status, "A") || strings.HasPrefix(file.Status, "?"):
			out.AddedFiles = append(out.AddedFiles, f)
		case strings.HasPrefix(file.Status, "D"):
			out.DeletedFiles = append(out.DeletedFiles, f)
		case strings.HasPrefix(file.Status, "R"):
			out.RenamedFiles = append(out.RenamedFiles, f)
		default:
			out.ModifiedFiles = append(out.ModifiedFiles, f)
		}
	}
	out.RawDiffAvailable = len(out.Files) > 0
	if out.DatabaseImpact || out.SecurityImpact {
		out.Risk = "high"
	} else if out.APIImpact || out.ConfigurationImpact || len(out.Files) > 10 {
		out.Risk = "medium"
	}
	if len(out.Files) == 0 {
		out.Summary = append(out.Summary, "工作区没有未提交变化")
		return out, nil
	}
	changeParts := []string{}
	for _, change := range []struct {
		count int
		label string
	}{{len(out.AddedFiles), "新增"}, {len(out.ModifiedFiles), "修改"}, {len(out.DeletedFiles), "删除"}, {len(out.RenamedFiles), "重命名"}} {
		if change.count > 0 {
			changeParts = append(changeParts, fmt.Sprintf("%s %d 个文件", change.label, change.count))
		}
	}
	out.Summary = append(out.Summary, strings.Join(changeParts, "，"))
	if out.UserImpact {
		out.Summary = append(out.Summary, "包含用户可见界面变化")
		out.SuggestedVerification = append(out.SuggestedVerification, "验证受影响的主要用户流程、空状态和错误状态", "检查桌面端与移动端布局及键盘可用性")
	}
	if out.APIImpact {
		out.Summary = append(out.Summary, "可能影响接口契约")
		out.RiskReasons = append(out.RiskReasons, "接口路径或契约文件发生变化，调用方兼容性需要确认")
		out.SuggestedVerification = append(out.SuggestedVerification, "运行接口契约与向后兼容测试")
	}
	if out.DatabaseImpact {
		out.Summary = append(out.Summary, "包含数据库迁移或结构变化")
		out.RiskReasons = append(out.RiskReasons, "数据库结构或迁移发生变化，失败可能影响升级与回滚")
		out.SuggestedVerification = append(out.SuggestedVerification, "在旧版数据副本上验证升级、回滚和数据保留")
	}
	if out.ConfigurationImpact {
		out.Summary = append(out.Summary, "包含部署或配置变化")
		out.RiskReasons = append(out.RiskReasons, "默认配置或部署文件发生变化，不同环境的行为可能不同")
		out.SuggestedVerification = append(out.SuggestedVerification, "验证全新安装、默认配置和旧配置升级")
	}
	if out.SecurityImpact {
		out.Summary = append(out.Summary, "涉及认证、权限或安全边界")
		out.RiskReasons = append(out.RiskReasons, "安全相关代码发生变化，需要确认拒绝路径和最小权限")
		out.SuggestedVerification = append(out.SuggestedVerification, "验证未授权、低权限、凭据脱敏和审计路径")
	}
	out.SuggestedVerification = append(out.SuggestedVerification, "运行与变更文件相关的自动化测试", "在提交前查看原始 Git Diff")
	out.RiskReasons = uniqueStrings(out.RiskReasons)
	out.SuggestedVerification = uniqueStrings(out.SuggestedVerification)
	return out, nil
}

func classifyImpactPath(out *ChangeImpact, file string) {
	normalized := strings.ToLower(filepath.ToSlash(file))
	segments := strings.Split(strings.Trim(normalized, "/"), "/")
	base := filepath.Base(normalized)
	if strings.HasSuffix(base, ".sql") || hasPathSegment(segments, "migration", "migrations", "schema", "schemas") {
		out.DatabaseImpact = true
	}
	if hasPathSegment(segments, "api", "apis", "route", "routes", "handler", "handlers", "controller", "controllers") || strings.HasPrefix(base, "openapi.") || strings.HasPrefix(base, "swagger.") || strings.HasSuffix(base, ".proto") {
		out.APIImpact = true
	}
	if hasPathSegment(segments, "config", "configs", "deploy", "deployment") || base == ".env.example" || strings.HasPrefix(base, "docker-compose.") || base == "compose.yml" || base == "compose.yaml" || strings.HasPrefix(base, "config.") || strings.Contains(base, ".config.") {
		out.ConfigurationImpact = true
	}
	if hasPathSegment(segments, "ui", "web", "frontend", "client", "static", "templates") || strings.HasSuffix(base, ".html") || strings.HasSuffix(base, ".css") || strings.HasSuffix(base, ".scss") || strings.HasSuffix(base, ".vue") || strings.HasSuffix(base, ".svelte") {
		out.UserImpact = true
	}
	if hasPathSegment(segments, "auth", "authentication", "authorization", "security", "permission", "permissions", "rbac", "iam", "crypto") || strings.Contains(base, "secret") || strings.Contains(base, "credential") {
		out.SecurityImpact = true
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func hasPathSegment(segments []string, candidates ...string) bool {
	for _, segment := range segments {
		for _, candidate := range candidates {
			if segment == candidate {
				return true
			}
		}
	}
	return false
}

type DevelopmentReport struct {
	Spec             string                   `json:"spec"`
	GeneratedAt      time.Time                `json:"generatedAt"`
	Candidate        ReleaseCandidate         `json:"candidate"`
	Evidence         []models.Evidence        `json:"evidence"`
	VerificationRuns []models.VerificationRun `json:"verificationRuns"`
	AgentSessions    []models.AgentSession    `json:"agentSessions"`
	Impacts          []TaskImpact             `json:"impacts"`
	Warnings         []string                 `json:"warnings"`
	EvidenceComplete bool                     `json:"evidenceComplete"`
}

type TaskImpact struct {
	TaskID       string       `json:"taskId"`
	TaskTitle    string       `json:"taskTitle"`
	WorktreePath string       `json:"worktreePath"`
	Impact       ChangeImpact `json:"impact"`
	Error        string       `json:"error,omitempty"`
}

func (a *App) DevelopmentReport(ctx context.Context, requirementID string) (DevelopmentReport, error) {
	c, err := a.ExportReleaseCandidate(ctx, requirementID)
	if err != nil {
		return DevelopmentReport{}, err
	}
	e, err := a.ListEvidence(ctx, requirementID)
	if err != nil {
		return DevelopmentReport{}, err
	}
	runs, err := a.ListVerificationRuns(ctx, requirementID)
	if err != nil {
		return DevelopmentReport{}, err
	}
	sessions := []models.AgentSession{}
	impacts := []TaskImpact{}
	warnings := []string{}
	for _, task := range c.Tasks {
		taskSessions, sessionErr := a.ListAgentSessions(ctx, task.ID)
		if sessionErr != nil {
			return DevelopmentReport{}, sessionErr
		}
		sessions = append(sessions, taskSessions...)
		if task.WorktreePath != "" {
			impact, impactErr := a.AnalyzeChanges(ctx, task.WorktreePath)
			item := TaskImpact{TaskID: task.ID, TaskTitle: task.Title, WorktreePath: task.WorktreePath, Impact: impact}
			if impactErr != nil {
				item.Error = impactErr.Error()
				warnings = append(warnings, fmt.Sprintf("task %q impact analysis failed: %v", task.Title, impactErr))
			}
			impacts = append(impacts, item)
		}
	}
	complete := c.Readiness.CriteriaTotal > 0 && c.Readiness.CriteriaWithEvidence == c.Readiness.CriteriaTotal
	return DevelopmentReport{Spec: "lifecycle-spec/development-report/v1", GeneratedAt: time.Now().UTC().Truncate(time.Second), Candidate: c, Evidence: e, VerificationRuns: runs, AgentSessions: sessions, Impacts: impacts, Warnings: warnings, EvidenceComplete: complete}, nil
}
