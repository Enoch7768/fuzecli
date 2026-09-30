package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type chatFileChange struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Action    string `json:"action"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
}

type ChatResponse struct {
	Type        string           `json:"type,omitempty"`
	Response    string           `json:"response,omitempty"`
	Message     string           `json:"message,omitempty"`
	Files       []chatFileChange `json:"files,omitempty"`
	Plan        *Plan            `json:"-"`
	Explanation string           `json:"explanation,omitempty"`
	Commands    []string         `json:"commands,omitempty"`
}

func ChatResponseSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"additionalProperties": true,
		"properties": map[string]any{
			"protocol_version": map[string]any{"type": "string", "const": ResponseProtocolVersion},
			"type":        map[string]any{"type": "string", "enum": []string{"chat", "edit", "command", "analysis", "error", "progress"}},
			"response":    map[string]any{"type": "string", "description": "Natural-language response for compatibility with the FuzeCLI chat envelope."},
			"message":     map[string]any{"type": "string", "description": "Natural-language response shown before or alongside file changes."},
			"files": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"path":       map[string]any{"type": "string"},
						"content":    map[string]any{"type": "string"},
						"action":     map[string]any{"type": "string", "enum": []string{"create", "modify", "delete"}},
						"line_start": map[string]any{"type": "integer"},
						"line_end":   map[string]any{"type": "integer"},
					},
					"required": []string{},
				},
			},
			"explanation": map[string]any{"type": "string"},
			"commands":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{},
	}
}

func ParseChatResponse(raw string) (ChatResponse, error) {
	clean, err := normalizeJSONDocument(raw)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("invalid chat JSON: %w", err)
	}
	return parseChatResponseDocument(normalizeChatResponseEnvelope(clean))
}

func normalizeChatResponseEnvelope(clean []byte) []byte {
	var value any
	if json.Unmarshal(clean, &value) != nil {
		return clean
	}
	if array, ok := value.([]any); ok {
		value = map[string]any{"type": "edit", "files": array}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return clean
	}
	for _, nested := range []string{"data", "result", "output"} {
		if child, ok := object[nested].(map[string]any); ok {
			if _, hasFiles := object["files"]; !hasFiles {
				if _, childFiles := child["files"]; childFiles {
					object = child
					break
				}
			}
		}
	}
	copyString := func(target string, aliases ...string) {
		if _, ok := object[target]; ok {
			return
		}
		for _, alias := range aliases {
			if v, ok := object[alias].(string); ok {
				object[target] = v
				return
			}
		}
	}
	copyString("response", "answer", "content", "text", "output")
	copyString("message", "answer", "content", "text")
	copyString("explanation", "summary", "description")
	if _, ok := object["files"]; !ok {
		for _, alias := range []string{"changes", "edits", "patches"} {
			if v, ok := object[alias]; ok {
				object["files"] = v
				break
			}
		}
	}
	if _, ok := object["commands"]; !ok {
		if command, ok := object["command"].(string); ok {
			object["commands"] = []any{command}
		}
	}
	if _, ok := object["type"]; !ok {
		if _, ok := object["files"]; ok {
			object["type"] = "edit"
		} else {
			object["type"] = "chat"
		}
	} else if mode, ok := object["type"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "edit", "edits", "change", "changes", "patch", "patches", "code":
			object["type"] = "edit"
		default:
			object["type"] = "chat"
		}
	}
	if files, ok := object["files"].([]any); ok {
		for i, rawFile := range files {
			file, ok := rawFile.(map[string]any)
			if !ok {
				continue
			}
			copyFile := func(target string, aliases ...string) {
				if _, exists := file[target]; exists {
					return
				}
				for _, alias := range aliases {
					if v, exists := file[alias]; exists {
						file[target] = v
						return
					}
				}
			}
			copyFile("path", "file", "filename", "filepath", "name")
			copyFile("content", "code", "source", "text", "body")
			copyFile("action", "operation", "op", "change")
			copyFile("line_start", "start_line", "from_line")
			copyFile("line_end", "end_line", "to_line")
			if _, exists := file["action"]; !exists {
				file["action"] = "modify"
			}
			if _, exists := file["content"]; !exists {
				file["content"] = ""
			}
			files[i] = file
		}
		object["files"] = files
	} else if fileMap, ok := object["files"].(map[string]any); ok {
		files := make([]any, 0, len(fileMap))
		for path, content := range fileMap {
			files = append(files, map[string]any{"path": path, "content": content, "action": "modify"})
		}
		object["files"] = files
	}
	out, err := json.Marshal(object)
	if err != nil {
		return clean
	}
	return out
}

func parseChatResponseDocument(clean []byte) (ChatResponse, error) {
	var response ChatResponse
	dec := json.NewDecoder(bytes.NewReader(clean))
	if err := dec.Decode(&response); err != nil {
		return ChatResponse{}, fmt.Errorf("invalid chat JSON: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return ChatResponse{}, errors.New("invalid chat JSON: trailing data")
		}
		return ChatResponse{}, fmt.Errorf("invalid chat JSON: trailing data: %w", err)
	}
	message := strings.TrimSpace(response.Message)
	if message == "" {
		message = strings.TrimSpace(response.Response)
	}
	if response.Type == "" {
		if len(response.Files) > 0 {
			response.Type = "edit"
		} else {
			response.Type = "chat"
		}
	}
	if response.Type != "chat" && response.Type != "edit" {
		return ChatResponse{}, fmt.Errorf("invalid chat JSON type %q; expected chat or edit", response.Type)
	}
		if response.Type == "chat" && message == "" && strings.TrimSpace(response.Explanation) == "" {
		return ChatResponse{}, errors.New("chat JSON contains neither a message nor an explanation")
	}
	if response.Response == "" {
		response.Response = response.Message
	}
	if response.Message == "" {
		response.Message = response.Response
	}
	if response.Type == "chat" {
		if len(response.Files) == 0 {
			return response, nil
		}
		plan, err := response.ToPlan()
		if err != nil {
			return ChatResponse{}, err
		}
		response.Plan = &plan
		return response, nil
	}
	if len(response.Files) == 0 {
		if message != "" || strings.TrimSpace(response.Explanation) != "" {
			response.Type = "chat"
			return response, nil
		}
		return ChatResponse{}, errors.New("edit JSON contains no file changes")
	}
	plan, err := response.ToPlan()
	if err != nil {
		return ChatResponse{}, err
	}
	response.Plan = &plan
	return response, nil
}

func completeJSONCandidate(raw string) bool {
	s := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	if s == "" {
		return false
	}
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return false
	}
	end, ok := balancedJSONEnd(s, start)
	return ok && strings.TrimSpace(s[end:]) == ""
}

func validateChatFile(i int, file chatFileChange) error {
	if file.Path == "" {
		return fmt.Errorf("file %d has empty path", i)
	}
	if file.LineStart < 0 || file.LineEnd < 0 || (file.LineStart > 0 && file.LineEnd > 0 && file.LineEnd < file.LineStart) {
		return fmt.Errorf("file %d has invalid line range", i)
	}
	if file.Action != "" && file.Action != "create" && file.Action != "modify" && file.Action != "delete" {
		return fmt.Errorf("file %d has invalid action %q", i, file.Action)
	}
	if file.Action == "delete" && file.Content != "" {
		return fmt.Errorf("file %d delete action must have empty content", i)
	}
	rel := strings.ReplaceAll(strings.TrimSpace(file.Path), "\\", "/")
	if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") || strings.Contains(rel, ":") {
		return fmt.Errorf("path traversal rejected for %q", file.Path)
	}
	return nil
}

func (r ChatResponse) ToPlan() (Plan, error) {
	if len(r.Files) == 0 {
		return Plan{}, errors.New("chat response contains no file changes")
	}
	plan := Plan{Explanation: r.Explanation, Commands: r.Commands, Files: make([]FileChange, 0, len(r.Files))}
	if plan.Explanation == "" {
		plan.Explanation = r.Message
		if plan.Explanation == "" {
			plan.Explanation = r.Response
		}
	}
	for i, file := range r.Files {
		if err := validateChatFile(i, file); err != nil {
			return Plan{}, err
		}
		plan.Files = append(plan.Files, FileChange{Path: file.Path, Content: file.Content, Action: file.Action})
	}
	return plan, nil
}

func (e *Engine) ParseChatResponse(ctx context.Context, raw string) (ChatResponse, error) {
	return ParseChatResponse(raw)
}

func (e *Engine) ParseChatPlan(ctx context.Context, raw string) (Plan, error) {
	response, err := e.ParseChatResponse(ctx, raw)
	if err != nil {
		return Plan{}, err
	}
	return response.ToPlan()
}
