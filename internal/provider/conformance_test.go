package provider

import (
	"context"
	"errors"
	"testing"
)

type conformanceProvider struct {
	name string
}

func (p conformanceProvider) Name() string {
	return p.name
}

func (p conformanceProvider) Send(ctx context.Context, messages []Message, opts RequestOptions) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Response{
		Content:      "ok",
		Model:        opts.Model,
		ProviderName: p.name,
		Usage:        Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
	}, nil
}

func (p conformanceProvider) Stream(ctx context.Context, messages []Message, opts RequestOptions) (<-chan StreamChunk, error) {
	out := make(chan StreamChunk, 2)
	go func() {
		defer close(out)
		select {
		case out <- StreamChunk{Delta: "ok"}:
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err()}
			return
		}
		out <- StreamChunk{Done: true}
	}()
	return out, nil
}

func (p conformanceProvider) ListModels(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []string{"test-model"}, nil
}

func TestProviderConformance(t *testing.T) {
	ctx := context.Background()
	p := conformanceProvider{name: "test"}

	if p.Name() == "" {
		t.Fatal("provider name must not be empty")
	}

	response, err := p.Send(ctx, []Message{{Role: "user", Content: "hello"}}, RequestOptions{Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	if response == nil || response.Content == "" {
		t.Fatal("provider returned an empty response")
	}
	if response.ProviderName != p.Name() {
		t.Fatalf("provider name = %q, want %q", response.ProviderName, p.Name())
	}

	stream, err := p.Stream(ctx, []Message{{Role: "user", Content: "hello"}}, RequestOptions{Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	var delta string
	done := false
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
		delta += chunk.Delta
		done = done || chunk.Done
	}
	if delta == "" || !done {
		t.Fatalf("stream contract failed: delta=%q done=%v", delta, done)
	}

	models, err := p.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 || models[0] == "" {
		t.Fatal("ListModels must return at least one non-empty model")
	}
}

func TestProviderErrorClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "rate limit",
			err:  &ProviderError{Kind: ErrorRateLimited, Provider: "test", Message: "slow down"},
			want: ErrRateLimited,
		},
		{
			name: "unauthorized",
			err:  &ProviderError{Kind: ErrorUnauthorized, Provider: "test", Message: "invalid key"},
			want: ErrUnauthorized,
		},
		{
			name: "unavailable",
			err:  &ProviderError{Kind: ErrorOverloaded, Provider: "test", Message: "busy"},
			want: ErrProviderUnavailable,
		},
		{
			name: "request too large",
			err:  &ProviderError{Kind: ErrorBadRequest, Provider: "test", Message: "too large", Err: ErrRequestTooLarge},
			want: ErrRequestTooLarge,
		},
		{
			name: "model missing",
			err:  &ProviderError{Kind: ErrorModelNotFound, Provider: "test", Message: "missing"},
			want: ErrModelNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, tc.want) {
				t.Fatalf("errors.Is(%v, %v) = false", tc.err, tc.want)
			}
		})
	}
}

func TestRegistryUsesProviderContract(t *testing.T) {
	p := conformanceProvider{name: "test"}
	registry := NewRegistry([]string{"test"}, map[string]string{"test": "test-model"}, p)

	response, err := registry.Send(context.Background(), "test", []Message{{Role: "user", Content: "hello"}}, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if response.ProviderName != "test" || response.Model != "test-model" {
		t.Fatalf("response metadata = %#v", response)
	}

	stream, err := registry.Stream(context.Background(), "test", []Message{{Role: "user", Content: "hello"}}, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	var done bool
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatal(chunk.Error)
		}
		got += chunk.Delta
		done = done || chunk.Done
	}
	if got != "ok" || !done {
		t.Fatalf("registry stream = %q done=%v", got, done)
	}
}
