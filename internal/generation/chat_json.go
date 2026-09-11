package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/provider"
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
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"type":        map[string]any{"type": "string", "enum": []string{"chat", "edit"}},
			"response":    map[string]any{"type": "string", "description": "Natural-language response for compatibility with the FuzeCLI chat envelope."},
			"message":     map[string]any{"type": "string", "description": "Natural-language response when no file changes are required."},
			"files":       map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}, "action": map[string]any{"type": "string", "enum": []string{"create", "modify", "delete"}}, "line_start": map[string]any{"type": "integer"}, "line_end": map[string]any{"type": "integer"}}, "required": []string{"path", "content"}}},
			"explanation": map[string]any{"type": "string"},
			"commands":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"propertyOrdering": []string{"type", "response", "message", "files", "explanation", "commands"},
	}
}

func ParseChatResponse(raw string) (ChatResponse, error) {
	clean, err := normalizeJSONDocument(raw)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("invalid chat JSON: %w", err)
	}
	var response ChatResponse
	dec := json.NewDecoder(bytes.NewReader(clean))
	dec.DisallowUnknownFields()
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
	if response.Type == "chat" && len(response.Files) > 0 {
		return ChatResponse{}, errors.New("chat JSON cannot contain file changes; use type edit")
	}
	if response.Type == "chat" && message == "" && strings.TrimSpace(response.Explanation) == "" {
		return ChatResponse{}, errors.New("chat JSON contains neither a message nor an explanation")
	}
	if response.Type == "chat" {
		if response.Response == "" {
			response.Response = response.Message
		}
		if response.Message == "" {
			response.Message = response.Response
		}
		return response, nil
	}
	if len(response.Files) == 0 {
		return ChatResponse{}, errors.New("edit JSON contains no file changes")
	}
	plan, err := response.ToPlan()
	if err != nil {
		return ChatResponse{}, err
	}
	response.Plan = &plan
	return response, nil
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
	response, err := ParseChatResponse(raw)
	if err == nil {
		return response, nil
	}
	if e == nil || e.Registry == nil {
		return ChatResponse{}, err
	}
	return e.normalizeChatResponse(ctx, raw, err)
}

func (e *Engine) ParseChatPlan(ctx context.Context, raw string) (Plan, error) {
	response, err := e.ParseChatResponse(ctx, raw)
	if err != nil {
		return Plan{}, err
	}
	return response.ToPlan()
}

func (e *Engine) normalizeChatResponse(ctx context.Context, raw string, parseErr error) (ChatResponse, error) {
	const maxNormalizerInput = 120000
	if len(raw) > maxNormalizerInput {
		raw = raw[:maxNormalizerInput]
	}
	messages := []provider.Message{{Role: "system", Content: `You are FuzeCLI's JSON normalization engine. Convert the supplied model response into the exact FuzeCLI chat response schema. Preserve the user's requested intent and every file change exactly. Do not invent files or commands. If the source is ordinary text, put it in message. If it contains file changes under another JSON shape, map them into files. Paths must be relative to the workspace and must not escape it. Return ONLY valid JSON matching the required schema.`}, {Role: "user", Content: "Required schema:\n" + schemaJSON() + "\n\nOriginal response:\n" + raw + "\n\nLocal parser error:\n" + parseErr.Error()}}
	resp, err := e.Registry.Send(ctx, JSONNormalizerProvider, messages, provider.RequestOptions{Temperature: 0, MaxTokens: 12000, JSONMode: true, JSONSchema: ChatResponseSchema()})
	if err != nil {
		return ChatResponse{}, fmt.Errorf("chat JSON normalization failed: %w; original parse error: %v", err, parseErr)
	}
	normalized, err := ParseChatResponse(resp.Content)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("normalized chat JSON is invalid: %w", err)
	}
	return normalized, nil
}

func schemaJSON() string { data, _ := json.Marshal(ChatResponseSchema()); return string(data) }
