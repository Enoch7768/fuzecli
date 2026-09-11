package app

import (
	"strings"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

func generationTrim(messages []provider.Message, maxChars int) []provider.Message {
	if maxChars <= 0 || len(messages) == 0 {
		return messages
	}
	total := 0
	for _, message := range messages {
		total += len(message.Content)
	}
	if total <= maxChars {
		return messages
	}
	result := make([]provider.Message, 0, len(messages))
	if messages[0].Role == "system" {
		result = append(result, messages[0])
	}
	remaining := maxChars
	if len(result) > 0 {
		remaining -= len(result[0].Content)
	}
	for i := len(messages) - 1; i >= 1 && remaining > 0; i-- {
		content := messages[i].Content
		if len(content) > remaining {
			content = strings.TrimSpace(content[len(content)-remaining:])
		}
		result = append(result, provider.Message{Role: messages[i].Role, Content: content})
		remaining -= len(content)
	}
	for left, right := 1, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}
