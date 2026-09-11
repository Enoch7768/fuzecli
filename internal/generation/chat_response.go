package generation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LooksLikeChatPlanPrefix(raw string) bool {
	text := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	if text == "" {
		return false
	}
	if strings.HasPrefix(text, "{") {
		prefix := text
		if len(prefix) > 4096 {
			prefix = prefix[:4096]
		}
		return strings.Contains(prefix, "\"files\"") || strings.Contains(prefix, "\"response\"") || strings.Contains(prefix, "\"message\"") || strings.Contains(prefix, "\"explanation\"")
	}
	lower := strings.ToLower(text)
	return strings.HasPrefix(lower, "```json") || strings.HasPrefix(lower, "```\n{")
}

// ParseChatPlan is the compatibility helper used by callers that specifically
// need a file-change plan. Normal conversational JSON is rejected as a plan.
func ParseChatPlan(raw string) (Plan, error) {
	response, err := ParseChatResponse(raw)
	if err != nil {
		return Plan{}, err
	}
	if response.Plan == nil {
		return Plan{}, fmt.Errorf("chat response is conversational JSON, not a file-change plan")
	}
	return *response.Plan, nil
}

func parseChatPlanStrict(raw string) (Plan, error) {
	response, err := ParseChatResponse(raw)
	if err != nil {
		return Plan{}, err
	}
	if response.Plan == nil {
		return Plan{}, fmt.Errorf("chat response is conversational JSON, not a file-change plan")
	}
	return *response.Plan, nil
}

func ApplyChatPlan(root string, plan Plan) ([]string, error) {
	resolved := Plan{Explanation: plan.Explanation, Commands: plan.Commands, Files: make([]FileChange, 0, len(plan.Files))}
	for _, file := range plan.Files {
		action := file.Action
		path, err := Resolve(root, file.Path)
		if err != nil {
			return nil, err
		}
		if action == "" {
			_, err := os.Stat(path)
			switch {
			case err == nil:
				action = "modify"
			case os.IsNotExist(err):
				action = "create"
			default:
				return nil, fmt.Errorf("inspect %s: %w", file.Path, err)
			}
		}
		if action == "delete" && file.Content != "" {
			return nil, fmt.Errorf("delete %s must not include content", file.Path)
		}
		resolved.Files = append(resolved.Files, FileChange{Path: file.Path, Content: file.Content, Action: action})
	}
	return Apply(root, resolved)
}

// Keep this helper available for legacy normalization callers that need a
// provider-backed recovery after the local parser rejects a response.
func normalizeWithConfiguredProvider(ctx context.Context, raw string, parseErr error) (Plan, error) {
	return Plan{}, fmt.Errorf("provider-backed chat normalization is available through Engine.ParseChatPlan: %w", parseErr)
}

var _ = context.Background
var _ = filepath.Separator
