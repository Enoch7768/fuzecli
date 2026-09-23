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
		"type": "object",
		"properties": map[string]any{
			"type":        map[string]any{"type": "string", "enum": []string{"chat", "edit"}},
			"response":    map[string]any{"type": "string", "description": "Natural-language response for compatibility with the FuzeCLI chat envelope."},
			"message":     map[string]any{"type": "string", "description": "Natural-language response when no file changes are required."},
			"files":       map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}, "action": map[string]any{"type": "string", "enum": []string{"create", "modify", "delete"}}, "line_start": map[string]any{"type": "integer"}, "line_end": map[string]any{"type": "integer"}}, "required": []string{"path", "content"}}},
			"explanation": map[string]any{"type": "string"},
			"commands":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required":         []string{"type", "response", "message", "files", "explanation", "commands"},
		"propertyOrdering": []string{"type", "response", "message", "files", "explanation", "commands"},
	}
}

// ParseChatResponse is the public parser used by the streaming terminal. It
// stays local for incomplete streamed JSON, but can invoke the dedicated
// Gemini normalizer once a complete JSON candidate is present and malformed.
// This lets the existing streaming path benefit from the fixer without making
// a network request for every partial chunk.
func ParseChatResponse(raw string) (ChatResponse, error) {
	clean, err := normalizeJSONDocument(raw)
	if err == nil {
		return parseChatResponseDocument(clean)
	}
	if !completeJSONCandidate(raw) {
		return ChatResponse{}, fmt.Errorf("invalid chat JSON: %w", err)
	}
	return normalizeWithConfiguredProvider(context.Background(), raw, fmt.Errorf("invalid chat JSON: %w", err))
}

func parseChatResponseDocument(clean []byte) (ChatResponse, error) {
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
	const maxNormalizerInput = 900000
	if len(raw) > maxNormalizerInput {
		raw = raw[:maxNormalizerInput]
	}

	system := `You are FuzeCLI's JSON normalization engine. Your output is consumed by a strict local parser. Convert the supplied model response into exactly one valid JSON object matching the supplied schema. Preserve the user's meaning and every requested file change. Never invent files, code, commands, paths, or requirements. For ordinary conversation, use type "chat" and put the answer in both "response" and "message". For file changes, use type "edit" and include every requested file with complete content and an explicit action. Never use markdown fences. Never output commentary outside JSON. Paths must be relative to the workspace and must never contain '..'. For delete actions, content must be empty. If the source is truncated or malformed, reconstruct only what is recoverable from the supplied response; never fabricate missing source code. If the source contains a complete JSON object with syntax or schema mistakes, repair it faithfully.`
	messages := []provider.Message{
		{Role: "system", Content: system + "\n\nRequired JSON schema:\n" + schemaJSON()},
		{Role: "user", Content: "Normalize this response. Preserve it faithfully.\n\nOriginal response:\n" + raw + "\n\nLocal parser error:\n" + parseErr.Error()},
	}

	var errs []error
	candidates := normalizerCandidates(e.Registry)
	if len(candidates) == 0 {
		return ChatResponse{}, fmt.Errorf("provider returned incomplete or invalid structured JSON; the response was not applied. Raw response length: %d bytes. JSON normalizer unavailable: Groq is not configured", len(raw))
	}
	for _, candidate := range candidates {
		for attempt := 0; attempt < 3; attempt++ {
			resp, err := e.Registry.Send(ctx, candidate, messages, provider.RequestOptions{
				Model:       normalizerModel(e.Registry, candidate),
				Temperature: 0,
				MaxTokens:   12000,
				JSONMode:    true,
				JSONSchema:  ChatResponseSchema(),
			})
			if err != nil {
				errs = append(errs, fmt.Errorf("%s normalization attempt %d: %w", candidate, attempt+1, err))
				break
			}
			clean, cleanErr := normalizeJSONDocument(resp.Content)
			if cleanErr != nil {
				errs = append(errs, fmt.Errorf("%s normalization attempt %d returned invalid JSON: %w", candidate, attempt+1, cleanErr))
				messages = append(messages, provider.Message{Role: "user", Content: "Your previous normalization was invalid. Return ONLY one complete JSON object matching the schema. Do not truncate, fence, explain, or add extra keys. Parser error: " + cleanErr.Error()})
				continue
			}
			normalized, parseNormalizedErr := parseChatResponseDocument(clean)
			if parseNormalizedErr == nil {
				return normalized, nil
			}
			errs = append(errs, fmt.Errorf("%s normalization attempt %d returned invalid JSON schema: %w", candidate, attempt+1, parseNormalizedErr))
			messages = append(messages, provider.Message{Role: "user", Content: "Your previous normalization was structurally invalid. Return ONLY one complete JSON object matching the schema. Fix this parser error: " + parseNormalizedErr.Error()})
		}
	}

	return ChatResponse{}, fmt.Errorf("provider returned incomplete or invalid structured JSON; the response was not applied. Raw response length: %d bytes. JSON normalizer failures: %s", len(raw), normalizeFailureSummary(errs))
}

func normalizerCandidates(registry *provider.Registry) []string {
	if registry == nil {
		return nil
	}
	if _, err := registry.Get("groq"); err != nil {
		return nil
	}
	return []string{"groq"}
}

func normalizerModel(registry *provider.Registry, providerName string) string {
	if registry == nil {
		return ""
	}
	if providerName == "groq" {
		return registry.DefaultModel("groq")
	}
	return ""
}

func schemaJSON() string {
	data, _ := json.Marshal(ChatResponseSchema())
	return string(data)
}
