package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

type jsonContinuationProvider struct { calls int }
func (p *jsonContinuationProvider) Name() string { return "json-test" }
func (p *jsonContinuationProvider) ListModels(context.Context) ([]string, error) { return []string{"test-model"}, nil }
func (p *jsonContinuationProvider) Stream(context.Context, []Message, RequestOptions) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 2)
	ch <- StreamChunk{Delta: "ok"}
	ch <- StreamChunk{Done: true}
	close(ch)
	return ch, nil
}
func (p *jsonContinuationProvider) Send(context.Context, []Message, RequestOptions) (*Response, error) {
	p.calls++
	if p.calls == 1 {
		return &Response{Content: "{\"type\":\"edit\",\"files\":[{\"path\":\"index.html\",\"content\":\"<div>", Model: "test-model", ProviderName: p.Name()}, nil
	}
	return &Response{Content: "div</div>\"}],\"explanation\":\"complete\",\"commands\":[]}", Model: "test-model", ProviderName: p.Name()}, nil
}

func TestRegistryContinuesTruncatedStructuredJSON(t *testing.T) {
	p := &jsonContinuationProvider{}
	r := NewRegistry([]string{"json-test"}, map[string]string{"json-test": "test-model"}, p)
	got, err := r.Send(context.Background(), "json-test", []Message{{Role: "user", Content: "create a page"}}, RequestOptions{Model: "test-model", JSONMode: true, JSONSchema: map[string]any{"type": "object"}})
	if err != nil { t.Fatalf("Send returned error: %v", err) }
	want := "{\"type\":\"edit\",\"files\":[{\"path\":\"index.html\",\"content\":\"<div>div</div>\"}],\"explanation\":\"complete\",\"commands\":[]}"
	if got == nil || got.Content != want { t.Fatalf("unexpected combined JSON: %q", got.Content) }
	if p.calls != 2 { t.Fatalf("provider calls = %d, want 2", p.calls) }
}


type jsonSchemaFallbackProvider struct {
	strictCalls int
	schemaCalls int
}

func (p *jsonSchemaFallbackProvider) Name() string { return "schema-test" }
func (p *jsonSchemaFallbackProvider) ListModels(context.Context) ([]string, error) { return []string{"test-model"}, nil }
func (p *jsonSchemaFallbackProvider) Stream(context.Context, []Message, RequestOptions) (<-chan StreamChunk, error) {
	return nil, &ProviderError{Kind: ErrorBadRequest, Provider: p.Name(), Message: "stream structured output unsupported"}
}
func (p *jsonSchemaFallbackProvider) Send(_ context.Context, _ []Message, opts RequestOptions) (*Response, error) {
	if opts.JSONSchema != nil {
		p.schemaCalls++
		if opts.JSONSchemaStrict {
			p.strictCalls++
			return nil, &ProviderError{Kind: ErrorBadRequest, Provider: p.Name(), Message: "structured output schema unsupported in strict mode"}
		}
	}
	return &Response{Content: "{\"type\":\"chat\",\"response\":\"ok\",\"message\":\"ok\",\"files\":[],\"explanation\":\"\",\"commands\":[]}", Model: "test-model", ProviderName: p.Name()}, nil
}

func TestRegistryStructuredJSONFallbackKeepsSchemaBeforeObjectMode(t *testing.T) {
	p := &jsonSchemaFallbackProvider{}
	r := NewRegistry([]string{"schema-test"}, map[string]string{"schema-test": "test-model"}, p)
	schema := map[string]any{"type": "object", "additionalProperties": false}
	got, err := r.Send(context.Background(), "schema-test", []Message{{Role: "user", Content: "hello"}}, RequestOptions{Model: "test-model", JSONMode: true, JSONSchema: schema})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if got == nil || got.Content == "" {
		t.Fatal("expected structured response")
	}
	if p.strictCalls != 1 {
		t.Fatalf("strict schema calls = %d, want 1", p.strictCalls)
	}
	if p.schemaCalls != 2 {
		t.Fatalf("schema calls = %d, want 2", p.schemaCalls)
	}
}


type jsonModeFallbackProvider struct {
	calls int
}

func (p *jsonModeFallbackProvider) Name() string { return "json-mode-test" }
func (p *jsonModeFallbackProvider) ListModels(context.Context) ([]string, error) {
	return []string{"test-model"}, nil
}
func (p *jsonModeFallbackProvider) Stream(context.Context, []Message, RequestOptions) (<-chan StreamChunk, error) {
	return nil, &ProviderError{Kind: ErrorBadRequest, Provider: p.Name(), Message: "JSON not accepted by this model"}
}
func (p *jsonModeFallbackProvider) Send(_ context.Context, _ []Message, opts RequestOptions) (*Response, error) {
	p.calls++
	if opts.JSONMode {
		return nil, &ProviderError{Kind: ErrorBadRequest, Provider: p.Name(), Message: "JSON not accepted by this model"}
	}
	return &Response{Content: "Plain text response", Model: "test-model", ProviderName: p.Name()}, nil
}

func TestRegistryFallsBackWhenProviderRejectsJSONMode(t *testing.T) {
	p := &jsonModeFallbackProvider{}
	r := NewRegistry([]string{"json-mode-test"}, map[string]string{"json-mode-test": "test-model"}, p)
	got, err := r.Send(context.Background(), "json-mode-test", []Message{{Role: "user", Content: "hello"}}, RequestOptions{Model: "test-model", JSONMode: true})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if got == nil || got.Content != "Plain text response" {
		t.Fatalf("unexpected fallback response: %#v", got)
	}
	if p.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", p.calls)
	}
}


