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
		return &Response{Content: "{\"type\":\"edit\",\"files\":[{\"path\":\"index.html\",\"content\":\"<div>"}, Model: "test-model", ProviderName: p.Name()}, nil
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
