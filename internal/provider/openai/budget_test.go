package openai

import (
	"testing"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

func TestEffectiveOptionsCapsCompletionToRequestBudget(t *testing.T) {
	messages := []provider.Message{
		{Role: "system", Content: "You are a coding assistant."},
		{Role: "user", Content: "Please inspect and explain this repository."},
	}
	opts := effectiveOptions(messages, provider.RequestOptions{
		Model:             "test-model",
		MaxTokens:         16000,
		RequestTokenLimit: 7600,
	})
	if opts.MaxTokens >= 16000 {
		t.Fatalf("expected max tokens to be reduced, got %d", opts.MaxTokens)
	}
	if opts.MaxTokens <= 0 {
		t.Fatalf("expected a positive completion budget, got %d", opts.MaxTokens)
	}
}

func TestEffectiveOptionsLeavesUnlimitedRequestsUntouched(t *testing.T) {
	opts := provider.RequestOptions{Model: "test-model", MaxTokens: 4096}
	got := effectiveOptions([]provider.Message{{Role: "user", Content: "hello"}}, opts)
	if got.MaxTokens != opts.MaxTokens {
		t.Fatalf("expected unlimited request to remain unchanged, got %d", got.MaxTokens)
	}
}
