// Package web provides the local web workbench: an embedded static UI and a
// JSON API over internal/app. It is designed to listen on loopback only.
package web

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/app"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/git"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/store"
)

//go:embed static
var staticFS embed.FS

const maxBodyBytes = 1 << 20 // 1 MiB

// Server exposes the workbench UI and API over HTTP.
type Server struct {
	app    *app.App
	mux    *http.ServeMux
	logger *log.Logger
}

// NewServer builds a Server on top of an open app.App.
func NewServer(a *app.App, logger *log.Logger) (*Server, error) {
	if a == nil {
		return nil, errors.New("nil app")
	}
	if logger == nil {
		logger = log.Default()
	}
	s := &Server{app: a, mux: http.NewServeMux(), logger: logger}

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}
	s.mux.Handle("GET /", http.FileServerFS(sub))

	s.mux.HandleFunc("GET /api/repositories", s.handleListRepositories)
	s.mux.HandleFunc("POST /api/repositories", s.handleImportRepository)
	s.mux.HandleFunc("DELETE /api/repositories/{id}", s.handleDeleteRepository)

	s.mux.HandleFunc("GET /api/requirements", s.handleListRequirements)
	s.mux.HandleFunc("POST /api/requirements", s.handleCreateRequirement)
	s.mux.HandleFunc("GET /api/requirements/{id}", s.handleGetRequirementDetail)
	s.mux.HandleFunc("PATCH /api/requirements/{id}", s.handleUpdateRequirement)
	s.mux.HandleFunc("DELETE /api/requirements/{id}", s.handleDeleteRequirement)
	s.mux.HandleFunc("GET /api/requirements/{id}/criteria", s.handleListCriteria)
	s.mux.HandleFunc("POST /api/requirements/{id}/criteria", s.handleCreateCriterion)
	s.mux.HandleFunc("GET /api/requirements/{id}/release-candidate", s.handleReleaseCandidate)
	s.mux.HandleFunc("GET /api/requirements/{id}/development-report", s.handleDevelopmentReport)
	s.mux.HandleFunc("GET /api/requirements/{id}/evidence", s.handleListEvidence)
	s.mux.HandleFunc("POST /api/requirements/{id}/evidence", s.handleCreateEvidence)
	s.mux.HandleFunc("GET /api/requirements/{id}/runs", s.handleListRuns)
	s.mux.HandleFunc("POST /api/requirements/{id}/runs", s.handleRunVerification)
	s.mux.HandleFunc("POST /api/verification-runs/{id}/stop", s.handleStopVerification)
	s.mux.HandleFunc("GET /api/ai/providers", s.handleAIProviders)
	s.mux.HandleFunc("POST /api/ai/providers", s.handleConfigureAIProvider)
	s.mux.HandleFunc("DELETE /api/ai/providers/{id}", s.handleDeleteAIProvider)
	s.mux.HandleFunc("POST /api/ai/providers/{id}/test", s.handleTestAIProvider)
	s.mux.HandleFunc("POST /api/ai/request-preview", s.handleAIRequestPreview)
	s.mux.HandleFunc("GET /api/requirements/{id}/plan", s.handleGetPlanningDocument)
	s.mux.HandleFunc("PUT /api/requirements/{id}/plan", s.handleSavePlanningDocument)
	s.mux.HandleFunc("POST /api/requirements/{id}/ai-plan", s.handleGenerateAIPlan)
	s.mux.HandleFunc("POST /api/requirements/{id}/ai-plan/apply", s.handleApplyAIPlan)

	s.mux.HandleFunc("PATCH /api/criteria/{id}", s.handleUpdateCriterion)
	s.mux.HandleFunc("DELETE /api/criteria/{id}", s.handleDeleteCriterion)

	s.mux.HandleFunc("GET /api/tasks", s.handleListTasks)
	s.mux.HandleFunc("POST /api/tasks", s.handleCreateTask)
	s.mux.HandleFunc("PATCH /api/tasks/{id}", s.handleUpdateTask)
	s.mux.HandleFunc("DELETE /api/tasks/{id}", s.handleDeleteTask)
	s.mux.HandleFunc("POST /api/tasks/{id}/worktree", s.handleCreateTaskWorktree)
	s.mux.HandleFunc("GET /api/tasks/{id}/handoffs", s.handleListTaskHandoffs)
	s.mux.HandleFunc("POST /api/tasks/{id}/handoffs", s.handleCreateTaskHandoff)
	s.mux.HandleFunc("POST /api/handoffs/{id}/accept", s.handleAcceptTaskHandoff)
	s.mux.HandleFunc("GET /api/agent-adapters", s.handleListAgentAdapters)
	s.mux.HandleFunc("POST /api/agent-adapters", s.handleConfigureAgentAdapter)
	s.mux.HandleFunc("DELETE /api/agent-adapters/{id}", s.handleDeleteAgentAdapter)
	s.mux.HandleFunc("GET /api/agent-sessions", s.handleListSessions)
	s.mux.HandleFunc("POST /api/agent-sessions", s.handleStartSession)
	s.mux.HandleFunc("POST /api/agent-sessions/{id}/stop", s.handleStopSession)
	s.mux.HandleFunc("GET /api/agent-sessions/{id}/log", s.handleSessionLog)

	s.mux.HandleFunc("GET /api/git/status", s.handleGitStatus)
	s.mux.HandleFunc("GET /api/git/diff", s.handleGitDiff)
	s.mux.HandleFunc("GET /api/git/structured-diff", s.handleGitStructuredDiff)
	s.mux.HandleFunc("POST /api/git/explain", s.handleGitExplain)
	s.mux.HandleFunc("GET /api/git/branches", s.handleGitBranches)
	s.mux.HandleFunc("GET /api/git/log", s.handleGitLog)
	s.mux.HandleFunc("GET /api/git/impact", s.handleGitImpact)

	return s, nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'")
	w.Header().Set("Cache-Control", "no-store")
	s.mux.ServeHTTP(w, r)
}

