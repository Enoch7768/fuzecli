package diagnostics

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

type UserError struct {
	Title       string `json:"title"`
	Message     string `json:"message"`
	Recovery    string `json:"recovery"`
	Technical   string `json:"technical,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Model       string `json:"model,omitempty"`
	Retryable   bool   `json:"retryable"`
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
	case errors.Is(err, io.EOF) || strings.Contains(lower, "unexpected end of json input") || strings.Contains(lower, "unexpected eof") || strings.Contains(lower, "invalid character") && strings.Contains(lower, "json"):
		return UserError{
			Title:     "The AI response was cut off or invalid",
			Message:   "Gemini returned a response that ended before FuzeCLI could read the required JSON.",
			Recovery:  "FuzeCLI can retry with a smaller generation batch. For a very large file, generate that file on its own.",
			Technical: message,
			Provider:  providerName,
			Model:     model,
			Retryable: true,
		}
	case strings.Contains(lower, "workspace not initialized"):
		return UserError{Title: "Workspace not initialized", Message: "FuzeCLI is not attached to a workspace yet.", Recovery: "Run aicli init in the project directory, then try again.", Technical: message, Provider: providerName, Model: model}
	case strings.Contains(lower, "no model configured") || strings.Contains(lower, "model is required"):
		return UserError{Title: "No model selected", Message: "FuzeCLI does not have a usable model for this provider.", Recovery: "Choose a model in Settings or set it with --model.", Technical: message, Provider: providerName, Model: model}
	case strings.Contains(lower, "unknown provider"):
		return UserError{Title: "Provider not found", Message: "The selected AI provider is not configured in FuzeCLI.", Recovery: "Open Settings and choose one of the configured providers.", Technical: message, Provider: providerName, Model: model}
	case strings.Contains(lower, "unsafe workspace path") || strings.Contains(lower, "path traversal") || strings.Contains(lower, "path escapes workspace"):
		return UserError{Title: "Unsafe file path rejected", Message: "FuzeCLI blocked a path that could leave the current workspace.", Recovery: "Keep generated paths relative to the workspace and try again.", Technical: message, Provider: providerName, Model: model}
	case strings.Contains(lower, "verification failed"):
		return UserError{Title: "Verification found problems", Message: "The generated files were written, but verification still found issues after the configured correction attempts.", Recovery: "Review the verification output and run the request again with a narrower scope.", Technical: message, Provider: providerName, Model: model, Retryable: true}
	case strings.Contains(lower, "already working on another request"):
		return UserError{Title: "FuzeCLI is already working", Message: "Another generation request is currently running.", Recovery: "Wait for the current job to finish before starting another one.", Technical: message}
	case errors.Is(err, os.ErrPermission) || strings.Contains(lower, "permission denied") || strings.Contains(lower, "access is denied"):
		return UserError{Title: "File access was denied", Message: "Windows did not allow FuzeCLI to read or write one of the requested files.", Recovery: "Check folder permissions and whether another application has the file locked.", Technical: message, Provider: providerName, Model: model}
	case errors.Is(err, syscall.ENOSPC) || strings.Contains(lower, "no space left"):
		return UserError{Title: "Not enough disk space", Message: "The workspace drive does not have enough free space for this operation.", Recovery: "Free some disk space and retry.", Technical: message, Provider: providerName, Model: model}
	case strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "timeout"):
		return UserError{Title: "The request timed out", Message: "The provider did not finish responding within the allowed time.", Recovery: "Try a smaller request, a smaller project batch, or another provider/model.", Technical: message, Provider: providerName, Model: model, Retryable: true}
	case isNetworkError(err):
		return UserError{Title: "The AI provider could not be reached", Message: "FuzeCLI could not maintain a connection to the selected provider.", Recovery: "Check your internet connection or provider endpoint, then retry.", Technical: message, Provider: providerName, Model: model, Retryable: true}
	default:
		return UserError{Title: "Request failed", Message: compact(message), Recovery: "Retry the request. If it keeps failing, narrow the request and check the selected provider and model.", Technical: message, Provider: providerName, Model: model, Retryable: true}
	}
}

func interpretProvider(err *provider.ProviderError, providerName, model string) UserError {
	if providerName == "" {
		providerName = err.Provider
	}

	switch err.Kind {
	case provider.ErrorUnauthorized:
		return UserError{Title: "Provider authentication failed", Message: "FuzeCLI reached the provider, but the API key or credentials were rejected.", Recovery: "Open your FuzeCLI configuration and update the provider credentials.", Technical: err.Error(), Provider: providerName, Model: model}
	case provider.ErrorRateLimited:
		return UserError{Title: "Provider rate limit reached", Message: "The provider is temporarily limiting requests from this API key.", Recovery: "Wait a moment, then retry or switch to another configured provider.", Technical: err.Error(), Provider: providerName, Model: model, Retryable: true}
	case provider.ErrorBadRequest:
		return UserError{Title: "The provider rejected the request", Message: compactProviderMessage(err.Message), Recovery: "FuzeCLI can retry automatically when the problem is JSON or batch size related; otherwise check the selected model and request scope.", Technical: err.Error(), Provider: providerName, Model: model, Retryable: true}
	case provider.ErrorProviderUnavailable:
		return UserError{Title: "Provider unavailable", Message: "The selected AI service is unavailable or unreachable right now.", Recovery: "Check the provider status or connection and retry. Auto mode can use another configured provider.", Technical: err.Error(), Provider: providerName, Model: model, Retryable: true}
	}

	return UserError{Title: "Provider request failed", Message: compactProviderMessage(err.Message), Recovery: "Retry the request or choose another provider/model.", Technical: err.Error(), Provider: providerName, Model: model, Retryable: true}
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
	b.WriteString(fmt.Sprintf("%s\n", u.Title))
	b.WriteString(fmt.Sprintf("  %s\n", u.Message))
	if u.Recovery != "" {
		b.WriteString(fmt.Sprintf("  Next: %s\n", u.Recovery))
	}
	return strings.TrimRight(b.String(), "\n")
}
