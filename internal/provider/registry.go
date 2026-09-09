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

func NewRegistry(
	fallback []string,
	defaults map[string]string,
	providers ...Provider,
) *Registry {
	registered := map[string]Provider{}

	for _, provider := range providers {
		registered[provider.Name()] = provider
	}

	return &Registry{
		providers: registered,
		fallback:  append([]string(nil), fallback...),
		models:    defaults,
	}
}

func (r *Registry) Get(
	name string,
) (Provider, error) {
	provider, ok := r.providers[name]

	if !ok {
		return nil, fmt.Errorf(
			"provider %q is not configured",
			name,
		)
	}

	return provider, nil
}

func (r *Registry) ListModels(
	ctx context.Context,
	name string,
) ([]string, error) {
	provider, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	return provider.ListModels(ctx)
}

func (r *Registry) Send(
	ctx context.Context,
	name string,
	messages []Message,
	opts RequestOptions,
) (*Response, error) {
	if name != "auto" {
		provider, err := r.Get(name)
		if err != nil {
			return nil, err
		}

		request := opts

		if request.Model == "" {
			request.Model = r.models[name]
		}

		if request.Model == "" {
			return nil, fmt.Errorf(
				"no default model configured for provider %s",
				name,
			)
		}

		return provider.Send(
			ctx,
			messages,
			request,
		)
	}

	var last error

	for _, candidate := range r.fallback {
		provider, err := r.Get(candidate)
		if err != nil {
			last = err
			continue
		}

		request := opts

		if request.Model == "" {
			request.Model = r.models[candidate]
		}

		if request.Model == "" {
			last = fmt.Errorf(
				"no default model configured for provider %s",
				candidate,
			)
			continue
		}

		response, err := provider.Send(
			ctx,
			messages,
			request,
		)

		if err == nil {
			return response, nil
		}

		if errors.Is(err, ErrRateLimited) ||
			errors.Is(err, ErrProviderUnavailable) {
			last = err
			continue
		}

		return nil, err
	}

	if last == nil {
		last = fmt.Errorf(
			"no providers available for auto mode",
		)
	}

	return nil, last
}

func (r *Registry) Stream(
	ctx context.Context,
	name string,
	messages []Message,
	opts RequestOptions,
) (<-chan StreamChunk, error) {
	if name != "auto" {
		provider, err := r.Get(name)
		if err != nil {
			return nil, err
		}

		request := opts

		if request.Model == "" {
			request.Model = r.models[name]
		}

		if request.Model == "" {
			return nil, fmt.Errorf(
				"no default model configured for provider %s",
				name,
			)
		}

		return provider.Stream(
			ctx,
			messages,
			request,
		)
	}

	var last error

	for _, candidate := range r.fallback {
		provider, err := r.Get(candidate)
		if err != nil {
			last = err
			continue
		}

		request := opts

		if request.Model == "" {
			request.Model = r.models[candidate]
		}

		if request.Model == "" {
			last = fmt.Errorf(
				"no default model configured for provider %s",
				candidate,
			)
			continue
		}

		stream, err := provider.Stream(
			ctx,
			messages,
			request,
		)

		if err == nil {
			return stream, nil
		}

		if errors.Is(err, ErrRateLimited) ||
			errors.Is(err, ErrProviderUnavailable) {
			last = err
			continue
		}

		return nil, err
	}

	if last == nil {
		last = fmt.Errorf(
			"no providers available for auto mode",
		)
	}

	return nil, last
}
