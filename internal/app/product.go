package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	devai "github.com/GeneJie199/ai-dev-cycle-manager/internal/ai"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/team"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type RegisteredModel struct {
	Model          team.Model `json:"model"`
	Roles          []string   `json:"roles"`
	VerifiedAt     string     `json:"verifiedAt,omitempty"`
	VerifiedDigest string     `json:"verifiedDigest,omitempty"`
	LastError      string     `json:"lastError,omitempty"`
}
type ProductProvider struct {
	Config *models.AIProviderConfig `json:"config,omitempty"`
	RegisteredModel
	State  string                 `json:"state"`
	Reason string                 `json:"reason"`
	Budget *models.ProviderBudget `json:"budget,omitempty"`
}
type ProviderRegistration struct {
	Provider     AIProviderInput        `json:"provider"`
	Transport    string                 `json:"transport"`
	Model        string                 `json:"model"`
	ContextLimit int                    `json:"contextLimit"`
	Tier         int                    `json:"tier"`
	Roles        []string               `json:"roles"`
	Capabilities []string               `json:"capabilities"`
	Budget       *models.ProviderBudget `json:"budget,omitempty"`
}

func (a *App) registeredModels(ctx context.Context) ([]RegisteredModel, error) {
	s, err := a.TeamStore()
	if err != nil {
		return nil, err
	}
	var entries []RegisteredModel
	err = s.ProductSetting(ctx, "provider-registry", &entries)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return entries, err
}
func (a *App) saveRegisteredModels(ctx context.Context, entries []RegisteredModel) error {
	s, err := a.TeamStore()
	if err != nil {
		return err
	}
	return s.SaveProductSetting(ctx, "provider-registry", entries)
}
func providerDigest(e RegisteredModel, config any) string {
	raw, _ := json.Marshal([]any{e.Model, e.Roles, config})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func (a *App) RegisterProductProvider(ctx context.Context, in ProviderRegistration) ([]ProductProvider, error) {
	if devai.ContainsSecretLiteral(in.Model + "\n" + in.Provider.Name + "\n" + in.Provider.BaseURL) {
		return nil, validationf("configuration contains a secret literal; use the credential field")
	}
	if in.Transport != "cli" && in.Transport != "api" {
		return nil, validationf("transport must be cli or api")
	}
	if in.Model == "" && in.Transport == "cli" {
		return nil, validationf("enter the exact model supported by your official CLI")
	}
	if in.ContextLimit < 1024 || in.ContextLimit > 2000000 || in.Tier < 0 || in.Tier > 3 {
		return nil, validationf("verified context limit and tier 0–3 required")
	}
	if len(in.Roles) == 0 || len(in.Capabilities) == 0 {
		return nil, validationf("select explicit roles and capabilities")
	}
	for _, r := range in.Roles {
		if !team.Contains(team.Roles, r) {
			return nil, validationf("unknown role")
		}
	}
	for _, c := range in.Capabilities {
		if !team.Contains(team.Capabilities, c) {
			return nil, validationf("unknown capability")
		}
	}
	id := strings.ToLower(strings.TrimSpace(in.Provider.ID))
	if in.Budget != nil {
		in.Budget.Provider = id
		if err := team.ValidateProviderBudget(*in.Budget); err != nil {
			return nil, validationf("%s", err)
		}
	}
	if in.Transport == "api" && len(id) > 60 {
		return nil, validationf("guided API provider ID must be at most 60 characters")
	}
	adapter := "api-" + id
	if in.Transport == "cli" {
		if id != "codex" {
			return nil, validationf("this guided CLI setup supports official Codex only; Kimi requires a supported authorized API or verified Expert adapter, subscription alone is not API access")
		}
		adapter = "guided-codex"
		_, err := a.ConfigureAgentAdapter(ctx, models.AgentAdapterConfig{ID: adapter, Name: "Official Codex guided", Command: "codex", Args: []string{"exec", "--ephemeral", "--model", "{{model}}", "--sandbox", "workspace-write", "--color", "never", "{{prompt_stdin}}"}, Capabilities: []string{"code_editing", "non_interactive", "worktree"}, Enabled: true})
		if err != nil {
			return nil, err
		}
	} else {
		in.Provider.Model = in.Model
		if _, err := a.ConfigureAIProvider(ctx, in.Provider); err != nil {
			return nil, err
		}
		if in.Budget != nil {
			s, err := a.TeamStore()
			if err != nil {
				return nil, err
			}
			in.Budget.Provider = id
			if err = s.SaveProviderBudget(ctx, *in.Budget); err != nil {
				return nil, validationf("%s", err)
			}
		}
	}
	entry := RegisteredModel{Model: team.Model{ID: in.Model, Provider: id, Adapter: adapter, Capabilities: in.Capabilities, ContextLimit: in.ContextLimit, Tier: in.Tier, Tools: true, Available: true}, Roles: in.Roles}
	entries, err := a.registeredModels(ctx)
	if err != nil {
		return nil, err
	}
	found := false
	for i := range entries {
		if entries[i].Model.Provider == id {
			entries[i] = entry
			found = true
		}
	}
	if !found {
		entries = append(entries, entry)
	}
	if err = a.saveRegisteredModels(ctx, entries); err != nil {
		return nil, err
	}
	return a.ProductProviders(ctx)
}
func (a *App) currentProviderConfig(ctx context.Context, e RegisteredModel) (any, error) {
	if strings.HasPrefix(e.Model.Adapter, "api-") {
		return a.Store.GetAIProvider(ctx, e.Model.Provider)
	}
	return a.GetAgentAdapter(ctx, e.Model.Adapter)
}
func (a *App) ProductProviders(ctx context.Context) ([]ProductProvider, error) {
	entries, err := a.registeredModels(ctx)
	if err != nil {
		return nil, err
	}
	out := []ProductProvider{}
	for _, entry := range entries {
		p := ProductProvider{RegisteredModel: entry, State: "configuration_required", Reason: "Run an explicit connection test for this model; API tests consume the authorized budget"}
		config, e := a.currentProviderConfig(ctx, entry)
		if e != nil {
			p.Reason = "Saved provider or adapter is missing; configure it again"
		} else if entry.LastError != "" {
			p.State = "unavailable"
			p.Reason = entry.LastError
		} else if entry.Model.ID == "" {
			p.Reason = "Discover and select a model"
		} else if strings.HasPrefix(entry.Model.Adapter, "api-") {
			b, be := a.ProviderBudget(ctx, entry.Model.Provider)
			if be == nil {
				p.Budget = &b
			}
			provider := config.(models.AIProviderConfig)
			p.Config = &provider
			if !provider.Enabled {
				p.State = "unavailable"
				p.Reason = "Provider disabled"
			} else if provider.SecretRef == "" && devai.ProviderRequiresSecret(provider) {
				p.Reason = "API credential required; subscriptions do not supply API permission"
			} else if provider.SecretRef != "" && a.AI.Secrets == nil {
				p.State = "unavailable"
				p.Reason = "Credential store unavailable"
			} else if be != nil {
				p.Reason = "Explicit CNY budget, prices and call limit required"
			} else if b.Blocked || b.Calls >= b.MaxCalls || b.ChargedMicros >= b.LimitMicros {
				p.State = "unavailable"
				p.Reason = "Budget exhausted or uncertain usage blocked further calls"
			} else if !a.providerCredentialAvailable(ctx, provider) {
				p.State = "configuration_required"
				p.Reason = "Credential unavailable; update or revoke this connection"
			} else if entry.VerifiedDigest == providerDigest(entry, config) {
				p.State = "ready"
				p.Reason = "Exact configured model passed its connection test; request failures still stop execution"
			}
		} else {
			adapter := config.(models.AgentAdapterConfig)
			if _, e := exec.LookPath(adapter.Command); e != nil {
				p.State = "unavailable"
				p.Reason = "Official CLI is not installed on this execution host"
			} else if entry.VerifiedDigest == providerDigest(entry, config) {
				p.State = "ready"
				p.Reason = "Official CLI model connection verified"
			}
		}
		out = append(out, p)
	}
	saved, err := a.Store.ListAIProviders(ctx)
	if err != nil {
		return nil, err
	}
	for _, config := range saved {
		present := false
		for _, p := range out {
			present = present || p.Model.Provider == config.ID
		}
		if !present {
			copy := config
			out = append(out, ProductProvider{RegisteredModel: RegisteredModel{Model: team.Model{ID: config.Model, Provider: config.ID, Adapter: "api-" + config.ID, ContextLimit: 16000, Tier: 1, Capabilities: []string{}}, Roles: []string{}}, Config: &copy, State: "configuration_required", Reason: "Saved API connection found: choose roles/capabilities, explicit budget, and run a model connection test"})
		}
	}
	// Honest catalog rows keep a no-model installation usable without advertising executors.
	for _, id := range []string{"codex", "kimi", "deepseek"} {
		present := false
		for _, p := range out {
			present = present || p.Model.Provider == id
		}
		if !present {
			reason := "Connect the official CLI or an authorized API; no credential or model has been verified"
			if id == "kimi" {
				reason = "BLOCKED: no authorized Kimi Coding/API credential has been configured; a chat subscription is not API permission"
			}
			out = append(out, ProductProvider{RegisteredModel: RegisteredModel{Model: team.Model{Provider: id}}, State: "configuration_required", Reason: reason})
		}
	}
	return out, nil
}
func (a *App) ProbeProductProvider(ctx context.Context, id string) ([]ProductProvider, error) {
	entries, err := a.registeredModels(ctx)
	if err != nil {
		return nil, err
	}
	index := -1
	for i := range entries {
		if entries[i].Model.Provider == id {
			index = i
		}
	}
	if index < 0 {
		return nil, validationf("configure this provider first")
	}
	e := &entries[index]
	var result struct {
		OK bool `json:"ok"`
	}
	if strings.HasPrefix(e.Model.Adapter, "api-") {
		_, err = a.TestAIProvider(ctx, id)
	} else {
		_, err = a.AI.GenerateJSON(devai.WithCLIModel(ctx, e.Model.ID), id, "model connection", `Return exactly {"ok":true}; do not edit files.`, &result)
		if err == nil && !result.OK {
			err = errors.New("model did not satisfy structured connection test")
		}
	}
	e.VerifiedDigest = ""
	e.LastError = ""
	if err != nil {
		e.LastError, _ = devai.Redact(err.Error())
	} else {
		config, ce := a.currentProviderConfig(ctx, *e)
		if ce != nil {
			return nil, ce
		}
		e.VerifiedAt = time.Now().UTC().Format(time.RFC3339)
		e.VerifiedDigest = providerDigest(*e, config)
	}
	if se := a.saveRegisteredModels(ctx, entries); se != nil {
		return nil, se
	}
	return a.ProductProviders(ctx)
}
func (a *App) RevokeProductProvider(ctx context.Context, id string) error {
	entries, err := a.registeredModels(ctx)
	if err != nil {
		return err
	}
	next := []RegisteredModel{}
	for _, e := range entries {
		if e.Model.Provider != id {
			next = append(next, e)
		}
	}
	if id != "codex" {
		if err = a.DeleteAIProvider(ctx, id); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return a.saveRegisteredModels(ctx, next)
}

type OnboardingStatus struct {
	State         string              `json:"state"`
	Reason        string              `json:"reason"`
	Platform      string              `json:"platform"`
	Git           bool                `json:"git"`
	Providers     []ProductProvider   `json:"providers"`
	Projects      []models.Repository `json:"projects"`
	TrustRequired bool                `json:"trustRequired"`
	Boundary      string              `json:"boundary"`
}

func (a *App) Onboarding(ctx context.Context) (OnboardingStatus, error) {
	p, err := a.ProductProviders(ctx)
	if err != nil {
		return OnboardingStatus{}, err
	}
	repos, err := a.ListRepositories(ctx)
	if err != nil {
		return OnboardingStatus{}, err
	}
	_, ge := exec.LookPath("git")
	s := OnboardingStatus{State: "configuration_required", Reason: "Connect and test an AI model, then select or create a project and confirm the execution scope", Platform: runtime.GOOS + "/" + runtime.GOARCH, Git: ge == nil, Providers: p, Projects: repos, TrustRequired: true, Boundary: "Trusted single-user host only. Local CLI and configured checks run with your OS privileges; worktrees are not an OS sandbox."}
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		s.State = "unsupported"
		s.Reason = "This operating system is not supported"
	} else if ge != nil {
		s.State = "unavailable"
		s.Reason = "Git is not installed on this execution host"
	} else {
		for _, p := range p {
			if p.State == "ready" {
				s.State = "ready"
				s.Reason = "Model connection verified; choose a project and safe machine checks to begin"
				break
			}
		}
	}
	return s, nil
}
func (a *App) CreateOnboardingProject(ctx context.Context, path string) (models.Repository, error) {
	if !filepath.IsAbs(path) {
		return models.Repository{}, validationf("an absolute project directory is required")
	}
	entries, err := os.ReadDir(path)
	if err == nil && len(entries) > 0 {
		return models.Repository{}, validationf("create a new empty directory, or import an existing Git project")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return models.Repository{}, err
	}
	if err = os.MkdirAll(path, 0700); err != nil {
		return models.Repository{}, err
	}
	if _, err = missionGit(ctx, path, "init"); err != nil {
		return models.Repository{}, err
	}
	if err = os.WriteFile(filepath.Join(path, "README.md"), []byte("# New DevCycle project\n"), 0600); err != nil {
		return models.Repository{}, err
	}
	for _, args := range [][]string{{"add", "README.md"}, {"-c", "user.name=DevCycle local", "-c", "user.email=devcycle@example.invalid", "commit", "-m", "Initialize local project"}} {
		if _, err = missionGit(ctx, path, args...); err != nil {
			return models.Repository{}, err
		}
	}
	return a.ImportRepository(ctx, path)
}

type SimpleMissionInput struct {
	Repository   string   `json:"repository"`
	Goal         string   `json:"goal"`
	Checks       []string `json:"checks"`
	Trust        bool     `json:"trust"`
	SourceExport bool     `json:"sourceExport"`
	Providers    []string `json:"providers"`
}

func (a *App) CreateSimpleMission(ctx context.Context, in SimpleMissionInput) (team.Mission, error) {
	if !in.Trust {
		return team.Mission{}, validationf("confirm trusted single-user execution scope")
	}
	if len(in.Providers) == 0 {
		return team.Mission{}, validationf("explicitly choose authorized providers; paid fallback is never automatic")
	}
	ps, err := a.ProductProviders(ctx)
	if err != nil {
		return team.Mission{}, err
	}
	models := []team.Model{}
	bindings := []RegisteredModel{}
	for _, id := range in.Providers {
		found := false
		for _, p := range ps {
			if p.Model.Provider == id && p.State == "ready" {
				models = append(models, p.Model)
				bindings = append(bindings, p.RegisteredModel)
				found = true
			}
		}
		if !found {
			return team.Mission{}, validationf("provider %s is not ready; open Provider Center", id)
		}
	}
	for _, role := range []string{"explorer", "lead", "qa", "review"} {
		found := false
		for _, b := range bindings {
			found = found || team.Contains(b.Roles, role)
		}
		if !found {
			return team.Mission{}, validationf("configure a model for role %s; no unauthorized provider fallback", role)
		}
	}
	m, err := a.CreateMission(ctx, team.Mission{Repository: in.Repository, Goal: in.Goal, Mode: "simple", Checks: in.Checks, Models: models, Policy: team.Policy{TrustedLocalCLI: true, SourceExport: in.SourceExport, CommitTaskChanges: true, AllowedProviders: in.Providers}, Budget: team.Budget{MaxExecutions: 12, MaxFixes: 1, TimeoutSeconds: 600}})
	if err != nil {
		return m, err
	}
	s, _ := a.TeamStore()
	if err = s.SaveProductSetting(ctx, "mission-routing:"+m.ID, bindings); err != nil {
		return m, err
	}
	return m, nil
}
func (a *App) routedMission(ctx context.Context, m team.Mission, role string) (team.Mission, error) {
	s, err := a.TeamStore()
	if err != nil {
		return m, err
	}
	var bindings []RegisteredModel
	err = s.ProductSetting(ctx, "mission-routing:"+m.ID, &bindings)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	current, err := a.ProductProviders(ctx)
	if err != nil {
		return m, err
	}
	models := []team.Model{}
	for _, b := range bindings {
		if !team.Contains(b.Roles, role) {
			continue
		}
		for _, p := range current {
			if p.State == "ready" && p.Model.Provider == b.Model.Provider && p.Model.ID == b.Model.ID && p.VerifiedDigest == b.VerifiedDigest {
				models = append(models, b.Model)
			}
		}
	}
	m.Models = models
	return m, nil
}

func (a *App) providerCredentialAvailable(ctx context.Context, c models.AIProviderConfig) bool {
	if !devai.ProviderRequiresSecret(c) {
		return true
	}
	if a.AI.Secrets == nil {
		return false
	}
	secret, err := a.AI.Secrets.Get(ctx, c.SecretRef)
	return err == nil && secret != ""
}
func (a *App) invalidateProductProvider(ctx context.Context, id string) error {
	entries, err := a.registeredModels(ctx)
	if err != nil {
		return err
	}
	changed := false
	for i := range entries {
		if entries[i].Model.Provider == id {
			entries[i].VerifiedDigest = ""
			entries[i].VerifiedAt = ""
			entries[i].LastError = ""
			changed = true
		}
	}
	if changed {
		return a.saveRegisteredModels(ctx, entries)
	}
	return nil
}

func (a *App) ResumeProviderAfterUncertainUsage(ctx context.Context, id string) error {
	s, err := a.TeamStore()
	if err != nil {
		return err
	}
	if err = s.ResumeProviderBudget(ctx, id); err != nil {
		return err
	}
	return a.invalidateProductProvider(ctx, id)
}
