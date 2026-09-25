package provider

import (
	"context"
	"strings"
	"testing"
)

type modelSafetyProvider struct {
	name string
	received []string
}

func (p *modelSafetyProvider) Name() string { return p.name }
func (p *modelSafetyProvider) ListModels(context.Context) ([]string, error) { return []string{"real-model"}, nil }
func (p *modelSafetyProvider) Send(_ context.Context, _ []Message, opts RequestOptions) (*Response, error) {
	p.received = append(p.received, opts.Model)
	return &Response{Content: `{"response":"ok","message":"ok","type":"answer","files":[],"explanation":"","commands":[]}`, Model: opts.Model, ProviderName: p.name}, nil
}
func (p *modelSafetyProvider) Stream(context.Context, []Message, RequestOptions) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	ch <- StreamChunk{Delta: "ok", Done: true}
	close(ch)
	return ch, nil
}

func TestRegistryNeverSendsPlaceholderModel(t *testing.T) {
	p := &modelSafetyProvider{name: "test"}
	r := NewRegistry(nil, map[string]string{"test": "default"}, p)
	for _, requested := range []string{"", "auto", "default"} {
		p.received = nil
		_, err := r.Send(context.Background(), "test", []Message{{Role: "user", Content: "hi"}}, RequestOptions{Model: requested})
		if err != nil { t.Fatalf("requested %q: %v", requested, err) }
		if len(p.received) != 1 { t.Fatalf("requested %q: provider was not called", requested) }
		if strings.EqualFold(p.received[0], "") || strings.EqualFold(p.received[0], "auto") || strings.EqualFold(p.received[0], "default") {
			t.Fatalf("placeholder model reached provider: %q", p.received[0])
		}
		if p.received[0] != "real-model" { t.Fatalf("expected resolved model, got %q", p.received[0]) }
	}
}

type emptyModelProvider struct{}
func (p *emptyModelProvider) Name() string { return "empty" }
func (p *emptyModelProvider) ListModels(context.Context) ([]string, error) { return nil, nil }
func (p *emptyModelProvider) Send(context.Context, []Message, RequestOptions) (*Response, error) { return nil, nil }
func (p *emptyModelProvider) Stream(context.Context, []Message, RequestOptions) (<-chan StreamChunk, error) { return nil, nil }

func TestRegistryRejectsUnresolvableModel(t *testing.T) {
	p := &emptyModelProvider{}
	r := NewRegistry(nil, map[string]string{"empty": "default"}, p)
	_, err := r.Send(context.Background(), "empty", []Message{{Role: "user", Content: "hi"}}, RequestOptions{Model: "default"})
	if err == nil || !strings.Contains(err.Error(), "no usable model") { t.Fatalf("expected no usable model error, got %v", err) }
}
