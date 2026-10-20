package generation

import (
	"strings"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

func TestMessagesWithSystemPreservesProfileAndAppliesContextBudget(t *testing.T) {
	system := strings.Repeat("s", 24)
	profile := strings.Repeat("p", 12)
	recent := provider.Message{Role: "assistant", Content: strings.Repeat("r", 8)}
	old := provider.Message{Role: "user", Content: strings.Repeat("o", 24)}
	prompt := "latest request"
	finalSystem := system + "\nDeveloper profile:\n" + profile
	budget := len(finalSystem) + len(recent.Content) + len(prompt)

	engine := Engine{MaxContextChars: budget}
	messages := engine.MessagesWithSystem(system, profile, []provider.Message{old, recent}, prompt)

	if len(messages) != 3 {
		t.Fatalf("message count = %d, want system, recent history, and latest prompt", len(messages))
	}
	if messages[0].Role != "system" || !strings.Contains(messages[0].Content, profile) {
		t.Fatalf("final system message did not preserve profile: %#v", messages[0])
	}
	if messages[1] != recent {
		t.Fatalf("recent history was not retained: %#v", messages[1])
	}
	if messages[2].Role != "user" || messages[2].Content != prompt {
		t.Fatalf("latest prompt was not retained: %#v", messages[2])
	}
}

func TestTrimMessagesRetainsLatestPromptWhenItExceedsBudget(t *testing.T) {
	system := provider.Message{Role: "system", Content: "system"}
	old := provider.Message{Role: "user", Content: "old context"}
	prompt := provider.Message{Role: "user", Content: "a prompt larger than the remaining context budget"}

	messages := trimMessages([]provider.Message{system, old, prompt}, len(system.Content)+4)
	if len(messages) != 2 {
		t.Fatalf("message count = %d, want system and latest prompt", len(messages))
	}
	if messages[1] != prompt {
		t.Fatalf("latest prompt was dropped or reordered: %#v", messages)
	}
}

func TestTrimMessagesKeepsContiguousRecentHistory(t *testing.T) {
	system := provider.Message{Role: "system", Content: "sys"}
	old := provider.Message{Role: "user", Content: "old"}
	tooLarge := provider.Message{Role: "assistant", Content: strings.Repeat("x", 40)}
	recent := provider.Message{Role: "assistant", Content: "recent"}
	prompt := provider.Message{Role: "user", Content: "next"}
	budget := len(system.Content) + len(recent.Content) + len(prompt.Content)

	messages := trimMessages([]provider.Message{system, old, tooLarge, recent, prompt}, budget)
	if len(messages) != 3 || messages[1] != recent || messages[2] != prompt {
		t.Fatalf("expected contiguous recent history and latest prompt, got %#v", messages)
	}
}
