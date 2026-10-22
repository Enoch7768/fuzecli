package app

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/agent"
)

func TestIsFuzeAIModel(t *testing.T) {
	tests := []struct {
		provider string
		model string
		want bool
	}{
		{"llamacpp", "r7194714R/fuzeai-qwen-7b-gguf:Q4_K_M", true},
		{"LLAMACPP", "r7194714R/fuzeai-qwen-7b-gguf:Q4_K_M", true},
		{"gemini", "r7194714R/fuzeai-qwen-7b-gguf:Q4_K_M", false},
		{"llamacpp", "some-other-model", false},
	}
	for _, test := range tests {
		if got := IsFuzeAIModel(test.provider, test.model); got != test.want {
			t.Errorf("IsFuzeAIModel(%q, %q) = %v, want %v", test.provider, test.model, got, test.want)
		}
	}
}

func TestTerminalAgentAuthorizerRequiresFuzeCLIDeletionConfirmation(t *testing.T) {
	definition := agent.ToolDefinition{Name: "workspace.apply_edits"}
	raw := json.RawMessage(`{"files":[{"path":"dist/","action":"delete"}],"verify":false}`)

	deniedScanner := bufio.NewScanner(strings.NewReader("n\n"))
	if err := TerminalAgentAuthorizer(deniedScanner, false)(context.Background(), definition, raw); err == nil {
		t.Fatal("expected declined deletion to be blocked")
	}

	approvedScanner := bufio.NewScanner(strings.NewReader("yes\n"))
	if err := TerminalAgentAuthorizer(approvedScanner, false)(context.Background(), definition, raw); err != nil {
		t.Fatalf("explicitly approved delete-only request was blocked: %v", err)
	}

	if err := TerminalAgentAuthorizer(bufio.NewScanner(strings.NewReader("yes\n")), true)(context.Background(), definition, raw); err == nil {
		t.Fatal("safe mode must block deletion even when user types yes")
	}
}
