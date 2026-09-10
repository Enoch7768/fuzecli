package diagnostics

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

type UserError struct {
	Title      string `json:"title"`
	Message    string `json:"message"`
	Recovery   string `json:"recovery"`
	Technical  string `json:"technical,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
	RetryAfter int    `json:"retry_after,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Retryable  bool   `json:"retryable"`
}

func (e UserError) Error() string {
	var b strings.Builder
	b.WriteString(e.Title)
	b.WriteString(": ")
	b.WriteString(e.Message)
	if e.Recovery != "" {
		b.WriteString(" ")
		b.WriteString(e.Recovery)
	}
	return b.String()
}

func Interpret(err error, providerName, model string) UserError {
	if err == nil {
		return UserError{}
	}

	var pe *provider.ProviderError
	if errors.As(err, &pe) {
		return interpretProvider(pe, providerName, model)
	}

	message := err.Error()
	lower := strings.ToLower(message)
	switch {
	case errors.Is(err, io.EOF) || strings.Contains(lower, "unexpected end of json input") || strings.Contains(lower, "unexpected eof") || (strings.Contains(lower, "invalid character") && strings.Contains(lower, "json")):
		return UserError{Title: "The AI response was cut off or invalid", Message: "The model ended before FuzeCLI received the complete response it needed.", Recovery: "Retry the request or reduce the project scope so each response stays comfortably inside the model limit.", Technical: message, Provider: providerName, Model: model, Retryable: true}
	case strings.Contains(lower, "response contains incomplete json") || strings.Contains(lower, "response was truncated"):
		return UserError{Title: "The model output was too large", Message: "The structured response was truncated before all requested content could be returned.", Recovery: "Use a smaller project batch or generate the largest file separately. Completed planner work is preserved for resume.", Technical: message, Provider: providerName, Model: model, Retryable: true}
	case strings.Contains(lower, "workspace not initialized"):
		return UserError{Title: "Workspace not initialized", Message: "FuzeCLI is not attached to a workspace yet.", Recovery: "Run aicli init in the project directory, then try again.", Technical: message, Provider: providerName, Model: model}
	case strings.Contains(lower, "no model configured") || strings.Contains(lower, "model is required"):
		return UserError{Title: "No model selected", Message: "FuzeCLI does not have a usable model for this provider.", Recovery: "Choose a model in Settings or set one with --model.", Technical: message, Provider: providerName, Model: model}
	case strings.Contains(lower, "unknown provider"):
		return UserError{Title: "Provider not found", Message: "The selected AI provider is not configured in FuzeCLI.", Recovery: "Open Settings and choose a configured provider.", Technical: message, Provider: providerName, Model: model}
	case strings.Contains(lower, "unsafe workspace path") || strings.Contains(lower, "path traversal") || strings.Contains(lower, "path escapes workspace"):
		return UserError{Title: "Unsafe file path rejected", Message: "FuzeCLI blocked a generated path that could leave the current workspace.", Recovery: "Keep generated paths relative to the workspace and retry.", Technical: message, Provider: providerName, Model: model}
	case strings.Contains(lower, "verification failed"):
		return UserError{Title: "Verification found problems", Message: "The generated files were written, but verification still found issues after the configured correction attempts.", Recovery: "Review the verification output and retry with a narrower scope.", Technical: message, Provider: providerName, Model: model, Retryable: true}
	case strings.Contains(lower, "already working on another request"):
		return UserError{Title: "FuzeCLI is already busy", Message: "Another generation request is already running in this workspace.", Recovery: "Let the current request finish before starting another one.", Technical: message}
	case errors.Is(err, os.ErrPermission) || strings.Contains(lower, "permission denied") || strings.Contains(lower, "access is denied"):
		return UserError{Title: "File access was denied", Message: "Windows did not allow FuzeCLI to read or write one of the requested files.", Recovery: "Check folder permissions and whether another application has the file locked.", Technical: message, Provider: providerName, Model: model}
	case strings.Contains(lower, "no space left") || strings.Contains(lower, "disk full"):
		return UserError{Title: "Not enough disk space", Message: "The workspace drive does not have enough free space for this operation.", Recovery: "Free some disk space and retry.", Technical: message, Provider: providerName, Model: model}
	case strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "timeout"):
		return UserError{Title: "The request timed out", Message: "The provider did not finish responding within the allowed time.", Recovery: "Try a smaller request, a smaller project batch, or another provider/model.", Technical: message, Provider: providerName, Model: model, Retryable: true}
	case isNetworkError(err):
		return UserError{Title: "The AI provider could not be reached", Message: "FuzeCLI could not maintain a connection to the selected provider.", Recovery: "Check your internet connection or provider endpoint and retry.", Technical: message, Provider: providerName, Model: model, Retryable: true}
	default:
		return UserError{Title: "Request failed", Message: compact(message), Recovery: "Retry the request. If it keeps failing, narrow the request and check the selected provider and model.", Technical: message, Provider: providerName, Model: model, Retryable: true}
	}
}

