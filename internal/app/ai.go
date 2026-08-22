package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	devai "github.com/GeneJie199/ai-dev-cycle-manager/internal/ai"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/google/uuid"
)

type AIPlanCriterion = models.PlanCriterion
type AIPlanTask = models.PlanTask

type AIPlanPreview struct {
	SchemaVersion    string                  `json:"schemaVersion"`
	RequirementID    string                  `json:"requirementId"`
	Understanding    string                  `json:"understanding"`
	Scope            models.PlanScope        `json:"scope"`
	Assumptions      []string                `json:"assumptions"`
	OpenQuestions    []models.PlanQuestion   `json:"openQuestions"`
	Criteria         []AIPlanCriterion       `json:"criteria"`
	TestCases        []models.PlanTestCase   `json:"testCases"`
	TestStrategy     models.PlanTestStrategy `json:"testStrategy"`
	Tasks            []AIPlanTask            `json:"tasks"`
	Risks            []models.PlanRisk       `json:"risks"`
	RollbackConcerns []string                `json:"rollbackConcerns"`
	CandidateNotes   string                  `json:"candidateNotes"`
	Source           string                  `json:"source"`
	Provider         string                  `json:"provider,omitempty"`
	Status           string                  `json:"status"`
	Revision         int                     `json:"revision"`
	Meta             devai.GenerationMeta    `json:"meta"`
}

type AppliedAIPlan struct {
	Criteria []models.AcceptanceCriterion `json:"criteria"`
	Tasks    []models.Task                `json:"tasks"`
	Plan     models.PlanningDocument      `json:"plan"`
}

func (a *App) GenerateAIPlan(ctx context.Context, requirementID, provider, additionalContext string) (AIPlanPreview, error) {
	requirement, err := a.Store.GetRequirement(ctx, requirementID)
	if err != nil {
		return AIPlanPreview{}, fmt.Errorf("requirement: %w", err)
	}
	criteria, err := a.Store.ListCriteriaByRequirement(ctx, requirementID)
	if err != nil {
		return AIPlanPreview{}, err
	}
	tasks, err := a.Store.ListTasksByRequirement(ctx, requirementID)
	if err != nil {
		return AIPlanPreview{}, err
	}
	var previousPlan any
	revision := 0
	if stored, planErr := a.Store.GetPlanningDocument(ctx, requirementID); planErr == nil {
		previousPlan = stored
		revision = stored.Revision
	} else if !errors.Is(planErr, sql.ErrNoRows) {
		return AIPlanPreview{}, planErr
	}
	input, _ := json.Marshal(map[string]any{"requirement": requirement, "existingCriteria": criteria, "existingTasks": tasks, "previousPlan": previousPlan, "additionalContext": strings.TrimSpace(additionalContext)})
	prompt := `你是资深软件交付规划师。根据输入生成一份可编辑、可执行、可核验的软件交付计划。
规则：
1. 不重复已有条目，不臆造已完成的实现或证据。
2. 信息不是硬阻塞时，给出清晰假设和 suggestedDefault，不反复追问；真正阻塞时 blocking=true。
3. included/excluded 明确功能边界；验收标准必须客观可验证。
4. 每个测试用例关联一个验收标准的完整描述，kind 只能是 unit、integration、e2e、manual、security、performance。
5. 任务按依赖顺序排列，order 从 1 连续递增；dependsOn 使用前置任务完整标题；suggestedAdapter 从 codex、claude、gemini、kimi、opencode、custom、human 中选择。
6. 风险 severity 只能是 low、medium、high、critical，并给出 mitigation；明确回滚关注点与发布候选说明。
7. 只输出一个 JSON 对象，不要 Markdown、代码围栏或额外字段。严格使用：
{"understanding":"...","scope":{"included":["..."],"excluded":["..."]},"assumptions":["..."],"openQuestions":[{"question":"...","blocking":false,"suggestedDefault":"..."}],"criteria":[{"description":"...","rationale":"..."}],"testCases":[{"title":"...","criterion":"验收标准完整描述","kind":"integration","setup":["..."],"steps":["..."],"expected":["..."]}],"testStrategy":{"summary":"...","environments":["..."],"commands":["..."]},"tasks":[{"title":"...","description":"...","dependsOn":[],"rationale":"...","order":1,"suggestedAdapter":"codex","expectedDeliverables":["..."]}],"risks":[{"risk":"...","severity":"medium","mitigation":"..."}],"rollbackConcerns":["..."],"candidateNotes":"..."}
输入数据：
` + string(input)
	var raw struct {
		Understanding    string                  `json:"understanding"`
		Scope            models.PlanScope        `json:"scope"`
		Assumptions      []string                `json:"assumptions"`
		OpenQuestions    []models.PlanQuestion   `json:"openQuestions"`
		Criteria         []AIPlanCriterion       `json:"criteria"`
		TestCases        []models.PlanTestCase   `json:"testCases"`
		TestStrategy     models.PlanTestStrategy `json:"testStrategy"`
		Tasks            []AIPlanTask            `json:"tasks"`
		Risks            []models.PlanRisk       `json:"risks"`
		RollbackConcerns []string                `json:"rollbackConcerns"`
		CandidateNotes   string                  `json:"candidateNotes"`
	}
	meta, err := a.AI.GenerateJSON(ctx, provider, "plan", prompt, &raw)
	if err != nil {
		return AIPlanPreview{}, err
	}
	preview := AIPlanPreview{SchemaVersion: models.PlanningDocumentSchemaV1, RequirementID: requirementID, Understanding: raw.Understanding, Scope: raw.Scope, Assumptions: raw.Assumptions, OpenQuestions: raw.OpenQuestions, Criteria: raw.Criteria, TestCases: raw.TestCases, TestStrategy: raw.TestStrategy, Tasks: raw.Tasks, Risks: raw.Risks, RollbackConcerns: raw.RollbackConcerns, CandidateNotes: raw.CandidateNotes, Source: "ai", Provider: provider, Status: "draft", Revision: revision, Meta: meta}
	if err := validateAIPlan(&preview, criteria, tasks); err != nil {
		return AIPlanPreview{}, fmt.Errorf("AI plan failed validation: %w", err)
	}
	if err := validateGeneratedPlanCompleteness(&preview); err != nil {
		return AIPlanPreview{}, fmt.Errorf("AI plan is incomplete: %w", err)
	}
	return preview, nil
}

