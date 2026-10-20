package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

type scriptedSender struct {
	responses []string
	calls     int
	err       error
}

func (s *scriptedSender) Send(_ context.Context, _ string, messages []provider.Message, _ provider.RequestOptions) (*provider.Response, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if len(s.responses) == 0 {
		return nil, errors.New("no scripted response")
	}
	content := s.responses[0]
	s.responses = s.responses[1:]
	if !strings.Contains(messages[0].Content, ToolProtocolVersion) {
		return nil, errors.New("tool protocol instructions missing")
	}
	return &provider.Response{Content: content}, nil
}

func TestRunnerExecutesToolThenReturnsFinal(t *testing.T) {
	sender := &scriptedSender{responses: []string{
		`{"protocol_version":"fuze-agent-v1","type":"tool_call","tool_calls":[{"id":"call_1","name":"workspace.read_file","arguments":{"path":"README.md"}}]}`,
		`{"protocol_version":"fuze-agent-v1","type":"final","response":"I checked README.md."}`,
	}}
	registry := NewToolRegistry()
	if err := registry.Register(testToolDefinition("workspace.read_file"), func(_ context.Context, args json.RawMessage) (any, error) {
		var input struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, err
		}
		return map[string]string{"path": input.Path, "content": "readme contents"}, nil
	}); err != nil {
		t.Fatal(err)
	}

	runner := Runner{Sender: sender, Tools: registry, MaxSteps: 4}
	result, err := runner.Run(context.Background(), "test-provider", []provider.Message{{Role: "user", Content: "inspect README"}}, provider.RequestOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Response != "I checked README.md." || result.Steps != 2 || result.ToolCalls != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunnerRejectsMalformedModelResponse(t *testing.T) {
	sender := &scriptedSender{responses: []string{"not json"}}
	registry := NewToolRegistry()
	if err := registry.Register(testToolDefinition("workspace.read_file"), func(context.Context, json.RawMessage) (any, error) {
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Sender: sender, Tools: registry}
	_, err := runner.Run(context.Background(), "test-provider", []provider.Message{{Role: "user", Content: "hello"}}, provider.RequestOptions{}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid fuze-agent-v1 JSON") {
		t.Fatalf("expected malformed response error, got %v", err)
	}
}

func TestRunnerStopsAfterStepLimit(t *testing.T) {
	sender := &scriptedSender{responses: []string{
		`{"protocol_version":"fuze-agent-v1","type":"tool_call","tool_calls":[{"id":"call_1","name":"workspace.read_file","arguments":{"path":"a"}}]}`,
	}}
	registry := NewToolRegistry()
	if err := registry.Register(testToolDefinition("workspace.read_file"), func(context.Context, json.RawMessage) (any, error) {
		return map[string]bool{"ok": true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Sender: sender, Tools: registry, MaxSteps: 1}
	_, err := runner.Run(context.Background(), "test-provider", []provider.Message{{Role: "user", Content: "read a"}}, provider.RequestOptions{}, nil)
	if err == nil || !strings.Contains(err.Error(), "step limit reached") {
		t.Fatalf("expected step limit error, got %v", err)
	}
}

func TestRunnerForcesStructuredJSONAndRemovesOtherSchema(t *testing.T) {
	sender := &optionsCheckingSender{}
	registry := NewToolRegistry()
	if err := registry.Register(testToolDefinition("workspace.read_file"), func(context.Context, json.RawMessage) (any, error) {
		return map[string]bool{"ok": true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Sender: sender, Tools: registry}
	_, err := runner.Run(context.Background(), "test-provider", []provider.Message{{Role: "user", Content: "hello"}}, provider.RequestOptions{JSONSchema: map[string]any{"type": "object"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

type optionsCheckingSender struct{}

func (*optionsCheckingSender) Send(_ context.Context, _ string, messages []provider.Message, options provider.RequestOptions) (*provider.Response, error) {
	if !options.JSONMode || options.JSONSchema != nil || options.JSONSchemaStrict {
		return nil, errors.New("runner did not normalize JSON options")
	}
	return &provider.Response{Content: `{"protocol_version":"fuze-agent-v1","type":"final","response":"done"}`}, nil
}
