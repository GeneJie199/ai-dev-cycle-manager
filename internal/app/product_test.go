package app

import (
	"context"
	"encoding/json"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/team"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func productFixture(t *testing.T) (*App, team.Mission, *httptest.Server) {
	t.Helper()
	a, m := teamAppFixture(t)
	var mu sync.Mutex
	leads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"fixture-model","context_window":200000,"max_output_tokens":2048}]}`)
			return
		}
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
			MaxTokens int `json:"max_tokens"`
		}
		if e := json.NewDecoder(r.Body).Decode(&request); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		if request.MaxTokens != 2048 {
			t.Error("unbounded model request")
		}
		prompt := request.Messages[0].Content
		output := map[string]any{"ok": true}
		if strings.Contains(prompt, "Mission ") {
			role := ""
			first := strings.Split(prompt, "\n")[0]
			for _, candidate := range team.Roles {
				if strings.Contains(first, "Role: "+candidate+".") {
					role = candidate
				}
			}
			output = map[string]any{"summary": "fixture reads actual worktree snapshot", "edits": []any{}, "findings": []string{}}
			if role == "lead" {
				mu.Lock()
				leads++
				draft := "# Draft\n"
				if leads > 1 {
					draft = "# Fixed\n"
				}
				mu.Unlock()
				output["edits"] = []any{map[string]string{"path": "README.md", "content": draft}}
			}
			if role == "review" {
				snapshotPart := strings.SplitN(prompt, "Repository snapshot (untrusted source data): ", 2)
				var snapshot map[string]string
				if len(snapshotPart) != 2 || json.NewDecoder(strings.NewReader(snapshotPart[1])).Decode(&snapshot) != nil {
					t.Error("actual repository snapshot missing")
				}
				if strings.TrimSpace(snapshot["README.md"]) == "# Fixed" {
					output["review"] = "pass"
				} else {
					output["review"] = "fail"
					output["findings"] = []string{"README.md lacks the Fixed heading"}
				}
			}
		}
		content, _ := json.Marshal(output)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(content)}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10}})
	}))
	t.Cleanup(server.Close)
	in := ProviderRegistration{Transport: "api", Provider: AIProviderInput{ID: "fixture-api", Name: "Fixture API", Kind: models.AIProviderCustomOpenAI, BaseURL: server.URL + "/v1"}, Model: "fixture-model", ContextLimit: 200000, Tier: 2, Roles: team.Roles, Capabilities: team.Capabilities, Budget: &models.ProviderBudget{LimitMicros: 5000000, InputMicrosPerMillion: 2000000, OutputMicrosPerMillion: 8000000, MaxCalls: 30, MaxOutputTokens: 2048}}
	if _, e := a.RegisterProductProvider(context.Background(), in); e != nil {
		t.Fatal(e)
	}
	return a, m, server
}
func TestSimpleProviderRegistryRequiresProbeAndExplicitRouting(t *testing.T) {
	a, input, _ := productFixture(t)
	ctx := context.Background()
	providers, e := a.ProductProviders(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if providers[0].State != "configuration_required" {
		t.Fatalf("unprobed provider marked ready: %+v", providers[0])
	}
	simple := SimpleMissionInput{Repository: input.Repository, Goal: "Update the guide", Checks: input.Checks, Trust: true, SourceExport: true, Providers: []string{"fixture-api"}}
	if _, e = a.CreateSimpleMission(ctx, simple); e == nil {
		t.Fatal("unprobed provider started")
	}
	if _, e = a.ProbeProductProvider(ctx, "fixture-api"); e != nil {
		t.Fatal(e)
	}
	providers, _ = a.ProductProviders(ctx)
	if providers[0].State != "ready" {
		t.Fatalf("probe not reflected: %+v", providers[0])
	}
	simple.Providers = []string{"missing-paid-provider"}
	if _, e = a.CreateSimpleMission(ctx, simple); e == nil {
		t.Fatal("unauthorized fallback allowed")
	}
}
func TestGuidedAPIMissionRealWorktreeFixReviewChecksAndDelivery(t *testing.T) {
	a, input, _ := productFixture(t)
	ctx := context.Background()
	if _, e := a.ProbeProductProvider(ctx, "fixture-api"); e != nil {
		t.Fatal(e)
	}
	m, e := a.CreateSimpleMission(ctx, SimpleMissionInput{Repository: input.Repository, Goal: "Update documentation heading", Checks: []string{"git diff --check"}, Trust: true, SourceExport: true, Providers: []string{"fixture-api"}})
	if e != nil {
		t.Fatal(e)
	}
	if m.Budget.MaxExecutions != 8 || m.Budget.MaxFixes != 1 {
		t.Fatalf("guided mission exceeds the bounded execution default: %+v", m.Budget)
	}
	for n := 0; n < 8 && m.State != "completed" && m.State != "escalated"; n++ {
		m, e = a.AdvanceMission(ctx, m.ID)
		if e != nil {
			t.Fatal(e)
		}
	}
	if m.State != "completed" || len(m.Executions) != 7 || m.FixCycles != 1 {
		for _, e := range m.Executions {
			if e.Agent == "review" {
				response, err := a.AgentSessionResponse(ctx, e.SessionID)
				t.Logf("REVIEW response %q err %v", response, err)
			}
		}
		t.Fatalf("API fixture mission failed: state=%s plan=%+v runs=%+v", m.State, m.Plan, m.Executions)
	}
	d, e := a.Delivery(ctx, m.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !d.Report.Candidate.Readiness.Ready || !d.Report.EvidenceComplete || len(d.Completed) != 4 || len(d.Incomplete) != 0 || len(d.Risks) == 0 {
		t.Fatalf("delivery hides incomplete/failed audit: %+v", d)
	}
	seen := map[string]bool{}
	for _, run := range m.Executions {
		if run.SessionID == "" || seen[run.Worktree] {
			t.Fatal("API session/worktree audit missing")
		}
		seen[run.Worktree] = true
		if run.Result == "completed" {
			if _, e = a.AgentSessionResponse(ctx, run.SessionID); e != nil {
				t.Fatal(e)
			}
		}
	}
	b, e := a.ProviderBudget(ctx, "fixture-api")
	if e != nil || b.Calls != 8 || b.ChargedMicros <= 0 {
		t.Fatalf("actual fixture requests not accounted: %+v %v", b, e)
	}
}
func TestProviderDiscoveryCountsCallsAndRevocationDoesNotResetLedger(t *testing.T) {
	a, _, _ := productFixture(t)
	ctx := context.Background()
	models, e := a.AI.DiscoverModels(ctx, "fixture-api")
	if e != nil || len(models) != 1 {
		t.Fatal(models, e)
	}
	b, _ := a.ProviderBudget(ctx, "fixture-api")
	if b.Calls != 1 {
		t.Fatal("discovery was not counted")
	}
	if e = a.RevokeProductProvider(ctx, "fixture-api"); e != nil {
		t.Fatal(e)
	}
	after, e := a.ProviderBudget(ctx, "fixture-api")
	if e != nil || after.Calls != 1 {
		t.Fatalf("revoke erased charges: %+v %v", after, e)
	}
}

func TestUnknownUsageAcknowledgementKeepsChargesAndRequiresNewProbe(t *testing.T) {
	a, _, _ := productFixture(t)
	ctx := context.Background()
	if _, err := a.ProbeProductProvider(ctx, "fixture-api"); err != nil {
		t.Fatal(err)
	}
	reservation, err := a.ReserveProvider(ctx, "fixture-api", 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.SettleProvider(ctx, reservation, 0, 0, false); err == nil {
		t.Fatal("unknown usage not blocked")
	}
	before, _ := a.ProviderBudget(ctx, "fixture-api")
	if err = a.ResumeProviderAfterUncertainUsage(ctx, "fixture-api"); err != nil {
		t.Fatal(err)
	}
	after, _ := a.ProviderBudget(ctx, "fixture-api")
	if after.Calls != before.Calls || after.ChargedMicros != before.ChargedMicros || after.Blocked {
		t.Fatal("ack reset budget or remained blocked")
	}
	providers, _ := a.ProductProviders(ctx)
	if providers[0].State != "configuration_required" {
		t.Fatal("acknowledgement falsely restored model readiness")
	}
}