// ListenAndServe runs the server on a loopback address.
func (s *Server) ListenAndServe(addr string) error {
	if err := validateListenAddress(addr); err != nil {
		return err
	}
	srv := &http.Server{Addr: addr, Handler: s, ReadHeaderTimeout: 10 * time.Second}
	s.logger.Printf("devcycle workbench listening on http://%s", addr)
	return srv.ListenAndServe()
}

// Serve runs the server until ctx is canceled and then drains active requests.
func (s *Server) Serve(ctx context.Context, addr string) error {
	if err := validateListenAddress(addr); err != nil {
		return err
	}
	srv := &http.Server{Addr: addr, Handler: s, ReadHeaderTimeout: 10 * time.Second}
	s.logger.Printf("devcycle workbench listening on http://%s", addr)
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func validateListenAddress(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return errors.New("refusing to listen on a non-loopback address; use an SSH tunnel")
	}
	return nil
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body: multiple JSON values are not allowed")
		return false
	}
	return true
}

func isNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// nonNil normalizes nil slices so list endpoints return [] instead of null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	if app.IsValidationError(err) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

// --- repositories ---

func (s *Server) handleListRepositories(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.ListRepositories(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(list))
}

func (s *Server) handleImportRepository(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	repo, err := s.app.ImportRepository(r.Context(), req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, repo)
}

