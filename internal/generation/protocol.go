package generation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const ResponseProtocolVersion = "fuze-response-v1"

type ResponseType string

const (
	ResponseChat     ResponseType = "chat"
	ResponseEdit     ResponseType = "edit"
	ResponseCommand  ResponseType = "command"
	ResponseAnalysis ResponseType = "analysis"
	ResponseError    ResponseType = "error"
	ResponseProgress ResponseType = "progress"
)

type ProtocolResponse struct {
	Version     string           `json:"protocol_version"`
	Type        ResponseType     `json:"type"`
	Response    string           `json:"response,omitempty"`
	Message     string           `json:"message,omitempty"`
	Explanation string           `json:"explanation,omitempty"`
	Files       []FileChange     `json:"files,omitempty"`
	Commands    []string         `json:"commands,omitempty"`
	Progress    int              `json:"progress,omitempty"`
	Error       string           `json:"error,omitempty"`
}

func NewProtocolResponse(t ResponseType) ProtocolResponse {
	return ProtocolResponse{Version: ResponseProtocolVersion, Type: t}
}

func ValidateProtocolResponse(r ProtocolResponse) error {
	if r.Version == "" {
		r.Version = ResponseProtocolVersion
	}
	if r.Version != ResponseProtocolVersion {
		return fmt.Errorf("unsupported Fuze response protocol %q", r.Version)
	}
	switch r.Type {
	case ResponseChat, ResponseEdit, ResponseCommand, ResponseAnalysis, ResponseError, ResponseProgress:
	default:
		return fmt.Errorf("unsupported response type %q", r.Type)
	}
	switch r.Type {
	case ResponseChat, ResponseAnalysis:
		if strings.TrimSpace(firstText(r.Response, r.Message, r.Explanation)) == "" {
			return errors.New("chat or analysis response must contain text")
		}
	case ResponseEdit:
		if len(r.Files) == 0 {
			return errors.New("edit response must contain files")
		}
		for i, f := range r.Files {
			if strings.TrimSpace(f.Path) == "" {
				return fmt.Errorf("edit file %d has an empty path", i)
			}
			if f.Action != "create" && f.Action != "modify" && f.Action != "delete" {
				return fmt.Errorf("edit file %d has invalid action %q", i, f.Action)
			}
			if f.Action == "delete" && f.Content != "" {
				return fmt.Errorf("edit file %d contains content for a delete action", i)
			}
		}
	case ResponseCommand:
		if len(r.Commands) == 0 {
			return errors.New("command response must contain commands")
		}
	case ResponseError:
		if strings.TrimSpace(r.Error) == "" {
			return errors.New("error response must contain an error message")
		}
	case ResponseProgress:
		if r.Progress < 0 || r.Progress > 100 {
			return errors.New("progress must be between 0 and 100")
		}
	}
	return nil
}

func EncodeProtocolResponse(r ProtocolResponse) ([]byte, error) {
	if r.Version == "" {
		r.Version = ResponseProtocolVersion
	}
	if err := ValidateProtocolResponse(r); err != nil {
		return nil, err
	}
	return json.Marshal(r)
}

func firstText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
