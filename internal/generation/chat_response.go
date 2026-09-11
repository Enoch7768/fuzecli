package generation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
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

func ParseChatPlan(raw string) (Plan, error) {
	clean, err := normalizeJSONDocument(raw)
	if err != nil {
		return Plan{}, fmt.Errorf("invalid chat JSON: %w", err)
	}
	var input chatPlan
	dec := json.NewDecoder(bytes.NewReader(clean))
	if err := dec.Decode(&input); err != nil {
		return Plan{}, fmt.Errorf("invalid chat JSON: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return Plan{}, fmt.Errorf("invalid chat JSON: trailing data")
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
		resolved.Files = append(resolved.Files, FileChange{Path: file.Path, Content: file.Content, Action: action})
	}
	return Apply(root, resolved)
}
