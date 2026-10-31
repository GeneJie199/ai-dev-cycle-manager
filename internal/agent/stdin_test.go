package agent

import (
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/models"
	"strings"
	"testing"
)

func TestStdinPromptKeepsContextOutOfArgv(t *testing.T) {
	config := models.AgentAdapterConfig{ID: "stdin-agent", Name: "Stdin agent", Command: "agent", Args: []string{"exec", StdinPromptPlaceholder}, Enabled: true}
	prompt := strings.Repeat("large context ", 10000)
	_, args, err := BuildCommand(config, prompt)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[1] != "-" {
		t.Fatal("stdin context leaked into argv")
	}
	config.Args = append(config.Args, PromptPlaceholder)
	if _, err = NormalizeAdapterConfig(config); err == nil {
		t.Fatal("ambiguous prompt transports accepted")
	}
}