func (s *Server) handleDeleteRepository(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteRepository(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- requirements ---

func (s *Server) handleListRequirements(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.ListRequirements(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(list))
}

func (s *Server) handleCreateRequirement(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	out, err := s.app.CreateRequirement(r.Context(), req.Title, req.Description)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleUpdateRequirement(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	out, err := s.app.UpdateRequirement(r.Context(), r.PathValue("id"), strings.TrimSpace(req.Title), req.Description)
	if err != nil {
		if strings.Contains(err.Error(), "required") {
			writeError(w, 400, err.Error())
		} else {
			s.fail(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteRequirement(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteRequirement(r.Context(), r.PathValue("id")); err != nil {
		if strings.Contains(err.Error(), "running agent") {
			writeError(w, 409, err.Error())
		} else {
			s.fail(w, err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type requirementDetail struct {
	Requirement models.Requirement           `json:"requirement"`
	Criteria    []models.AcceptanceCriterion `json:"criteria"`
	Tasks       []models.Task                `json:"tasks"`
	Plan        *models.PlanningDocument     `json:"plan,omitempty"`
}

func (s *Server) handleGetRequirementDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, err := s.app.Store.GetRequirement(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	criteria, err := s.app.ListCriteriaByRequirement(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	tasks, err := s.app.ListTasksByRequirement(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	var plan *models.PlanningDocument
	if stored, planErr := s.app.GetPlanningDocument(r.Context(), id); planErr == nil {
		plan = &stored
	} else if !isNotFound(planErr) {
		s.fail(w, planErr)
		return
	}
	writeJSON(w, http.StatusOK, requirementDetail{Requirement: req, Criteria: criteria, Tasks: tasks, Plan: plan})
}

func (s *Server) handleListCriteria(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.ListCriteriaByRequirement(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(list))
}

func (s *Server) handleCreateCriterion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description string `json:"description"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Description) == "" {
		writeError(w, http.StatusBadRequest, "description is required")
		return
	}
	c, err := s.app.CreateCriterion(r.Context(), r.PathValue("id"), req.Description)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateCriterion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Satisfied     *bool   `json:"satisfied"`
		Description   *string `json:"description,omitempty"`
		EvidenceTitle string  `json:"evidenceTitle,omitempty"`
		EvidenceNote  string  `json:"evidenceNote,omitempty"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Satisfied == nil && req.Description == nil {
		writeError(w, http.StatusBadRequest, "satisfied or description is required")
		return
	}
	criterion, err := s.app.Store.GetCriterion(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if req.Description != nil {
		description := strings.TrimSpace(*req.Description)
		if len([]rune(description)) < 5 || len([]rune(description)) > 2000 {
			writeError(w, http.StatusBadRequest, "criterion must contain 5 to 2000 characters")
			return
		}
		criterion, err = s.app.Store.UpdateCriterion(r.Context(), criterion.ID, description, criterion.Satisfied)
		if err != nil {
			s.fail(w, err)
			return
		}
	}
	if req.Satisfied == nil {
		writeJSON(w, http.StatusOK, criterion)
		return
	}
	if *req.Satisfied && strings.TrimSpace(req.EvidenceTitle) != "" {
		_, err = s.app.AddEvidence(r.Context(), models.Evidence{RequirementID: criterion.RequirementID, CriterionID: criterion.ID, Kind: "manual", Title: req.EvidenceTitle, Status: "passed", Inline: req.EvidenceNote})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	c, err := s.app.UpdateCriterionResult(r.Context(), r.PathValue("id"), *req.Satisfied)
	if err != nil {
		if strings.Contains(err.Error(), "requires passing evidence") {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			s.fail(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteCriterion(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteCriterion(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- AI-assisted planning ---

func (s *Server) handleAIProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.app.AIProviders(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(providers))
}

func (s *Server) handleConfigureAIProvider(w http.ResponseWriter, r *http.Request) {
	var input app.AIProviderInput
	if !decodeBody(w, r, &input) {
		return
	}
	provider, err := s.app.ConfigureAIProvider(r.Context(), input)
	if err != nil {
		if app.IsValidationError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusServiceUnavailable, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, provider)
}

func (s *Server) handleDeleteAIProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteAIProvider(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestAIProvider(w http.ResponseWriter, r *http.Request) {
	meta, err := s.app.TestAIProvider(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "meta": meta})
}

func (s *Server) handleAIRequestPreview(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Provider string `json:"provider"`
		Input    string `json:"input"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	preview, err := s.app.AIRequestPreview(r.Context(), input.Provider, input.Input)
	if err != nil {
		if app.IsValidationError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusBadGateway, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleGetPlanningDocument(w http.ResponseWriter, r *http.Request) {
	document, err := s.app.GetPlanningDocument(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, document)
}

func (s *Server) handleSavePlanningDocument(w http.ResponseWriter, r *http.Request) {
	var document models.PlanningDocument
	if !decodeBody(w, r, &document) {
		return
	}
	saved, err := s.app.SavePlanningDocument(r.Context(), r.PathValue("id"), document)
	if err != nil {
		if errors.Is(err, store.ErrPlanningRevisionConflict) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			s.fail(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleGenerateAIPlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider          string `json:"provider"`
		AdditionalContext string `json:"additionalContext"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Provider) == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}
	preview, err := s.app.GenerateAIPlan(r.Context(), r.PathValue("id"), req.Provider, req.AdditionalContext)
	if err != nil {
		if isNotFound(err) || strings.Contains(err.Error(), "requirement: sql") {
			writeError(w, http.StatusNotFound, "requirement not found")
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleApplyAIPlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Document *models.PlanningDocument `json:"document,omitempty"`
		Criteria []app.AIPlanCriterion    `json:"criteria,omitempty"`
		Tasks    []app.AIPlanTask         `json:"tasks,omitempty"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	var applied app.AppliedAIPlan
	var err error
	if req.Document != nil {
		applied, err = s.app.ApplyPlanningDocument(r.Context(), r.PathValue("id"), *req.Document)
	} else {
		applied, err = s.app.ApplyAIPlan(r.Context(), r.PathValue("id"), req.Criteria, req.Tasks)
	}
	if err != nil {
		if isNotFound(err) || strings.Contains(err.Error(), "requirement: sql") {
			writeError(w, http.StatusNotFound, "requirement not found")
			return
		}
		if errors.Is(err, store.ErrPlanningRevisionConflict) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, applied)
}

// --- tasks ---

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if reqID := r.URL.Query().Get("requirementId"); reqID != "" {
		list, err := s.app.ListTasksByRequirement(ctx, reqID)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, nonNil(list))
		return
	}
	list, err := s.app.ListTasks(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(list))
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequirementID string `json:"requirementId"`
		Title         string `json:"title"`
		Description   string `json:"description"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.RequirementID) == "" || strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "requirementId and title are required")
		return
	}
	t, err := s.app.CreateTask(r.Context(), req.RequirementID, req.Title, req.Description)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       *string            `json:"title,omitempty"`
		Description *string            `json:"description,omitempty"`
		Status      *models.TaskStatus `json:"status,omitempty"`
		DependsOn   *[]string          `json:"dependsOn,omitempty"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	current, err := s.app.GetTask(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	title, description, status, dependencies := current.Title, current.Description, current.Status, current.DependsOn
	if req.Title != nil {
		title = *req.Title
	}
	if req.Description != nil {
		description = *req.Description
	}
	if req.Status != nil {
		status = *req.Status
	}
	if req.DependsOn != nil {
		dependencies = *req.DependsOn
	}
	t, err := s.app.UpdateTask(r.Context(), current.ID, title, description, status, dependencies)
	if err != nil {
		if app.IsValidationError(err) || strings.HasPrefix(err.Error(), "invalid task status") || strings.Contains(err.Error(), "running agent") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteTask(r.Context(), r.PathValue("id")); err != nil {
		if strings.Contains(err.Error(), "running agent") {
			writeError(w, 409, err.Error())
		} else {
			s.fail(w, err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateTaskWorktree(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepositoryPath string `json:"repositoryPath"`
		Branch         string `json:"branch"`
		WorktreePath   string `json:"worktreePath"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.RepositoryPath) == "" || strings.TrimSpace(req.Branch) == "" || strings.TrimSpace(req.WorktreePath) == "" {
		writeError(w, http.StatusBadRequest, "repositoryPath, branch, and worktreePath are required")
		return
	}
	task, worktree, err := s.app.LinkTaskToWorktree(r.Context(), req.RepositoryPath, r.PathValue("id"), req.Branch, req.WorktreePath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"task": task, "worktree": worktree})
}

func (s *Server) handleListAgentAdapters(w http.ResponseWriter, r *http.Request) {
	adapters, err := s.app.AgentAdapters(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(adapters))
}

func (s *Server) handleConfigureAgentAdapter(w http.ResponseWriter, r *http.Request) {
	var adapter models.AgentAdapterConfig
	if !decodeBody(w, r, &adapter) {
		return
	}
	status, err := s.app.ConfigureAgentAdapter(r.Context(), adapter)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleDeleteAgentAdapter(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteAgentAdapter(r.Context(), r.PathValue("id")); err != nil {
		if strings.Contains(err.Error(), "running agent") {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			s.fail(w, err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListTaskHandoffs(w http.ResponseWriter, r *http.Request) {
	handoffs, err := s.app.ListTaskHandoffs(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(handoffs))
}

func (s *Server) handleCreateTaskHandoff(w http.ResponseWriter, r *http.Request) {
	var input app.TaskHandoffInput
	if !decodeBody(w, r, &input) {
		return
	}
	handoff, err := s.app.CreateTaskHandoff(r.Context(), r.PathValue("id"), input)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, handoff)
}

func (s *Server) handleAcceptTaskHandoff(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SessionID string `json:"sessionId,omitempty"`
	}
	if !decodeBody(w, r, &input) {
		return
	}
	handoff, err := s.app.AcceptTaskHandoff(r.Context(), r.PathValue("id"), input.SessionID)
	if err != nil {
		if strings.Contains(err.Error(), "already accepted") {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			s.fail(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, handoff)
}

// --- git ---

func repoParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if repo == "" {
		writeError(w, http.StatusBadRequest, "repo query parameter is required")
		return "", false
	}
	return repo, true
}

func (s *Server) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	repo, ok := repoParam(w, r)
	if !ok {
		return
	}
	st, err := s.app.GitStatus(r.Context(), repo)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	repo, ok := repoParam(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	res, err := s.app.GitDiff(r.Context(), repo, git.DiffOptions{
		Stat:   q.Get("stat") == "1" || q.Get("stat") == "true",
		Staged: q.Get("staged") == "1" || q.Get("staged") == "true",
		From:   strings.TrimSpace(q.Get("from")),
		To:     strings.TrimSpace(q.Get("to")),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGitStructuredDiff(w http.ResponseWriter, r *http.Request) {
	repo, ok := repoParam(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	result, err := s.app.GitStructuredDiff(r.Context(), repo, git.DiffOptions{
		Staged: q.Get("staged") == "1" || q.Get("staged") == "true",
		From:   strings.TrimSpace(q.Get("from")),
		To:     strings.TrimSpace(q.Get("to")),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGitExplain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepositoryPath string `json:"repositoryPath"`
		Provider       string `json:"provider"`
		From           string `json:"from"`
		To             string `json:"to"`
		Staged         bool   `json:"staged"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.RepositoryPath) == "" || strings.TrimSpace(req.Provider) == "" {
		writeError(w, http.StatusBadRequest, "repositoryPath and provider are required")
		return
	}
	explanation, err := s.app.ExplainGitDiff(r.Context(), req.RepositoryPath, git.DiffOptions{From: strings.TrimSpace(req.From), To: strings.TrimSpace(req.To), Staged: req.Staged}, req.Provider)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, explanation)
}

func (s *Server) handleGitBranches(w http.ResponseWriter, r *http.Request) {
	repo, ok := repoParam(w, r)
	if !ok {
		return
	}
	remote := r.URL.Query().Get("remote") == "1" || r.URL.Query().Get("remote") == "true"
	list, err := s.app.GitBranches(r.Context(), repo, remote)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nonNil(list))
}

func (s *Server) handleGitLog(w http.ResponseWriter, r *http.Request) {
	repo, ok := repoParam(w, r)
	if !ok {
		return
	}
	list, err := s.app.GitLog(r.Context(), repo, 20)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nonNil(list))
}

// --- release candidate ---

func (s *Server) handleReleaseCandidate(w http.ResponseWriter, r *http.Request) {
	rc, err := s.app.ExportReleaseCandidate(r.Context(), r.PathValue("id"))
	if err != nil {
		if isNotFound(err) || strings.Contains(err.Error(), "no rows") {
			writeError(w, http.StatusNotFound, "requirement not found")
			return
		}
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rc)
}

func (s *Server) handleDevelopmentReport(w http.ResponseWriter, r *http.Request) {
	report, err := s.app.DevelopmentReport(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleListEvidence(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListEvidence(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(items))
}

func (s *Server) handleCreateEvidence(w http.ResponseWriter, r *http.Request) {
	var e models.Evidence
	if !decodeBody(w, r, &e) {
		return
	}
	e.ID = ""
	e.RequirementID = r.PathValue("id")
	created, err := s.app.AddEvidence(r.Context(), e)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListVerificationRuns(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(items))
}

func (s *Server) handleRunVerification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string `json:"name"`
		Command        string `json:"command"`
		WorkingDir     string `json:"workingDir"`
		CriterionID    string `json:"criterionId"`
		TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	run, err := s.app.StartVerification(r.Context(), r.PathValue("id"), req.CriterionID, req.Name, req.Command, req.WorkingDir, time.Duration(req.TimeoutSeconds)*time.Second)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleStopVerification(w http.ResponseWriter, r *http.Request) {
	run, err := s.app.StopVerification(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	items, err := s.app.ListAgentSessions(r.Context(), r.URL.Query().Get("taskId"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, nonNil(items))
}
func (s *Server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID   string `json:"taskId"`
		Provider string `json:"provider"`
		Prompt   string `json:"prompt"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	session, err := s.app.StartAgentSession(r.Context(), req.TaskID, req.Provider, req.Prompt)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, session)
}
func (s *Server) handleStopSession(w http.ResponseWriter, r *http.Request) {
	session, err := s.app.StopAgentSession(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, session)
}
func (s *Server) handleSessionLog(w http.ResponseWriter, r *http.Request) {
	value, err := s.app.AgentSessionLog(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"log": value})
}
func (s *Server) handleGitImpact(w http.ResponseWriter, r *http.Request) {
	repo, ok := repoParam(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	impact, err := s.app.AnalyzeChangesWithOptions(r.Context(), repo, git.DiffOptions{From: strings.TrimSpace(q.Get("from")), To: strings.TrimSpace(q.Get("to")), Staged: q.Get("staged") == "1" || q.Get("staged") == "true"})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, impact)
}