func (a *App) ApplyAIPlan(ctx context.Context, requirementID string, suggestedCriteria []AIPlanCriterion, suggestedTasks []AIPlanTask) (AppliedAIPlan, error) {
	document := models.PlanningDocument{SchemaVersion: models.PlanningDocumentSchemaV1, RequirementID: requirementID, Understanding: "Plan reviewed through the compatibility planning API.", Criteria: suggestedCriteria, Tasks: suggestedTasks, Source: "ai", Status: "draft"}
	return a.ApplyPlanningDocument(ctx, requirementID, document)
}

func (a *App) GetPlanningDocument(ctx context.Context, requirementID string) (models.PlanningDocument, error) {
	if _, err := a.Store.GetRequirement(ctx, requirementID); err != nil {
		return models.PlanningDocument{}, fmt.Errorf("requirement: %w", err)
	}
	return a.Store.GetPlanningDocument(ctx, requirementID)
}

func (a *App) SavePlanningDocument(ctx context.Context, requirementID string, document models.PlanningDocument) (models.PlanningDocument, error) {
	if _, err := a.Store.GetRequirement(ctx, requirementID); err != nil {
		return models.PlanningDocument{}, fmt.Errorf("requirement: %w", err)
	}
	document.RequirementID = requirementID
	document.SchemaVersion = models.PlanningDocumentSchemaV1
	document.Status = "draft"
	document.AppliedAt = time.Time{}
	if document.Source == "" {
		document.Source = "manual"
	}
	if document.Source != "manual" && document.Source != "ai" {
		return models.PlanningDocument{}, validationf("plan source must be manual or ai")
	}
	preview := previewFromPlanningDocument(document)
	if err := validateAIPlan(&preview, nil, nil); err != nil {
		return models.PlanningDocument{}, validationf("%s", err.Error())
	}
	return a.Store.SavePlanningDocument(ctx, planningDocumentFromPreview(preview))
}

