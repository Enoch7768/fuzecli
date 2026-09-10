package gemini

import (
	"testing"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

func TestConvertKeepsUserContent(t *testing.T) {
	contents, system := convert([]provider.Message{
		{Role: "system", Content: "system instructions"},
		{Role: "user", Content: "hello"},
	})
	if system == nil {
		t.Fatal("expected system instruction")
	}
	if len(contents) != 1 {
		t.Fatalf("expected one content entry, got %d", len(contents))
	}
	if contents[0].Role != "user" || len(contents[0].Parts) != 1 || contents[0].Parts[0].Text != "hello" {
		t.Fatal("user content was not converted correctly")
	}
}

func TestConvertNeverReturnsEmptyContents(t *testing.T) {
	contents, _ := convert([]provider.Message{
		{Role: "system", Content: "system instructions"},
		{Role: "user", Content: ""},
	})
	if len(contents) == 0 {
		t.Fatal("expected a non-empty Gemini contents payload")
	}
	if len(contents[0].Parts) == 0 || contents[0].Parts[0].Text == "" {
		t.Fatal("expected non-empty fallback user content")
	}
}
