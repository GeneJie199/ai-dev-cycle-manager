package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	devai "github.com/GeneJie199/ai-dev-cycle-manager/internal/ai"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/google/uuid"
)

type AIPlanCriterion struct {
	Description string `json:"description"`
	Rationale   string `json:"rationale"`
}

type AIPlanTask struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	DependsOn   []string `json:"dependsOn"`
	Rationale   string   `json:"rationale"`
}

type AIPlanPreview struct {
	Criteria    []AIPlanCriterion    `json:"criteria"`
	Tasks       []AIPlanTask         `json:"tasks"`
	Assumptions []string             `json:"assumptions"`
	Risks       []string             `json:"risks"`
	Meta        devai.GenerationMeta `json:"meta"`
}

type AppliedAIPlan struct {
	Criteria []models.AcceptanceCriterion `json:"criteria"`
	Tasks    []models.Task                `json:"tasks"`
}

func (a *App) AIProviders() []devai.ProviderStatus {
	return a.AI.Providers()
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
	input, _ := json.Marshal(map[string]any{"requirement": requirement, "existingCriteria": criteria, "existingTasks": tasks, "additionalContext": strings.TrimSpace(additionalContext)})
	prompt := `你是资深软件交付规划师。根据下面的需求生成可核验的验收标准与可独立执行的工程任务。
规则：
1. 不重复已有条目，不臆造已经完成的实现。
2. 验收标准必须可由测试、接口响应、界面行为或人工检查客观核对，避免“体验良好”等空话。
3. 任务要有明确边界和产出，按依赖顺序排列；dependsOn 使用本次建议中前置任务的完整标题。
4. 明确列出仍需产品或工程负责人确认的假设和风险。
5. 只输出一个 JSON 对象，不要 Markdown、代码围栏或解释文字。严格使用这个结构：
{"criteria":[{"description":"...","rationale":"..."}],"tasks":[{"title":"...","description":"...","dependsOn":["前置任务标题"],"rationale":"..."}],"assumptions":["..."],"risks":["..."]}
输入数据：
` + string(input)
	var raw struct {
		Criteria    []AIPlanCriterion `json:"criteria"`
		Tasks       []AIPlanTask      `json:"tasks"`
		Assumptions []string          `json:"assumptions"`
		Risks       []string          `json:"risks"`
	}
	meta, err := a.AI.GenerateJSON(ctx, provider, "plan", prompt, &raw)
	if err != nil {
		return AIPlanPreview{}, err
	}
	preview := AIPlanPreview{Criteria: raw.Criteria, Tasks: raw.Tasks, Assumptions: raw.Assumptions, Risks: raw.Risks, Meta: meta}
	if err := validateAIPlan(&preview, criteria, tasks); err != nil {
		return AIPlanPreview{}, fmt.Errorf("AI plan failed validation: %w", err)
	}
	return preview, nil
}

func (a *App) ApplyAIPlan(ctx context.Context, requirementID string, suggestedCriteria []AIPlanCriterion, suggestedTasks []AIPlanTask) (AppliedAIPlan, error) {
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
	preview := AIPlanPreview{Criteria: suggestedCriteria, Tasks: suggestedTasks}
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
	if err := a.Store.InsertPlan(ctx, criteria, tasks); err != nil {
		return AppliedAIPlan{}, err
	}
	return AppliedAIPlan{Criteria: criteria, Tasks: tasks}, nil
}

func validateAIPlan(plan *AIPlanPreview, existingCriteria []models.AcceptanceCriterion, existingTasks []models.Task) error {
	if len(plan.Criteria) == 0 && len(plan.Tasks) == 0 {
		return errors.New("plan must contain at least one criterion or task")
	}
	if len(plan.Criteria) > 20 || len(plan.Tasks) > 30 || len(plan.Assumptions) > 20 || len(plan.Risks) > 20 {
		return errors.New("plan exceeds item limits")
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
		if len(item.Description) < 5 || len(item.Description) > 2000 || len(item.Rationale) > 2000 {
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
		key := normalizedText(item.Title)
		if len(item.Title) < 2 || len(item.Title) > 300 || len(item.Description) > 8000 || len(item.Rationale) > 2000 || len(item.DependsOn) > 20 {
			return fmt.Errorf("task %d has invalid length", index+1)
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
		if plan.Assumptions[index] == "" || len(plan.Assumptions[index]) > 2000 {
			return fmt.Errorf("assumption %d is invalid", index+1)
		}
	}
	for index := range plan.Risks {
		plan.Risks[index] = strings.TrimSpace(plan.Risks[index])
		if plan.Risks[index] == "" || len(plan.Risks[index]) > 2000 {
			return fmt.Errorf("risk %d is invalid", index+1)
		}
	}
	return nil
}

func normalizedText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
