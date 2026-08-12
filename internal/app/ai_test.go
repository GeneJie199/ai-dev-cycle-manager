package app

import (
	"context"
	"strings"
	"testing"
)

func TestApplyAIPlanPersistsSelectedItemsAndDependencies(t *testing.T) {
	a := openTestApp(t)
	ctx := context.Background()
	requirement, err := a.CreateRequirement(ctx, "可发布的登录流程", "支持会话撤销")
	if err != nil {
		t.Fatal(err)
	}

	applied, err := a.ApplyAIPlan(ctx, requirement.ID,
		[]AIPlanCriterion{{Description: "登录成功返回非空会话标识", Rationale: "可由接口测试核验"}},
		[]AIPlanTask{
			{Title: "实现登录接口", Description: "返回会话标识"},
			{Title: "实现注销接口", Description: "撤销现有会话", DependsOn: []string{"实现登录接口"}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Criteria) != 1 || len(applied.Tasks) != 2 {
		t.Fatalf("applied=%+v", applied)
	}
	if got := applied.Tasks[1].DependsOn; len(got) != 1 || got[0] != applied.Tasks[0].ID {
		t.Fatalf("dependency ids=%v", got)
	}
	persisted, err := a.ListTasksByRequirement(ctx, requirement.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted[1].DependsOn; len(got) != 1 || got[0] != persisted[0].ID {
		t.Fatalf("persisted dependencies=%v", got)
	}
}

func TestValidateAIPlanRejectsDuplicateAndForwardDependency(t *testing.T) {
	plan := AIPlanPreview{Tasks: []AIPlanTask{{Title: "后置任务", DependsOn: []string{"尚未出现"}}}}
	if err := validateAIPlan(&plan, nil, nil); err == nil || !strings.Contains(err.Error(), "unknown or later") {
		t.Fatalf("unexpected error: %v", err)
	}

	plan = AIPlanPreview{Criteria: []AIPlanCriterion{{Description: "接口必须返回二百状态"}, {Description: "  接口必须返回二百状态  "}}}
	if err := validateAIPlan(&plan, nil, nil); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("unexpected error: %v", err)
	}
}
