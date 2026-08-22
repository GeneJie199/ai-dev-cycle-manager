package agent

import (
	"reflect"
	"strings"
	"testing"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
)

func TestBuiltInAdaptersCoverSupportedCLIs(t *testing.T) {
	adapters := BuiltInAdapters()
	want := []string{"codex", "claude", "gemini", "kimi", "opencode"}
	got := make([]string, len(adapters))
	for index, adapter := range adapters {
		got[index] = adapter.ID
		if !adapter.BuiltIn || !adapter.Enabled {
			t.Fatalf("adapter=%+v", adapter)
		}
		_, args, err := BuildCommand(adapter, "do the work")
		if err != nil || !contains(args, "do the work") {
			t.Fatalf("adapter=%s args=%v err=%v", adapter.ID, args, err)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("adapters=%v want=%v", got, want)
	}
	adapters[0].Capabilities[0] = "changed"
	if adapters[1].Capabilities[0] == "changed" {
		t.Fatal("built-in adapter capability slices share mutable storage")
	}
}

func TestCustomAdapterUsesArgvAndRequiresOnePromptPlaceholder(t *testing.T) {
	config := models.AgentAdapterConfig{ID: "team-agent", Name: "Team Agent", Command: "team-agent", Args: []string{"run", "--instruction", PromptPlaceholder}, Capabilities: []string{"code_editing"}, Enabled: true}
	normalized, err := NormalizeAdapterConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	command, args, err := BuildCommand(normalized, "fix it; Remove-Item important")
	if err != nil || command != "team-agent" || len(args) != 3 || args[2] != "fix it; Remove-Item important" {
		t.Fatalf("command=%q args=%v err=%v", command, args, err)
	}
	for _, invalid := range []models.AgentAdapterConfig{
		{ID: "codex", Name: "Override", Command: "tool", Args: []string{PromptPlaceholder}},
		{ID: "team-agent", Name: "Team Agent", Command: "tool", Args: []string{"run"}},
		{ID: "team-agent", Name: "Team Agent", Command: "tool", Args: []string{PromptPlaceholder, PromptPlaceholder}},
		{ID: "team-agent", Name: "Team Agent", Command: "tool\nother", Args: []string{PromptPlaceholder}},
	} {
		if _, err = NormalizeAdapterConfig(invalid); err == nil {
			t.Fatalf("expected invalid config: %+v", invalid)
		}
	}
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if strings.Contains(item, value) {
			return true
		}
	}
	return false
}
