package provider

import (
	"context"
	"testing"
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
