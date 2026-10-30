// Package team implements vendor-neutral local engineering policies and contracts.
package team

import (
	"errors"
	"fmt"
	extensions "github.com/GeneJie199/ai-dev-cycle-manager/api/extensions/v2"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const Spec = "devcycle.ai-team/v2alpha1"

var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

var Roles = []string{"lead", "frontend", "explorer", "qa", "review"}
var Capabilities = []string{"coding", "reasoning", "planning", "frontend", "vision", "long_context", "review", "testing", "exploration", "low_cost"}

type Model struct {
	ID             string   `json:"id"`
	Provider       string   `json:"provider"`
	Adapter        string   `json:"adapter"`
	Capabilities   []string `json:"capabilities"`
	ContextLimit   int      `json:"contextLimit"`
	Tier           int      `json:"tier"`
	CostPerMillion *float64 `json:"costPerMillion"`
	Available      bool     `json:"available"`
	Tools          bool     `json:"tools"`
	// EnforcedPolicies is an executor assertion, never inferred from a model name.
	EnforcedPolicies []string `json:"enforcedPolicies"`
	Evaluation       float64  `json:"evaluation"`
}
type Budget struct {
	MaxFixes       int      `json:"maxFixes"`
	MaxExecutions  int      `json:"maxExecutions"`
	MaxTokens      int      `json:"maxTokens"`
	MaxCost        *float64 `json:"maxCost"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
}
type Policy struct {
	RequireDifferentReviewModel bool     `json:"requireDifferentReviewModel"`
	CommitTaskChanges           bool     `json:"commitTaskChanges"`
	AllowedProviders            []string `json:"allowedProviders"`
	SourceExport                bool     `json:"sourceExport"`
	RequiredEnforcement         []string `json:"requiredEnforcement"`
	// TrustedLocalCLI explicitly permits CLI tools with the user's own OS privileges.
	TrustedLocalCLI bool `json:"trustedLocalCLI"`
}
type Task struct {
	ID           string   `json:"id"`
	Role         string   `json:"role"`
	Prompt       string   `json:"prompt"`
	DependsOn    []string `json:"dependsOn"`
	Capabilities []string `json:"capabilities"`
	ContextSize  int      `json:"contextSize"`
	State        string   `json:"state"`
	Attempts     int      `json:"attempts"`
	MaxAttempts  int      `json:"maxAttempts"`
	TaskID       string   `json:"taskId"`
	Worktree     string   `json:"worktree"`
	Branch       string   `json:"branch"`
	Head         string   `json:"head"`
	ArtifactHead string   `json:"artifactHead"`
}
type Execution struct {
	ID            string    `json:"id"`
	TaskID        string    `json:"taskId"`
	Agent         string    `json:"agent"`
	Provider      string    `json:"provider"`
	Model         string    `json:"model"`
	SessionID     string    `json:"sessionId"`
	HandoffID     string    `json:"handoffId,omitempty"`
	Result        string    `json:"result"`
	Retry         int       `json:"retry"`
	Escalation    int       `json:"escalation"`
	Tokens        *int      `json:"tokens"`
	Cost          *float64  `json:"cost"`
	Started       time.Time `json:"started"`
	DurationMS    int64     `json:"durationMs"`
	EvidenceIDs   []string  `json:"evidenceIds"`
	TestResult    string    `json:"testResult"`
	ReviewResult  string    `json:"reviewResult"`
	HumanOverride bool      `json:"humanOverride"`
	Error         string    `json:"error,omitempty"`
}
type Mission struct {
	WorktreeRoot      string      `json:"worktreeRoot"`
	BranchPrefix      string      `json:"branchPrefix"`
	FixCycles         int         `json:"fixCycles"`
	Spec              string      `json:"spec"`
	ID                string      `json:"id"`
	ProjectID         string      `json:"projectId"`
	Repository        string      `json:"repository"`
	Goal              string      `json:"goal"`
	Mode              string      `json:"mode"`
	RequirementID     string      `json:"requirementId"`
	Risk              int         `json:"risk"`
	RiskReasons       []string    `json:"riskReasons"`
	Plan              []Task      `json:"plan"`
	Acceptance        []string    `json:"acceptance"`
	Checks            []string    `json:"checks"`
	IntegrationChecks []string    `json:"integrationChecks"`
	SecurityChecks    []string    `json:"securityChecks"`
	Models            []Model     `json:"models"`
	Budget            Budget      `json:"budget"`
	Policy            Policy      `json:"policy"`
	State             string      `json:"state"`
	Executions        []Execution `json:"executions"`
	Memory            []Memory    `json:"memory"`
	Revision          int         `json:"revision"`
	Created           time.Time   `json:"created"`
}
type Memory struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	Source    string    `json:"source"`
	Author    string    `json:"author"`
	Revision  int       `json:"revision"`
	Updated   time.Time `json:"updated"`
}

var MemoryKinds = []string{"architecture", "product", "rejected_option", "constraint", "convention", "api_contract", "environment", "release", "risk", "failure", "user_decision"}

func Contains(items []string, value string) bool {
	for _, v := range items {
		if v == value {
			return true
		}
	}
	return false
}
func all(have, need []string) bool {
	for _, n := range need {
		if !Contains(have, n) {
			return false
		}
	}
	return true
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
func Validate(m Mission) error {
	if strings.TrimSpace(m.Goal) == "" || len(m.Goal) > 20000 || m.ProjectID == "" || m.Repository == "" {
		return errors.New("projectId, repository and bounded goal are required")
	}
	if m.Mode != "simple" && m.Mode != "expert" {
		return errors.New("mode must be simple or expert")
	}
	if m.Risk < 0 || m.Risk > 4 || len(m.Plan) == 0 || len(m.Plan) > 100 {
		return errors.New("invalid risk or plan size")
	}
	if m.Budget.MaxExecutions < 1 || m.Budget.MaxExecutions > 300 || m.Budget.TimeoutSeconds < 1 || m.Budget.TimeoutSeconds > 1800 || m.Budget.MaxTokens < 0 || (m.Budget.MaxCost != nil && !finite(*m.Budget.MaxCost)) {
		return errors.New("invalid budget")
	}
	if m.Budget.MaxFixes < 0 || m.Budget.MaxFixes > 3 {
		return errors.New("invalid fix cycle bound")
	}
	if len(m.Checks) == 0 || len(m.Checks) > 20 || len(m.Acceptance) == 0 {
		return errors.New("machine checks and acceptance are required")
	}
	for _, c := range m.Checks {
		if strings.TrimSpace(c) == "" || len(c) > 4000 {
			return errors.New("invalid check")
		}
	}
	ids := map[string]Task{}
	roles := map[string]bool{}
	for _, t := range m.Plan {
		if !safeID.MatchString(t.ID) || ids[t.ID].ID != "" || !Contains(Roles, t.Role) || strings.TrimSpace(t.Prompt) == "" || len(t.Prompt) > 20000 || t.ContextSize < 0 || t.MaxAttempts < 1 || t.MaxAttempts > 5 || !all(Capabilities, t.Capabilities) {
			return errors.New("invalid task, role, capability, context or retry bound")
		}
		ids[t.ID] = t
		roles[t.Role] = true
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return errors.New("DAG cycle")
		}
		if visited[id] {
			return nil
		}
		t, ok := ids[id]
		if !ok {
			return fmt.Errorf("unknown dependency %s", id)
		}
		visiting[id] = true
		for _, dep := range t.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	var ancestor func(string, string) bool
	ancestor = func(id, target string) bool {
		for _, dep := range ids[id].DependsOn {
			if dep == target || ancestor(dep, target) {
				return true
			}
		}
		return false
	}
	for _, t := range m.Plan {
		for _, other := range m.Plan {
			if t.Role == "qa" && (other.Role == "lead" || other.Role == "frontend") && !ancestor(t.ID, other.ID) {
				return errors.New("QA must depend on every implementation task")
			}
			if t.Role == "review" && other.Role == "qa" && !ancestor(t.ID, other.ID) {
				return errors.New("review must depend on every QA task")
			}
		}
	}
	if !roles["lead"] || !roles["qa"] || (m.Risk >= 1 && !roles["review"]) {
		return errors.New("plan lacks required development, testing or independent review role")
	}
	seen := map[string]bool{}
	for _, model := range m.Models {
		if model.ID == "" || seen[model.ID] || model.Provider == "" || model.Adapter == "" || model.ContextLimit < 1 || model.Tier < 0 || model.Tier > 2 || !finite(model.Evaluation) || model.Evaluation > 1 || !all(Capabilities, model.Capabilities) || (model.CostPerMillion != nil && !finite(*model.CostPerMillion)) {
			return errors.New("invalid or duplicate registry entry")
		}
		seen[model.ID] = true
	}
	if len(seen) == 0 {
		return errors.New("empty capability registry")
	}
	return nil
}

// Route returns deterministic eligible candidates ordered by tier, evaluation and cost.
func Route(m Mission, t Task, available map[string]bool) ([]Model, error) {
	if len(m.Executions) >= m.Budget.MaxExecutions {
		return nil, errors.New("execution budget exceeded")
	}
	if m.Budget.MaxCost != nil || m.Budget.MaxTokens > 0 {
		return nil, errors.New("strict cost/token budget requires a metered executor; CLI usage is unknown")
	}
	out := []Model{}
	minTier := 0
	if t.Role == "lead" || t.Role == "review" || m.Risk >= 3 {
		minTier = 2
	}
	if t.Attempts > minTier {
		minTier = t.Attempts
	}
	for _, model := range m.Models {
		if !model.Available || !available[model.Adapter] || !Contains(m.Policy.AllowedProviders, model.Provider) || !model.Tools || model.Tier < minTier || model.ContextLimit < t.ContextSize || !all(model.Capabilities, t.Capabilities) || !all(model.EnforcedPolicies, m.Policy.RequiredEnforcement) {
			continue
		}
		if !m.Policy.SourceExport && !Contains(model.EnforcedPolicies, "no_network") {
			continue
		}
		out = append(out, model)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Tier != b.Tier {
			return a.Tier < b.Tier
		}
		if a.Evaluation != b.Evaluation {
			return a.Evaluation > b.Evaluation
		}
		if a.CostPerMillion != nil && b.CostPerMillion != nil && *a.CostPerMillion != *b.CostPerMillion {
			return *a.CostPerMillion < *b.CostPerMillion
		}
		return a.ID < b.ID
	})
	if len(out) == 0 {
		return nil, errors.New("no available executor satisfies capability, context, escalation and policy")
	}
	return out, nil
}

func Ready(m *Mission) []int {
	states := map[string]string{}
	for _, t := range m.Plan {
		states[t.ID] = t.State
	}
	out := []int{}
	for i := range m.Plan {
		t := &m.Plan[i]
		if t.State != "pending" && t.State != "blocked" && t.State != "ready" && t.State != "retry" {
			continue
		}
		ready := true
		for _, dep := range t.DependsOn {
			if states[dep] != "completed" {
				ready = false
			}
		}
		if ready {
			t.State = "ready"
			out = append(out, i)
		} else {
			t.State = "blocked"
		}
	}
	return out
}

// Assess is intentionally conservative. Callers may increase but cannot lower its result.
func Assess(goal string, files []string) (int, []string) {
	s := strings.ToLower(goal + " " + strings.Join(files, " "))
	risk := 1
	reasons := []string{"ordinary engineering change"}
	if len(files) > 0 {
		docs := true
		for _, f := range files {
			if !strings.HasSuffix(strings.ToLower(f), ".md") {
				docs = false
			}
		}
		if docs {
			risk = 0
			reasons = []string{"documentation-only declared scope"}
		}
	}
	for _, term := range []string{"跨模块", "cross-module", "large change"} {
		if strings.Contains(s, term) && risk < 2 {
			risk = 2
			reasons = append(reasons, term)
		}
	}
	for _, term := range []string{"auth", "database", "payment", "security", "migration", "critical api", "支付", "认证", "数据库", "迁移", "安全"} {
		if strings.Contains(s, term) && risk < 3 {
			risk = 3
			reasons = append(reasons, term)
		}
	}
	for _, term := range []string{"production", "destructive", "credential", "irreversible", "生产", "凭据", "不可逆", "销毁"} {
		if strings.Contains(s, term) {
			risk = 4
			reasons = append(reasons, term)
		}
	}
	return risk, reasons
}

// Writable validates an integration boundary, not an OS sandbox.
func Writable(role, file string) bool {
	clean := path.Clean(strings.ReplaceAll(file, "\\", "/"))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, ":") {
		return false
	}
	for _, p := range strings.Split(clean, "/") {
		if p == ".git" {
			return false
		}
	}
	switch role {
	case "explorer", "review":
		return false
	case "frontend":
		return strings.HasPrefix(clean, "frontend/") || strings.HasPrefix(clean, "web/") || strings.HasPrefix(clean, "internal/web/static/")
	case "qa":
		return strings.HasSuffix(clean, "_test.go") || strings.HasPrefix(clean, "tests/") || strings.HasPrefix(clean, "docs/")
	case "lead":
		return true
	}
	return false
}

func Simple(goal string) []Task {
	plan := []Task{
		{ID: "analysis", Role: "explorer", Prompt: "Read the repository and report constraints and impact for: " + goal, Capabilities: []string{"exploration"}, MaxAttempts: 3},
		{ID: "build", Role: "lead", Prompt: "Implement the goal, inspect project memory and analysis. Commit changes only on this task branch. Goal: " + goal, DependsOn: []string{"analysis"}, Capabilities: []string{"coding"}, MaxAttempts: 3},
		{ID: "tests", Role: "qa", Prompt: "Run the project checks; report regressions and evidence. Do not change implementation. Goal: " + goal, DependsOn: []string{"build"}, Capabilities: []string{"testing"}, MaxAttempts: 3},
		{ID: "review", Role: "review", Prompt: "Independently review the committed goal and test evidence. Report findings with file references; do not change files. Goal: " + goal, DependsOn: []string{"tests"}, Capabilities: []string{"review"}, MaxAttempts: 3},
	}
	text := strings.ToLower(goal)
	if strings.Contains(text, "frontend") || strings.Contains(text, "前端") || strings.Contains(text, "界面") || strings.Contains(text, "user interface") {
		frontend := Task{ID: "frontend", Role: "frontend", Prompt: "Implement frontend and UX only in allowed frontend paths. Report backend/API gaps to lead; never change backend. Goal: " + goal, DependsOn: []string{"build"}, Capabilities: []string{"frontend"}, MaxAttempts: 3}
		plan = append(plan[:2], append([]Task{frontend}, plan[2:]...)...)
		plan[3].DependsOn = []string{"frontend"}
	}
	return plan
}

// Extension is a public boundary; Community has no enterprise implementation dependency.
type Extension = extensions.Extension
type Entitlements = extensions.Entitlements
type CommunityEntitlements = extensions.CommunityEntitlements