func (a *App) ApplyPlanningDocument(ctx context.Context, requirementID string, document models.PlanningDocument) (AppliedAIPlan, error) {
	if _, err := a.Store.GetRequirement(ctx, requirementID); err != nil {
		return AppliedAIPlan{}, fmt.Errorf("requirement: %w", err)
	}
	existingCriteria, err := a.Store.ListCriteriaByRequirement(ctx, requirementID)
	if err != nil {
		return AppliedAIPlan{}, err
	}
	existingTasks, err := a.Store.ListTasksByRequirement(ctx, requirementID)
	if err != nil {
		return AppliedAIPlan{}, err
	}
	document.RequirementID = requirementID
	document.SchemaVersion = models.PlanningDocumentSchemaV1
	document.Status = "draft"
	document.AppliedAt = time.Time{}
	if document.Source == "" {
		document.Source = "manual"
	}
	preview := previewFromPlanningDocument(document)
	if err := validateAIPlan(&preview, existingCriteria, existingTasks); err != nil {
		return AppliedAIPlan{}, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	criteria := make([]models.AcceptanceCriterion, 0, len(preview.Criteria))
	for _, suggestion := range preview.Criteria {
		criteria = append(criteria, models.AcceptanceCriterion{ID: uuid.NewString(), RequirementID: requirementID, Description: suggestion.Description, CreatedAt: now})
	}
	tasks := make([]models.Task, 0, len(preview.Tasks))
	taskIDsByTitle := make(map[string]string, len(preview.Tasks)+len(existingTasks))
	for _, task := range existingTasks {
		taskIDsByTitle[normalizedText(task.Title)] = task.ID
	}
	for _, suggestion := range preview.Tasks {
		id := uuid.NewString()
		dependencyIDs := make([]string, 0, len(suggestion.DependsOn))
		for _, dependency := range suggestion.DependsOn {
			dependencyIDs = append(dependencyIDs, taskIDsByTitle[normalizedText(dependency)])
		}
		tasks = append(tasks, models.Task{ID: id, RequirementID: requirementID, Title: suggestion.Title, Description: suggestion.Description, Status: models.TaskStatusTodo, DependsOn: dependencyIDs, CreatedAt: now, UpdatedAt: now})
		taskIDsByTitle[normalizedText(suggestion.Title)] = id
	}
	saved, err := a.Store.ApplyPlanningDocument(ctx, planningDocumentFromPreview(preview), criteria, tasks)
	if err != nil {
		return AppliedAIPlan{}, err
	}
	return AppliedAIPlan{Criteria: criteria, Tasks: tasks, Plan: saved}, nil
}

func previewFromPlanningDocument(document models.PlanningDocument) AIPlanPreview {
	return AIPlanPreview{SchemaVersion: document.SchemaVersion, RequirementID: document.RequirementID, Understanding: document.Understanding, Scope: document.Scope, Assumptions: document.Assumptions, OpenQuestions: document.OpenQuestions, Criteria: document.Criteria, TestCases: document.TestCases, TestStrategy: document.TestStrategy, Tasks: document.Tasks, Risks: document.Risks, RollbackConcerns: document.RollbackConcerns, CandidateNotes: document.CandidateNotes, Source: document.Source, Provider: document.Provider, Status: document.Status, Revision: document.Revision}
}

func planningDocumentFromPreview(preview AIPlanPreview) models.PlanningDocument {
	return models.PlanningDocument{SchemaVersion: models.PlanningDocumentSchemaV1, RequirementID: preview.RequirementID, Understanding: preview.Understanding, Scope: preview.Scope, Assumptions: preview.Assumptions, OpenQuestions: preview.OpenQuestions, Criteria: preview.Criteria, TestCases: preview.TestCases, TestStrategy: preview.TestStrategy, Tasks: preview.Tasks, Risks: preview.Risks, RollbackConcerns: preview.RollbackConcerns, CandidateNotes: preview.CandidateNotes, Source: preview.Source, Provider: preview.Provider, Status: preview.Status, Revision: preview.Revision}
}

func validateAIPlan(plan *AIPlanPreview, existingCriteria []models.AcceptanceCriterion, existingTasks []models.Task) error {
	if len(plan.Criteria) == 0 && len(plan.Tasks) == 0 {
		return errors.New("plan must contain at least one criterion or task")
	}
	if len(plan.Criteria) > 20 || len(plan.Tasks) > 30 || len(plan.Assumptions) > 20 || len(plan.Risks) > 20 || len(plan.OpenQuestions) > 20 || len(plan.TestCases) > 50 || len(plan.RollbackConcerns) > 20 || len(plan.Scope.Included) > 50 || len(plan.Scope.Excluded) > 50 {
		return errors.New("plan exceeds item limits")
	}
	plan.Understanding = strings.TrimSpace(plan.Understanding)
	if utf8.RuneCountInString(plan.Understanding) > 12000 {
		return errors.New("plan understanding is too long")
	}
	if err := normalizePlanStrings(plan.Scope.Included, "included scope", 2000); err != nil {
		return err
	}
	if err := normalizePlanStrings(plan.Scope.Excluded, "excluded scope", 2000); err != nil {
		return err
	}
	criterionNames := map[string]bool{}
	for _, criterion := range existingCriteria {
		criterionNames[normalizedText(criterion.Description)] = true
	}
	for index := range plan.Criteria {
		item := &plan.Criteria[index]
		item.Description = strings.TrimSpace(item.Description)
		item.Rationale = strings.TrimSpace(item.Rationale)
		key := normalizedText(item.Description)
		if utf8.RuneCountInString(item.Description) < 5 || utf8.RuneCountInString(item.Description) > 2000 || utf8.RuneCountInString(item.Rationale) > 2000 {
			return fmt.Errorf("criterion %d has invalid length", index+1)
		}
		if criterionNames[key] {
			return fmt.Errorf("duplicate criterion %q", item.Description)
		}
		criterionNames[key] = true
	}
	taskNames := map[string]bool{}
	for _, task := range existingTasks {
		taskNames[normalizedText(task.Title)] = true
	}
	generatedTaskNames := map[string]bool{}
	for index := range plan.Tasks {
		item := &plan.Tasks[index]
		item.Title = strings.TrimSpace(item.Title)
		item.Description = strings.TrimSpace(item.Description)
		item.Rationale = strings.TrimSpace(item.Rationale)
		item.SuggestedAdapter = strings.ToLower(strings.TrimSpace(item.SuggestedAdapter))
		if item.Order == 0 {
			item.Order = index + 1
		}
		key := normalizedText(item.Title)
		if utf8.RuneCountInString(item.Title) < 2 || utf8.RuneCountInString(item.Title) > 300 || utf8.RuneCountInString(item.Description) > 8000 || utf8.RuneCountInString(item.Rationale) > 2000 || len(item.DependsOn) > 20 || item.Order != index+1 {
			return fmt.Errorf("task %d has invalid length", index+1)
		}
		if item.SuggestedAdapter != "" && !containsString([]string{"codex", "claude", "gemini", "kimi", "opencode", "custom", "human"}, item.SuggestedAdapter) {
			return fmt.Errorf("task %q has an unsupported suggested adapter", item.Title)
		}
		if len(item.ExpectedDeliverables) > 20 {
			return fmt.Errorf("task %q has too many expected deliverables", item.Title)
		}
		if err := normalizePlanStrings(item.ExpectedDeliverables, "expected deliverable", 2000); err != nil {
			return fmt.Errorf("task %q: %w", item.Title, err)
		}
		if taskNames[key] || generatedTaskNames[key] {
			return fmt.Errorf("duplicate task %q", item.Title)
		}
		for _, dependency := range item.DependsOn {
			if !generatedTaskNames[normalizedText(dependency)] && !taskNames[normalizedText(dependency)] {
				return fmt.Errorf("task %q references unknown or later dependency %q", item.Title, dependency)
			}
		}
		generatedTaskNames[key] = true
	}
	for index := range plan.Assumptions {
		plan.Assumptions[index] = strings.TrimSpace(plan.Assumptions[index])
		if plan.Assumptions[index] == "" || utf8.RuneCountInString(plan.Assumptions[index]) > 2000 {
			return fmt.Errorf("assumption %d is invalid", index+1)
		}
	}
	for index := range plan.OpenQuestions {
		question := &plan.OpenQuestions[index]
		question.Question = strings.TrimSpace(question.Question)
		question.SuggestedDefault = strings.TrimSpace(question.SuggestedDefault)
		if question.Question == "" || utf8.RuneCountInString(question.Question) > 2000 || utf8.RuneCountInString(question.SuggestedDefault) > 2000 || (!question.Blocking && question.SuggestedDefault == "") {
			return fmt.Errorf("open question %d is invalid", index+1)
		}
	}
	for index := range plan.TestCases {
		testCase := &plan.TestCases[index]
		testCase.Title = strings.TrimSpace(testCase.Title)
		testCase.Criterion = strings.TrimSpace(testCase.Criterion)
		testCase.Kind = strings.ToLower(strings.TrimSpace(testCase.Kind))
		if utf8.RuneCountInString(testCase.Title) < 2 || utf8.RuneCountInString(testCase.Title) > 300 || !criterionNames[normalizedText(testCase.Criterion)] || !containsString([]string{"unit", "integration", "e2e", "manual", "security", "performance"}, testCase.Kind) {
			return fmt.Errorf("test case %d is invalid", index+1)
		}
		if len(testCase.Setup) > 20 || len(testCase.Steps) == 0 || len(testCase.Steps) > 50 || len(testCase.Expected) == 0 || len(testCase.Expected) > 50 {
			return fmt.Errorf("test case %q has invalid step counts", testCase.Title)
		}
		for label, values := range map[string][]string{"setup": testCase.Setup, "step": testCase.Steps, "expected result": testCase.Expected} {
			if err := normalizePlanStrings(values, label, 2000); err != nil {
				return fmt.Errorf("test case %q: %w", testCase.Title, err)
			}
		}
	}
	plan.TestStrategy.Summary = strings.TrimSpace(plan.TestStrategy.Summary)
	if utf8.RuneCountInString(plan.TestStrategy.Summary) > 8000 || len(plan.TestStrategy.Environments) > 20 || len(plan.TestStrategy.Commands) > 50 {
		return errors.New("test strategy is invalid")
	}
	if err := normalizePlanStrings(plan.TestStrategy.Environments, "test environment", 2000); err != nil {
		return err
	}
	if err := normalizePlanStrings(plan.TestStrategy.Commands, "test command", 4000); err != nil {
		return err
	}
	for index := range plan.Risks {
		risk := &plan.Risks[index]
		risk.Risk = strings.TrimSpace(risk.Risk)
		risk.Severity = strings.ToLower(strings.TrimSpace(risk.Severity))
		risk.Mitigation = strings.TrimSpace(risk.Mitigation)
		if risk.Risk == "" || utf8.RuneCountInString(risk.Risk) > 2000 || !containsString([]string{"low", "medium", "high", "critical"}, risk.Severity) || risk.Mitigation == "" || utf8.RuneCountInString(risk.Mitigation) > 4000 {
			return fmt.Errorf("risk %d is invalid", index+1)
		}
	}
	if err := normalizePlanStrings(plan.RollbackConcerns, "rollback concern", 2000); err != nil {
		return err
	}
	plan.CandidateNotes = strings.TrimSpace(plan.CandidateNotes)
	if utf8.RuneCountInString(plan.CandidateNotes) > 8000 {
		return errors.New("candidate notes are too long")
	}
	return nil
}

func validateGeneratedPlanCompleteness(plan *AIPlanPreview) error {
	if utf8.RuneCountInString(plan.Understanding) < 5 || len(plan.Scope.Included) == 0 || len(plan.Criteria) == 0 || len(plan.TestCases) == 0 || strings.TrimSpace(plan.TestStrategy.Summary) == "" || len(plan.Tasks) == 0 || len(plan.RollbackConcerns) == 0 || strings.TrimSpace(plan.CandidateNotes) == "" {
		return errors.New("understanding, included scope, criteria, test cases, test strategy, tasks, rollback concerns, and candidate notes are required")
	}
	covered := map[string]bool{}
	for _, testCase := range plan.TestCases {
		covered[normalizedText(testCase.Criterion)] = true
	}
	for _, criterion := range plan.Criteria {
		if !covered[normalizedText(criterion.Description)] {
			return fmt.Errorf("criterion %q has no test case", criterion.Description)
		}
	}
	for _, task := range plan.Tasks {
		if task.SuggestedAdapter == "" || len(task.ExpectedDeliverables) == 0 {
			return fmt.Errorf("task %q needs an adapter suggestion and expected deliverables", task.Title)
		}
	}
	return nil
}

func normalizePlanStrings(values []string, label string, maximum int) error {
	for index := range values {
		values[index] = strings.TrimSpace(values[index])
		if values[index] == "" || utf8.RuneCountInString(values[index]) > maximum {
			return fmt.Errorf("%s %d is invalid", label, index+1)
		}
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func normalizedText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
