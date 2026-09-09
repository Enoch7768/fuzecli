package provider

import (
	"context"
	"errors"
	"fmt"
)

type Registry struct {
	providers map[string]Provider
	fallback  []string
	models    map[string]string
}

func NewRegistry(fallback []string, defaults map[string]string, providers ...Provider) *Registry {
	m := map[string]Provider{}
	for _, p := range providers {
		m[p.Name()] = p
	}
	return &Registry{providers: m, fallback: fallback, models: defaults}
}

func (r *Registry) Get(name string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q is not configured", name)
	}
	return p, nil
}

func (r *Registry) Send(ctx context.Context, name string, messages []Message, opts RequestOptions) (*Response, error) {
	if name != "auto" {
		p, err := r.Get(name)
		if err != nil {
			return nil, err
		}
		return p.Send(ctx, messages, opts)
	}
	var last error
	for _, candidate := range r.fallback {
		p, err := r.Get(candidate)
		if err != nil {
			last = err
			continue
		}
		request := opts
		if request.Model == "" {
			request.Model = r.models[candidate]
		}
		if request.Model == "" {
			last = fmt.Errorf("no default model configured for provider %s", candidate)
			continue
		}
		resp, err := p.Send(ctx, messages, request)
		if err == nil {
			return resp, nil
		}
		if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) {
			last = err
			continue
		}
		return nil, err
	}
	if last == nil {
		last = fmt.Errorf("no providers available for auto mode")
	}
	return nil, last
}

func (r *Registry) Stream(ctx context.Context, name string, messages []Message, opts RequestOptions) (<-chan StreamChunk, error) {
	if name != "auto" {
		p, err := r.Get(name)
		if err != nil {
			return nil, err
		}
		return p.Stream(ctx, messages, opts)
	}
	var last error
	for _, candidate := range r.fallback {
		p, err := r.Get(candidate)
		if err != nil {
			last = err
			continue
		}
		request := opts
		if request.Model == "" {
			request.Model = r.models[candidate]
		}
		if request.Model == "" {
			last = fmt.Errorf("no default model configured for provider %s", candidate)
			continue
		}
		ch, err := p.Stream(ctx, messages, request)
		if err == nil {
			return ch, nil
		}
		if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) {
			last = err
			continue
		}
		return nil, err
	}
	if last == nil {
		last = fmt.Errorf("no providers available for auto mode")
	}
	return nil, last
}
