package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const ToolProtocolVersion = "fuze-agent-v1"

type AgentResponseType string

const (
	AgentResponseFinal    AgentResponseType = "final"
	AgentResponseToolCall AgentResponseType = "tool_call"
	AgentResponseError    AgentResponseType = "error"
)

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type AgentResponse struct {
	ProtocolVersion string            `json:"protocol_version"`
	Type            AgentResponseType `json:"type"`
	Response        string            `json:"response,omitempty"`
	ToolCalls       []ToolCall        `json:"tool_calls,omitempty"`
	Error           string            `json:"error,omitempty"`
}

type ToolResultStatus string

const (
	ToolResultSuccess ToolResultStatus = "success"
	ToolResultError   ToolResultStatus = "error"
)

type ToolResult struct {
	ProtocolVersion string           `json:"protocol_version"`
	ToolCallID      string           `json:"tool_call_id"`
	Name            string           `json:"name"`
	Status          ToolResultStatus `json:"status"`
	Result          json.RawMessage  `json:"result,omitempty"`
	Error           string           `json:"error,omitempty"`
}

func ValidateAgentResponse(response AgentResponse) error {
	if response.ProtocolVersion != ToolProtocolVersion {
		return fmt.Errorf("unsupported agent protocol %q", response.ProtocolVersion)
	}

	switch response.Type {
	case AgentResponseFinal:
		if strings.TrimSpace(response.Response) == "" {
			return errors.New("final response must contain text")
		}
		if len(response.ToolCalls) != 0 {
			return errors.New("final response cannot contain tool calls")
		}
		if response.Error != "" {
			return errors.New("final response cannot contain an error")
		}
	case AgentResponseToolCall:
		if len(response.ToolCalls) == 0 {
			return errors.New("tool_call response must contain at least one tool call")
		}
		if response.Response != "" || response.Error != "" {
			return errors.New("tool_call response cannot contain final text or an error")
		}
		seenIDs := make(map[string]struct{}, len(response.ToolCalls))
		for i, call := range response.ToolCalls {
			if strings.TrimSpace(call.ID) == "" {
				return fmt.Errorf("tool call %d has an empty id", i)
			}
			if strings.TrimSpace(call.Name) == "" || strings.ContainsAny(call.Name, " \t\r\n") {
				return fmt.Errorf("tool call %d has an invalid name", i)
			}
			if _, exists := seenIDs[call.ID]; exists {
				return fmt.Errorf("duplicate tool call id %q", call.ID)
			}
			seenIDs[call.ID] = struct{}{}
			if len(call.Arguments) == 0 || !json.Valid(call.Arguments) {
				return fmt.Errorf("tool call %q has invalid JSON arguments", call.ID)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(call.Arguments, &object); err != nil || object == nil {
				return fmt.Errorf("tool call %q arguments must be a JSON object", call.ID)
			}
		}
	case AgentResponseError:
		if strings.TrimSpace(response.Error) == "" {
			return errors.New("error response must contain an error message")
		}
		if len(response.ToolCalls) != 0 {
			return errors.New("error response cannot contain tool calls")
		}
	default:
		return fmt.Errorf("unsupported agent response type %q", response.Type)
	}

	return nil
}

func ValidateToolResult(result ToolResult) error {
	if result.ProtocolVersion != ToolProtocolVersion {
		return fmt.Errorf("unsupported agent protocol %q", result.ProtocolVersion)
	}
	if strings.TrimSpace(result.ToolCallID) == "" {
		return errors.New("tool result must include a tool call id")
	}
	if strings.TrimSpace(result.Name) == "" {
		return errors.New("tool result must include a tool name")
	}

	switch result.Status {
	case ToolResultSuccess:
		if result.Error != "" {
			return errors.New("successful tool result cannot contain an error")
		}
		if len(result.Result) > 0 && !json.Valid(result.Result) {
			return errors.New("tool result contains invalid JSON")
		}
	case ToolResultError:
		if strings.TrimSpace(result.Error) == "" {
			return errors.New("failed tool result must contain an error message")
		}
		if len(result.Result) > 0 {
			return errors.New("failed tool result cannot contain a success result")
		}
	default:
		return fmt.Errorf("unsupported tool result status %q", result.Status)
	}

	return nil
}
