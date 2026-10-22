package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const (
	MaxToolArgumentsBytes = 64 * 1024
	MaxToolResultBytes    = 1 * 1024 * 1024
)

var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type ToolHandler func(context.Context, json.RawMessage) (any, error)
type ToolAuthorizer func(context.Context, ToolDefinition, json.RawMessage) error

type registeredTool struct {
	definition ToolDefinition
	handler    ToolHandler
}

type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]registeredTool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]registeredTool)}
}

func (r *ToolRegistry) Register(definition ToolDefinition, handler ToolHandler) error {
	if r == nil {
		return errors.New("tool registry is nil")
	}
	if !toolNamePattern.MatchString(definition.Name) {
		return fmt.Errorf("invalid tool name %q", definition.Name)
	}
	if strings.TrimSpace(definition.Description) == "" {
		return fmt.Errorf("tool %q must have a description", definition.Name)
	}
	if len(definition.InputSchema) == 0 || !json.Valid(definition.InputSchema) {
		return fmt.Errorf("tool %q has an invalid input schema", definition.Name)
	}
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil || schema == nil {
		return fmt.Errorf("tool %q input schema must be a JSON object", definition.Name)
	}
	if handler == nil {
		return fmt.Errorf("tool %q has no handler", definition.Name)
	}

	definition.InputSchema = append(json.RawMessage(nil), definition.InputSchema...)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[definition.Name]; exists {
		return fmt.Errorf("tool %q is already registered", definition.Name)
	}
	r.tools[definition.Name] = registeredTool{definition: definition, handler: handler}
	return nil
}

func (r *ToolRegistry) Definitions() []ToolDefinition {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	definitions := make([]ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		definition := tool.definition
		definition.InputSchema = append(json.RawMessage(nil), definition.InputSchema...)
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

func (r *ToolRegistry) Execute(
	ctx context.Context,
	call ToolCall,
	authorize ToolAuthorizer,
) ToolResult {
	result := ToolResult{
		ProtocolVersion: ToolProtocolVersion,
		ToolCallID:      call.ID,
		Name:            call.Name,
		Status:          ToolResultError,
	}

	if r == nil {
		result.Error = "tool registry is unavailable"
		return result
	}
	if strings.TrimSpace(call.ID) == "" {
		result.Error = "tool call id is required"
		return result
	}
	if !toolNamePattern.MatchString(call.Name) {
		result.Error = "invalid tool name"
		return result
	}
	if len(call.Arguments) == 0 || len(call.Arguments) > MaxToolArgumentsBytes || !json.Valid(call.Arguments) {
		result.Error = "tool arguments are missing, too large, or invalid JSON"
		return result
	}
	var argsObject map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &argsObject); err != nil || argsObject == nil {
		result.Error = "tool arguments must be a JSON object"
		return result
	}
	if err := ctx.Err(); err != nil {
		result.Error = err.Error()
		return result
	}

	r.mu.RLock()
	tool, exists := r.tools[call.Name]
	r.mu.RUnlock()
	if !exists {
		result.Error = fmt.Sprintf("unknown tool %q", call.Name)
		return result
	}
	if err := ValidateToolArguments(tool.definition, call.Arguments); err != nil {
		result.Error = "invalid tool arguments: " + err.Error()
		return result
	}

	requiresApproval, approvalReason := toolRequiresApproval(call.Name, call.Arguments)
	if requiresApproval && authorize == nil {
		result.Error = "tool authorization denied: explicit approval is required for " + approvalReason
		return result
	}
	if authorize != nil {
		if err := authorize(ctx, tool.definition, call.Arguments); err != nil {
			result.Error = fmt.Sprintf("tool authorization denied: %v", err)
			return result
		}
	}
	if err := ctx.Err(); err != nil {
		result.Error = err.Error()
		return result
	}

	value, err := tool.handler(ctx, append(json.RawMessage(nil), call.Arguments...))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		result.Error = fmt.Sprintf("tool result could not be encoded: %v", err)
		return result
	}
	if len(encoded) > MaxToolResultBytes {
		result.Error = fmt.Sprintf("tool result exceeds %d bytes", MaxToolResultBytes)
		return result
	}

	result.Status = ToolResultSuccess
	result.Result = encoded
	return result
}


// toolRequiresApproval separates read-only inspection from operations that
// modify files, local Git history, or remote GitHub state.
func toolRequiresApproval(name string, raw json.RawMessage) (bool, string) {
	var args map[string]any
	_ = json.Unmarshal(raw, &args)
	action, _ := args["action"].(string)
	switch name {
	case "workspace.apply_edits":
		return true, "workspace file changes"
	case "terminal.run":
		return true, "running a terminal command"
	case "git.commit":
		return true, "local Git commits"
	case "github.branches":
		return action == "create", "remote branch creation"
	case "github.sync":
		return true, "synchronizing the local repository with GitHub"
	case "github.pull_requests":
		switch action { case "create": return true,"creating a pull request"; case "review": return true,"submitting a pull-request review"; case "comment": return true,"posting a pull-request comment"; case "merge": return true,"merging a pull request" }
	case "github.issues":
		switch action { case "create": return true,"creating an issue"; case "update": return true,"updating an issue"; case "comment": return true,"posting an issue comment" }
	case "github.actions":
		return action == "rerun_failed", "rerunning GitHub Actions jobs"
	case "github.auth":
		return action == "login_start" || action == "disconnect", "changing GitHub authentication state"
	}
	return false, ""
}

func RequiresToolApproval(name string, raw json.RawMessage) (bool, string) {
	return toolRequiresApproval(name, raw)
}
