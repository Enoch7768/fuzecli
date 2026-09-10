package provider

import (
	"context"
	"errors"
	"fmt"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type RequestOptions struct {
	Model       string
	Temperature float64
	MaxTokens   int
	JSONMode    bool
	JSONSchema  map[string]any
}

type Response struct {
	Content      string
	Model        string
	ProviderName string
	Usage        Usage
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type StreamChunk struct {
	Delta string
	Done  bool
	Error error
}

type Provider interface {
	Send(ctx context.Context, messages []Message, opts RequestOptions) (*Response, error)
	Stream(ctx context.Context, messages []Message, opts RequestOptions) (<-chan StreamChunk, error)
	ListModels(ctx context.Context) ([]string, error)
	Name() string
}

type ErrorKind string

const (
	ErrorRateLimited         ErrorKind = "rate_limited"
	ErrorQuotaExceeded       ErrorKind = "quota_exceeded"
	ErrorUnauthorized        ErrorKind = "unauthorized"
	ErrorProviderUnavailable ErrorKind = "provider_unavailable"
	ErrorBadRequest          ErrorKind = "bad_request"
	ErrorOverloaded          ErrorKind = "overloaded"
	ErrorUnknown             ErrorKind = "unknown"
)

type ProviderError struct {
	Kind       ErrorKind
	Provider   string
	StatusCode int
	Message    string
	RetryAfter int
	Err        error
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Provider, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Provider, e.Message)
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

func (e *ProviderError) Is(target error) bool {
	switch {
	case target == ErrRateLimited:
		return e.Kind == ErrorRateLimited
	case target == ErrUnauthorized:
		return e.Kind == ErrorUnauthorized
	case target == ErrProviderUnavailable:
		return e.Kind == ErrorProviderUnavailable || e.Kind == ErrorOverloaded
	case target == ErrRequestTooLarge:
		return errors.Is(e.Err, ErrRequestTooLarge)
	default:
		return false
	}
}

var (
	ErrRateLimited         = errors.New("provider rate limited")
	ErrUnauthorized        = errors.New("provider unauthorized")
	ErrProviderUnavailable = errors.New("provider unavailable")
	ErrRequestTooLarge     = errors.New("provider request too large")
)

func ClassifyError(err error) error {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		switch providerErr.Kind {
		case ErrorRateLimited:
			return fmt.Errorf("%w: %v", ErrRateLimited, err)
		case ErrorUnauthorized:
			return fmt.Errorf("%w: %v", ErrUnauthorized, err)
		case ErrorProviderUnavailable, ErrorOverloaded:
			return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
		}
	}
	return err
}
