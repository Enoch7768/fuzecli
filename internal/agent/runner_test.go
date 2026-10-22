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
	seen      [][]provider.Message
}

func (s *scriptedSender) Send(_ context.Context, _ string, messages []provider.Message, _ provider.RequestOptions) (*provider.Response, error) {
	s.calls++
	copied := append([]provider.Message(nil), messages...)
	s.seen = append(s.seen, copied)
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
	if len(sender.seen) != 2 { t.Fatalf("model was called %d times, want 2", len(sender.seen)) }
	last := sender.seen[1][len(sender.seen[1])-1]
	if last.Role != "user" { t.Fatalf("tool results should use provider-compatible user messages, got role %q", last.Role) }
	var envelope struct {
		ProtocolVersion string `json:"protocol_version"`
		Type string `json:"type"`
		Results []struct {
			ID string `json:"id"`
			Name string `json:"name"`
			Status string `json:"status"`
			Result map[string]string `json:"result"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(last.Content), &envelope); err != nil { t.Fatalf("decode tool-result envelope: %v", err) }
	if envelope.ProtocolVersion != ToolProtocolVersion || envelope.Type != "tool_result" || len(envelope.Results) != 1 { t.Fatalf("unexpected tool-result envelope: %s", last.Content) }
	if envelope.Results[0].ID != "call_1" || envelope.Results[0].Name != "workspace.read_file" || envelope.Results[0].Status != "ok" || envelope.Results[0].Result["content"] != "readme contents" { t.Fatalf("tool result does not match FuzeAI training contract: %+v", envelope.Results[0]) }
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

func TestRunnerAcceptsLegacyFuzeAIChatAsFinalText(t *testing.T) {
	sender := &scriptedSender{responses: []string{
		`{"protocol_version":"fuze-agent-v1","type":"chat","message":"I can help with that."}`,
	}}
	registry := NewToolRegistry()
	if err := registry.Register(testToolDefinition("workspace.read_file"), func(context.Context, json.RawMessage) (any, error) {
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Sender: sender, Tools: registry}
	result, err := runner.Run(context.Background(), "test-provider", []provider.Message{{Role: "user", Content: "hello"}}, provider.RequestOptions{}, nil)
	if err != nil {
		t.Fatalf("legacy chat response should be accepted as final text: %v", err)
	}
	if result.Response != "I can help with that." || result.ToolCalls != 0 {
		t.Fatalf("unexpected normalized result: %+v", result)
	}
}

func TestRunnerAcceptsLegacyChatAgentProtocolAsFinalText(t *testing.T) {
	sender := &scriptedSender{responses: []string{
		`{"protocol_version":"chat-agent-v1","type":"chat","message":"FuzeAI is ready."}`,
	}}
	registry := NewToolRegistry()
	if err := registry.Register(testToolDefinition("workspace.read_file"), func(context.Context, json.RawMessage) (any, error) {
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Sender: sender, Tools: registry}
	result, err := runner.Run(context.Background(), "test-provider", []provider.Message{{Role: "user", Content: "hello"}}, provider.RequestOptions{}, nil)
	if err != nil {
		t.Fatalf("legacy chat-agent-v1 response should be accepted as final text: %v", err)
	}
	if result.Response != "FuzeAI is ready." || result.ToolCalls != 0 {
		t.Fatalf("unexpected normalized result: %+v", result)
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


func TestRunnerStopsImmediatelyWhenAuthorizationBlocksTool(t *testing.T) {
	sender := &scriptedSender{responses: []string{
		`{"protocol_version":"fuze-agent-v1","type":"tool_call","tool_calls":[{"id":"delete-1","name":"workspace.apply_edits","arguments":{"files":[{"path":"dist","action":"delete"}],"verify":false}}]}`,
		`{"protocol_version":"fuze-agent-v1","type":"final","response":"I deleted it."}`,
	}}
	registry := NewToolRegistry()
	if err := registry.Register(toolDef("workspace.apply_edits"), func(context.Context, json.RawMessage) (any, error) {
		t.Fatal("handler must not run when approval is denied")
		return nil, nil
	}); err != nil { t.Fatal(err) }
	runner := Runner{Sender: sender, Tools: registry, MaxSteps: 4}
	result, err := runner.Run(context.Background(), "test-provider", []provider.Message{{Role:"user", Content:"delete dist"}}, provider.RequestOptions{}, func(context.Context, ToolDefinition, json.RawMessage) error {
		return errors.New("explicit FuzeCLI confirmation required")
	})
	if err == nil || !strings.Contains(err.Error(), "approval required") {
		t.Fatalf("expected immediate approval stop, got result=%+v err=%v", result, err)
	}
	if sender.calls != 1 { t.Fatalf("model was called %d times after approval was blocked; want 1", sender.calls) }
}


func TestRunnerAcceptsChatAgentV1MessageAndAnswerFields(t *testing.T) {
	sender := &scriptedSender{responses: []string{
		`{"protocol_version":"chat-agent-v1","type":"message","answer":"Your project is ready."}`,
	}}
	registry := NewToolRegistry()
	if err := registry.Register(testToolDefinition("workspace.read_file"), func(context.Context, json.RawMessage) (any, error) {
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}

	runner := Runner{Sender: sender, Tools: registry}
	result, err := runner.Run(context.Background(), "test-provider", []provider.Message{{Role: "user", Content: "hello"}}, provider.RequestOptions{}, nil)
	if err != nil {
		t.Fatalf("chat-agent-v1 text response should be normalized: %v", err)
	}
	if result.Response != "Your project is ready." || result.ToolCalls != 0 {
		t.Fatalf("unexpected normalized result: %+v", result)
	}
}

func TestNormalizeChatResponseDoesNotTreatToolCallAsFinal(t *testing.T) {
	response := AgentResponse{
		ProtocolVersion: "chat-agent-v1",
		Type: AgentResponseToolCall,
	}
	normalizeChatResponse(`{"protocol_version":"chat-agent-v1","type":"tool_call","message":"do something"}`, &response)
	if response.ProtocolVersion != "chat-agent-v1" {
		t.Fatalf("tool-call envelope should not be normalized as final: %+v", response)
	}
}
