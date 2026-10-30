package team

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func missionFixture() Mission {
	return Mission{ProjectID: "project", Repository: "repo", Goal: "Add quickstart", Mode: "simple", Risk: 1, Plan: Simple("Add quickstart"), Checks: []string{"go test ./..."}, Acceptance: []string{"tests pass"}, Models: []Model{{ID: "strong", Provider: "local", Adapter: "agent", Capabilities: Capabilities, ContextLimit: 100000, Tier: 2, Available: true, Tools: true}}, Budget: Budget{MaxExecutions: 12, TimeoutSeconds: 60}, Policy: Policy{AllowedProviders: []string{"local"}}}
}
func TestValidateDAGAndRegistry(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Mission)
	}{
		{"cycle", func(m *Mission) { m.Plan[0].DependsOn = []string{"review"} }},
		{"missing dependency", func(m *Mission) { m.Plan[1].DependsOn = []string{"missing"} }},
		{"duplicate task", func(m *Mission) { m.Plan[1].ID = m.Plan[0].ID }},
		{"path traversal", func(m *Mission) { m.Plan[0].ID = "../escape" }},
		{"invalid capability", func(m *Mission) { m.Models[0].Capabilities = []string{"magic"} }},
		{"duplicate model", func(m *Mission) { m.Models = append(m.Models, m.Models[0]) }},
		{"negative budget", func(m *Mission) { m.Budget.MaxExecutions = -1 }},
		{"invalid role", func(m *Mission) { m.Plan[0].Role = "root" }},
		{"no checks", func(m *Mission) { m.Checks = nil }},
		{"no review", func(m *Mission) { m.Plan = m.Plan[:3] }},
	}
	if err := Validate(missionFixture()); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := missionFixture()
			tt.mutate(&m)
			if Validate(m) == nil {
				t.Fatal("malformed config accepted")
			}
		})
	}
}
func TestRoutePoliciesFallbackAndBudgets(t *testing.T) {
	m := missionFixture()
	task := m.Plan[0]
	cheap := m.Models[0]
	cheap.ID = "cheap"
	cheap.Tier = 0
	normal := cheap
	normal.ID = "normal"
	normal.Tier = 1
	m.Models = append(m.Models, cheap, normal)
	available := map[string]bool{"agent": true}
	out, err := Route(m, task, available)
	if err != nil || out[0].ID != "cheap" {
		t.Fatalf("cheap route: %v %v", out, err)
	}
	task.Attempts = 1
	out, err = Route(m, task, available)
	if err != nil || out[0].ID != "normal" {
		t.Fatal("normal fallback missing")
	}
	task.Attempts = 2
	out, err = Route(m, task, available)
	if err != nil || out[0].ID != "strong" {
		t.Fatal("strong fallback missing")
	}
	task.Attempts = 3
	if _, err = Route(m, task, available); err == nil {
		t.Fatal("escalation failed")
	}
	task.Attempts = 0
	for _, name := range []string{"unavailable", "capability", "context", "provider", "budget", "enforcement", "tokens", "cost"} {
		t.Run(name, func(t *testing.T) {
			v := m
			v.Models = append([]Model{}, m.Models...)
			task := task
			switch name {
			case "unavailable":
				available = map[string]bool{}
				defer func() { available = map[string]bool{"agent": true} }()
			case "capability":
				task.Capabilities = []string{"vision-missing"}
			case "context":
				task.ContextSize = 200000
			case "provider":
				v.Policy.AllowedProviders = nil
			case "budget":
				v.Budget.MaxExecutions = 0
			case "enforcement":
				v.Policy.RequiredEnforcement = []string{"readonly"}
			case "tokens":
				v.Budget.MaxTokens = 10
			case "cost":
				cost := 1.0
				v.Budget.MaxCost = &cost
			}
			if _, err := Route(v, task, available); err == nil {
				t.Fatal("policy failed open")
			}
		})
	}
	m.Models[0].Provider = "remote"
	m.Models = m.Models[:1]
	m.Policy.AllowedProviders = []string{"remote"}
	if _, err = Route(m, task, available); err == nil {
		t.Fatal("implicit source export")
	}
	m.Policy.SourceExport = true
	if _, err = Route(m, task, available); err != nil {
		t.Fatal(err)
	}
}
func TestReadyOnlyAfterDependencies(t *testing.T) {
	m := missionFixture()
	for i := range m.Plan {
		m.Plan[i].State = "pending"
	}
	ready := Ready(&m)
	if len(ready) != 1 || ready[0] != 0 {
		t.Fatal(ready)
	}
	m.Plan[0].State = "failed"
	if len(Ready(&m)) != 0 {
		t.Fatal("failed dependency released work")
	}
	m.Plan[0].State = "completed"
	ready = Ready(&m)
	if len(ready) != 1 || ready[0] != 1 {
		t.Fatal(ready)
	}
}
func TestRiskAndWriteBoundaries(t *testing.T) {
	for _, v := range []struct {
		goal string
		risk int
	}{{"docs", 1}, {"cross-module change", 2}, {"payment API", 3}, {"production payment", 4}, {"数据库", 3}, {"生产", 4}} {
		risk, _ := Assess(v.goal, nil)
		if risk != v.risk {
			t.Fatalf("%s risk=%d", v.goal, risk)
		}
	}
	r, _ := Assess("docs", []string{"README.md"})
	if r != 0 {
		t.Fatal(r)
	}
	for _, v := range []struct {
		role, file string
		want       bool
	}{{"frontend", "internal/web/static/app.js", true}, {"frontend", "internal/app/backend.go", false}, {"review", "README.md", false}, {"explorer", "docs/a.md", false}, {"qa", "internal/app/a_test.go", true}, {"qa", "internal/app/a.go", false}, {"lead", "../oops", false}, {"lead", ".git/config", false}, {"lead", "C:/escape", false}} {
		if Writable(v.role, v.file) != v.want {
			t.Fatalf("%+v", v)
		}
	}
}
func TestStoreCASRecoveryAndMemoryHistory(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "team.db")
	s, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	m := missionFixture()
	m.ID = "mission"
	m.State = "running"
	if err = s.Save(ctx, &m); err != nil {
		t.Fatal(err)
	}
	stale := m
	if err = s.Save(ctx, &m); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.Save(ctx, &stale), ErrConflict) {
		t.Fatal("stale write accepted")
	}
	mem := Memory{ID: "decision", ProjectID: "project", Kind: "architecture", Content: "HTTP contracts", Source: "ADR-1", Author: "maintainer"}
	mem, err = s.SaveMemory(ctx, mem)
	if err != nil {
		t.Fatal(err)
	}
	staleMem := mem
	mem.Content = "versioned HTTP contracts"
	mem, err = s.SaveMemory(ctx, mem)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveMemory(ctx, staleMem); !errors.Is(err, ErrConflict) {
		t.Fatal("memory lost update")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restored, err := s.Get(ctx, m.ID)
	if err != nil || restored.State != "running" || restored.Revision != m.Revision {
		t.Fatal("restart lost mission")
	}
	current, err := s.Memory(ctx, "project", false)
	if err != nil || len(current) != 1 || current[0].Revision != 2 {
		t.Fatal(current, err)
	}
	history, err := s.Memory(ctx, "project", true)
	if err != nil || len(history) != 2 {
		t.Fatal(history, err)
	}
	foreign, _ := s.Memory(ctx, "another-project", true)
	if len(foreign) != 0 {
		t.Fatal("cross-project memory leak")
	}
	mem.Kind = "chat"
	if _, err = s.SaveMemory(ctx, mem); err == nil {
		t.Fatal("unstructured memory accepted")
	}
}
