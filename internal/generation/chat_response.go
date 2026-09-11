package generation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type chatFileChange struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Action    string `json:"action"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
}

type chatPlan struct {
	Files       []chatFileChange `json:"files"`
	Explanation string           `json:"explanation"`
	Commands    []string         `json:"commands"`
}

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
		return strings.Contains(prefix, "\"files\"") || strings.Contains(prefix, "\"explanation\"") || strings.HasPrefix(prefix, "{\"files\"")
	}
	lower := strings.ToLower(text)
	return strings.HasPrefix(lower, "```json") || strings.HasPrefix(lower, "```\n{")
}

func ParseChatPlan(raw string) (Plan, error) {
	clean, err := normalizeJSONDocument(raw)
	if err != nil {
		return Plan{}, fmt.Errorf("invalid chat JSON: %w", err)
	}
	var input chatPlan
	dec := json.NewDecoder(bytes.NewReader(clean))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		return Plan{}, fmt.Errorf("invalid chat JSON: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Plan{}, fmt.Errorf("invalid chat JSON: trailing data")
		}
		return Plan{}, fmt.Errorf("invalid chat JSON: trailing data: %w", err)
	}
	if len(input.Files) == 0 {
		return Plan{}, fmt.Errorf("chat JSON contains no files")
	}
	plan := Plan{Explanation: input.Explanation, Commands: input.Commands, Files: make([]FileChange, 0, len(input.Files))}
	for i, file := range input.Files {
		if file.Path == "" {
			return Plan{}, fmt.Errorf("file %d has empty path", i)
		}
		if file.LineStart < 0 || file.LineEnd < 0 || (file.LineStart > 0 && file.LineEnd > 0 && file.LineEnd < file.LineStart) {
			return Plan{}, fmt.Errorf("file %d has invalid line range", i)
		}
		if file.Action != "" && file.Action != "create" && file.Action != "modify" && file.Action != "delete" {
			return Plan{}, fmt.Errorf("file %d has invalid action %q", i, file.Action)
		}
		if file.Action == "delete" && file.Content != "" {
			return Plan{}, fmt.Errorf("file %d delete action must have empty content", i)
		}
		rel := filepath.ToSlash(file.Path)
		if filepath.IsAbs(file.Path) || filepath.VolumeName(file.Path) != "" || rel == "." || rel == ".." || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
			return Plan{}, fmt.Errorf("path traversal rejected for %q", file.Path)
		}
		plan.Files = append(plan.Files, FileChange{Path: file.Path, Content: file.Content, Action: file.Action})
	}
	return plan, nil
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
