package api

import (
	"testing"\n\t"strings"

	"github.com/Enoch7768/fuzecli/internal/generation"
)

func TestChatRequestOptionsUsePlainTextByDefault(t *testing.T) {
	options := chatRequestOptions("test-model", "standard", "request-1", false)
	if options.JSONMode {
		t.Fatal("ordinary chat must not force JSON mode")
	}
	if options.JSONSchema != nil {
		t.Fatal("ordinary chat must not attach a JSON schema")
	}
	if options.Model != "test-model" || options.BillingMode != "standard" || options.RequestID != "request-1" {
		t.Fatalf("request metadata was not preserved: %#v", options)
	}
}

func TestChatRequestOptionsEnableStructuredOutputForEdits(t *testing.T) {
	options := chatRequestOptions("test-model", "standard", "request-2", true)
	if !options.JSONMode {
		t.Fatal("workspace-edit requests must enable JSON mode")
	}
	if options.JSONSchema == nil {
		t.Fatal("workspace-edit requests must include the response schema")
	}
	if options.JSONSchema["type"] != "object" {
		t.Fatalf("unexpected response schema: %#v", options.JSONSchema)
	}
}

func TestChatSystemPromptSeparatesConversationFromEdits(t *testing.T) {
	plain := chatSystemPrompt(false)
	if !containsAll(plain, "ordinary text", "do not wrap the answer in JSON", "Do not modify files") {
		t.Fatalf("plain-chat prompt does not define the plain-text contract: %s", plain)
	}
	structured := chatSystemPrompt(true)
	if !containsAll(structured, "STRUCTURED RESPONSE CONTRACT", "JSON object only", "complete content") {
		t.Fatalf("edit prompt does not define the structured contract: %s", structured)
	}
	if structured == plain {
		t.Fatal("chat and edit prompts must have different response contracts")
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}

func TestChatResponseSchemaRemainsAvailableForStructuredEdits(t *testing.T) {
	schema := generation.ChatResponseSchema()
	if schema["type"] != "object" {
		t.Fatalf("unexpected schema type: %#v", schema["type"])
	}
}
