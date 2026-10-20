package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateAgentResponseAcceptsFinal(t *testing.T) {
	response := AgentResponse{
		ProtocolVersion: ToolProtocolVersion,
		Type:            AgentResponseFinal,
		Response:        "The bug is fixed.",
	}
	if err := ValidateAgentResponse(response); err != nil {
		t.Fatalf("expected valid final response, got %v", err)
	}
}

func TestValidateAgentResponseAcceptsToolCall(t *testing.T) {
	response := AgentResponse{
		ProtocolVersion: ToolProtocolVersion,
		Type:            AgentResponseToolCall,
		ToolCalls: []ToolCall{{
			ID:        "call_1",
			Name:      "workspace.read_file",
			Arguments: json.RawMessage(`{"path":"main.go"}`),
		}},
	}
	if err := ValidateAgentResponse(response); err != nil {
		t.Fatalf("expected valid tool call, got %v", err)
	}
}

func TestValidateAgentResponseRejectsInvalidToolCalls(t *testing.T) {
	tests := []struct {
		name     string
		response AgentResponse
		want     string
	}{
		{
			name: "unsupported protocol",
			response: AgentResponse{
				ProtocolVersion: "unknown",
				Type:            AgentResponseFinal,
				Response:        "done",
			},
			want: "unsupported agent protocol",
		},
		{
			name: "empty final response",
			response: AgentResponse{
				ProtocolVersion: ToolProtocolVersion,
				Type:            AgentResponseFinal,
			},
			want: "final response must contain text",
		},
		{
			name: "empty tool calls",
			response: AgentResponse{
				ProtocolVersion: ToolProtocolVersion,
				Type:            AgentResponseToolCall,
			},
			want: "at least one tool call",
		},
		{
			name: "arguments must be object",
			response: AgentResponse{
				ProtocolVersion: ToolProtocolVersion,
				Type:            AgentResponseToolCall,
				ToolCalls: []ToolCall{{
					ID:        "call_1",
					Name:      "workspace.read_file",
					Arguments: json.RawMessage(`["not","an","object"]`),
				}},
			},
			want: "arguments must be a JSON object",
		},
		{
			name: "duplicate ids",
			response: AgentResponse{
				ProtocolVersion: ToolProtocolVersion,
				Type:            AgentResponseToolCall,
				ToolCalls: []ToolCall{
					{ID: "same", Name: "workspace.read_file", Arguments: json.RawMessage(`{}`)},
					{ID: "same", Name: "workspace.list_files", Arguments: json.RawMessage(`{}`)},
				},
			},
			want: "duplicate tool call id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAgentResponse(tt.response)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestValidateToolResult(t *testing.T) {
	valid := ToolResult{
		ProtocolVersion: ToolProtocolVersion,
		ToolCallID:      "call_1",
		Name:            "workspace.read_file",
		Status:          ToolResultSuccess,
		Result:          json.RawMessage(`{"content":"hello"}`),
	}
	if err := ValidateToolResult(valid); err != nil {
		t.Fatalf("expected valid tool result, got %v", err)
	}

	invalid := valid
	invalid.Status = ToolResultError
	invalid.Result = nil
	invalid.Error = "permission denied"
	if err := ValidateToolResult(invalid); err != nil {
		t.Fatalf("expected valid error result, got %v", err)
	}
}
