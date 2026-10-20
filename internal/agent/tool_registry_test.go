package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func testToolDefinition(name string) ToolDefinition {
	return ToolDefinition{
		Name:        name,
		Description: "Read a file from the workspace.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`),
	}
}

func TestToolRegistryRegisterAndListSorted(t *testing.T) {
	registry := NewToolRegistry()
	handler := func(context.Context, json.RawMessage) (any, error) {
		return map[string]string{"ok": "yes"}, nil
	}
	if err := registry.Register(testToolDefinition("workspace.read_file"), handler); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(testToolDefinition("workspace.list_files"), handler); err != nil {
		t.Fatal(err)
	}
	definitions := registry.Definitions()
	if len(definitions) != 2 || definitions[0].Name != "workspace.list_files" {
		t.Fatalf("expected sorted definitions, got %#v", definitions)
	}
}

func TestToolRegistryRejectsInvalidOrDuplicateRegistration(t *testing.T) {
	registry := NewToolRegistry()
	handler := func(context.Context, json.RawMessage) (any, error) { return nil, nil }

	if err := registry.Register(testToolDefinition("read-file"), handler); err == nil {
		t.Fatal("expected invalid name to be rejected")
	}
	if err := registry.Register(testToolDefinition("workspace.read_file"), nil); err == nil {
		t.Fatal("expected nil handler to be rejected")
	}
	if err := registry.Register(testToolDefinition("workspace.read_file"), handler); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(testToolDefinition("workspace.read_file"), handler); err == nil {
		t.Fatal("expected duplicate registration to be rejected")
	}
}

func TestToolRegistryExecute(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(testToolDefinition("workspace.read_file"), func(ctx context.Context, args json.RawMessage) (any, error) {
		var input struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, err
		}
		return map[string]string{"path": input.Path}, nil
	}); err != nil {
		t.Fatal(err)
	}

	call := ToolCall{
		ID:        "call_1",
		Name:      "workspace.read_file",
		Arguments: json.RawMessage(`{"path":"main.go"}`),
	}
	result := registry.Execute(context.Background(), call, nil)
	if err := ValidateToolResult(result); err != nil {
		t.Fatalf("result failed validation: %v", err)
	}
	if result.Status != ToolResultSuccess {
		t.Fatalf("expected success, got %#v", result)
	}
}

func TestToolRegistryExecuteDeniesUnauthorizedCalls(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(testToolDefinition("workspace.read_file"), func(context.Context, json.RawMessage) (any, error) {
		t.Fatal("handler must not run when authorization fails")
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}

	result := registry.Execute(context.Background(), ToolCall{
		ID: "call_1", Name: "workspace.read_file", Arguments: json.RawMessage(`{"path":"main.go"}`),
	}, func(context.Context, ToolDefinition, json.RawMessage) error {
		return errors.New("permission required")
	})
	if result.Status != ToolResultError || result.Error == "" {
		t.Fatalf("expected structured authorization failure, got %#v", result)
	}
}

func TestToolRegistryRejectsUnknownToolAndInvalidArguments(t *testing.T) {
	registry := NewToolRegistry()
	unknown := registry.Execute(context.Background(), ToolCall{
		ID: "call_1", Name: "workspace.missing", Arguments: json.RawMessage(`{}`),
	}, nil)
	if unknown.Status != ToolResultError {
		t.Fatalf("expected unknown tool error, got %#v", unknown)
	}

	invalid := registry.Execute(context.Background(), ToolCall{
		ID: "call_2", Name: "workspace.read_file", Arguments: json.RawMessage(`[]`),
	}, nil)
	if invalid.Status != ToolResultError {
		t.Fatalf("expected invalid arguments error, got %#v", invalid)
	}
}