func interpretProvider(err *provider.ProviderError, providerName, model string) UserError {
	if providerName == "" {
		providerName = err.Provider
	}

	base := UserError{Provider: providerName, Model: model, RetryAfter: err.RetryAfter, StatusCode: err.StatusCode, Technical: err.Error()}
	wait := retryText(err.RetryAfter)

	switch err.Kind {
	case provider.ErrorUnauthorized:
		base.Title = "Authentication failed"
		base.Message = "The provider rejected the API credentials before it could process the request."
		base.Recovery = "Check the API key and provider configuration, then retry."
		return base
	case provider.ErrorQuotaExceeded:
		base.Title = "Provider quota exhausted"
		base.Message = "The provider reports that this API account has reached its current usage or billing quota."
		base.Recovery = "Check your provider usage and billing limits, or switch to another configured provider."
		return base
	case provider.ErrorRateLimited:
		base.Title = "Too many requests"
		base.Message = "The provider is rate-limiting this API key or account right now."
		base.Recovery = wait + " Reduce request frequency or switch to another configured provider."
		base.Retryable = true
		return base
	case provider.ErrorOverloaded:
		base.Title = "Model is busy"
		base.Message = "The selected model is temporarily overloaded or unavailable for new work."
		base.Recovery = wait + " Retry shortly, or choose another model/provider."
		base.Retryable = true
		return base
	case provider.ErrorBadRequest:
		base.Title = "The provider rejected the request"
		base.Message = explainProviderBadRequest(err.Message)
		base.Recovery = "Check the selected model and request size. For code generation, try a smaller batch if the request is large."
		base.Retryable = true
		return base
	case provider.ErrorProviderUnavailable:
		base.Title = "Provider temporarily unavailable"
		base.Message = "The selected AI service could not complete the request because its service or connection is temporarily unavailable."
		base.Recovery = wait + " Retry, check the provider status, or switch to another configured provider."
		base.Retryable = true
		return base
	default:
		base.Title = "Provider request failed"
		base.Message = compactProviderMessage(err.Message)
		base.Recovery = "Retry the request or choose another provider/model."
		base.Retryable = true
		return base
	}
}

func retryText(seconds int) string {
	if seconds > 0 {
		return "Retry in about " + strconv.Itoa(seconds) + " seconds."
	}
	return "Wait a moment and retry."
}

func explainProviderBadRequest(message string) string {
	text := compactProviderMessage(message)
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "model") && (strings.Contains(lower, "not found") || strings.Contains(lower, "does not exist") || strings.Contains(lower, "unknown")):
		return "The selected model name is not available for this provider."
	case strings.Contains(lower, "context") || strings.Contains(lower, "token") || strings.Contains(lower, "too large") || strings.Contains(lower, "maximum"):
		return "The request is too large for the selected model's context or request limits."
	case strings.Contains(lower, "json") || strings.Contains(lower, "schema") || strings.Contains(lower, "structured"):
		return "The provider could not accept the structured response format requested by FuzeCLI."
	default:
		return text
	}
}

func compactProviderMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "The provider returned an error while processing the request."
	}
	var obj map[string]any
	if json.Unmarshal([]byte(message), &obj) == nil {
		if text := findString(obj, "message"); text != "" {
			return text
		}
		if text := findString(obj, "error"); text != "" {
			return text
		}
	}
	return compact(message)
}

func findString(v any, key string) string {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			if strings.EqualFold(k, key) {
				if s, ok := value.(string); ok {
					return strings.TrimSpace(s)
				}
			}
			if s := findString(value, key); s != "" {
				return s
			}
		}
	case []any:
		for _, item := range x {
			if s := findString(item, key); s != "" {
				return s
			}
		}
	}
	return ""
}

func compact(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 360 {
		return message[:357] + "..."
	}
	return message
}

func isNetworkError(err error) bool {
	var ne net.Error
	return errors.As(err, &ne)
}

func FormatTerminal(err error, providerName, model string) string {
	u := Interpret(err, providerName, model)
	var b strings.Builder
	b.WriteString("✕ ")
	b.WriteString(u.Title)
	b.WriteString("\n  ")
	b.WriteString(u.Message)
	if u.Provider != "" || u.Model != "" {
		b.WriteString("\n  Service: ")
		if u.Provider != "" {
			b.WriteString(u.Provider)
		}
		if u.Model != "" {
			b.WriteString(" · ")
			b.WriteString(u.Model)
		}
	}
	if u.StatusCode != 0 {
		b.WriteString(fmt.Sprintf("\n  HTTP: %d", u.StatusCode))
	}
	if u.Recovery != "" {
		b.WriteString("\n  Next: ")
		b.WriteString(u.Recovery)
	}
	return b.String()
}