type emptyThenValidProvider struct {
	calls int
}

func (p *emptyThenValidProvider) Name() string { return "empty-test" }
func (p *emptyThenValidProvider) ListModels(context.Context) ([]string, error) { return []string{"test-model"}, nil }
func (p *emptyThenValidProvider) Stream(context.Context, []Message, RequestOptions) (<-chan StreamChunk, error) {
	return nil, &ProviderError{Kind: ErrorBadRequest, Provider: p.Name(), Message: "stream not used"}
}
func (p *emptyThenValidProvider) Send(context.Context, []Message, RequestOptions) (*Response, error) {
	p.calls++
	if p.calls == 1 {
		return &Response{Content: "   ", Model: "test-model", ProviderName: p.Name()}, nil
	}
	return &Response{Content: "Recovered response", Model: "test-model", ProviderName: p.Name()}, nil
}

func TestRegistryRetriesEmptyResponse(t *testing.T) {
	p := &emptyThenValidProvider{}
	r := NewRegistry([]string{"empty-test"}, map[string]string{"empty-test": "test-model"}, p)
	got, err := r.Send(context.Background(), "empty-test", []Message{{Role: "user", Content: "hello"}}, RequestOptions{Model: "test-model"})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if got == nil || got.Content != "Recovered response" {
		t.Fatalf("unexpected recovered response: %#v", got)
	}
	if p.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", p.calls)
	}
}

type unavailableModelProvider struct{}

func (p *unavailableModelProvider) Name() string { return "unavailable-test" }
func (p *unavailableModelProvider) ListModels(context.Context) ([]string, error) { return nil, nil }
func (p *unavailableModelProvider) Stream(context.Context, []Message, RequestOptions) (<-chan StreamChunk, error) {
	return nil, &ProviderError{Kind: ErrorModelNotFound, Provider: p.Name(), Message: "model not found"}
}
func (p *unavailableModelProvider) Send(context.Context, []Message, RequestOptions) (*Response, error) {
	return nil, &ProviderError{Kind: ErrorModelNotFound, Provider: p.Name(), Message: "model not found"}
}

type fallbackSuccessProvider struct{}

func (p *fallbackSuccessProvider) Name() string { return "fallback-test" }
func (p *fallbackSuccessProvider) ListModels(context.Context) ([]string, error) { return []string{"test-model"}, nil }
func (p *fallbackSuccessProvider) Stream(context.Context, []Message, RequestOptions) (<-chan StreamChunk, error) {
	return nil, &ProviderError{Kind: ErrorBadRequest, Provider: p.Name(), Message: "stream not used"}
}
func (p *fallbackSuccessProvider) Send(context.Context, []Message, RequestOptions) (*Response, error) {
	return &Response{Content: "Fallback response", Model: "test-model", ProviderName: p.Name()}, nil
}

func TestRegistryFallsBackWhenModelIsUnavailable(t *testing.T) {
	primary := &unavailableModelProvider{}
	fallback := &fallbackSuccessProvider{}
	r := NewRegistry([]string{"unavailable-test", "fallback-test"}, map[string]string{"unavailable-test": "bad-model", "fallback-test": "test-model"}, primary, fallback)
	got, err := r.Send(context.Background(), "unavailable-test", []Message{{Role: "user", Content: "hello"}}, RequestOptions{Model: "bad-model"})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if got == nil || got.Content != "Fallback response" || got.ProviderName != "fallback-test" {
		t.Fatalf("unexpected fallback response: %#v", got)
	}
}

type deadlineProvider struct{}

func (p *deadlineProvider) Name() string { return "deadline-test" }
func (p *deadlineProvider) ListModels(context.Context) ([]string, error) { return []string{"test-model"}, nil }
func (p *deadlineProvider) Stream(context.Context, []Message, RequestOptions) (<-chan StreamChunk, error) {
	return nil, context.DeadlineExceeded
}
func (p *deadlineProvider) Send(ctx context.Context, _ []Message, _ RequestOptions) (*Response, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRegistryReturnsPromptlyWhenRequestTimesOut(t *testing.T) {
	p := &deadlineProvider{}
	r := NewRegistry([]string{"deadline-test"}, map[string]string{"deadline-test": "test-model"}, p)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := r.Send(ctx, "deadline-test", []Message{{Role: "user", Content: "hello"}}, RequestOptions{Model: "test-model"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}


type emptyStreamProvider struct{}

func (p *emptyStreamProvider) Name() string { return "empty-stream-test" }
func (p *emptyStreamProvider) ListModels(context.Context) ([]string, error) { return []string{"test-model"}, nil }
func (p *emptyStreamProvider) Stream(context.Context, []Message, RequestOptions) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	ch <- StreamChunk{Done: true}
	close(ch)
	return ch, nil
}
func (p *emptyStreamProvider) Send(context.Context, []Message, RequestOptions) (*Response, error) {
	return &Response{Content: "Recovered stream response", Model: "test-model", ProviderName: p.Name()}, nil
}

func TestRegistryRecoversEmptyStream(t *testing.T) {
	p := &emptyStreamProvider{}
	r := NewRegistry([]string{"empty-stream-test"}, map[string]string{"empty-stream-test": "test-model"}, p)
	stream, err := r.Stream(context.Background(), "empty-stream-test", []Message{{Role: "user", Content: "hello"}}, RequestOptions{Model: "test-model"})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	var got string
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("stream returned error: %v", chunk.Error)
		}
		got += chunk.Delta
	}
	if got != "Recovered stream response" {
		t.Fatalf("stream response = %q, want recovered response", got)
	}
}
