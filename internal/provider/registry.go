package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	if opts.MaxTokens < 16000 {
		opts.MaxTokens = 16000
	}

	if name != "auto" {
		stream, err := r.streamProvider(ctx, name, messages, opts)
		if err != nil {
			return nil, err
		}

		return r.continueStream(ctx, name, messages, opts, stream), nil
	}

	var last error

	for _, candidate := range r.fallback {
		stream, err := r.streamProvider(ctx, candidate, messages, opts)
		if err == nil {
			return r.continueStream(ctx, candidate, messages, opts, stream), nil
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

func (r *Registry) streamProvider(
	ctx context.Context,
	name string,
	messages []Message,
	opts RequestOptions,
) (<-chan StreamChunk, error) {
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

func (r *Registry) continueStream(
	ctx context.Context,
	name string,
	messages []Message,
	opts RequestOptions,
	initial <-chan StreamChunk,
) <-chan StreamChunk {
	out := make(chan StreamChunk)

	go func() {
		defer close(out)

		current := initial
		combined := strings.Builder{}
		continuations := 0
		limit := opts.MaxTokens * 4
		if limit < 16000 {
			limit = 16000
		}
		threshold := int(float64(limit) * 0.84)

		for {
			finished := false

			for chunk := range current {
				if chunk.Error != nil {
					out <- chunk
					return
				}

				if chunk.Delta != "" {
					combined.WriteString(chunk.Delta)
					out <- StreamChunk{Delta: chunk.Delta}
				}

				if chunk.Done {
					finished = true
				}
			}

			partial := strings.TrimSpace(combined.String())
			if !finished ||
				continuations >= 3 ||
				combined.Len() < threshold ||
				!needsContinuation(partial) {
				out <- StreamChunk{Done: true}
				return
			}

			if partial == "" {
				out <- StreamChunk{Done: true}
				return
			}

			continuations++
			continuationMessages := append(
				append([]Message{}, messages...),
				Message{Role: "assistant", Content: partial},
				Message{Role: "user", Content: "Continue the previous response exactly from where it stopped. Do not repeat any text. Output only the missing continuation. Preserve the same format and complete the response."},
			)

			var err error
			current, err = r.streamProvider(ctx, name, continuationMessages, opts)
			if err != nil {
				out <- StreamChunk{Error: err}
				return
			}
		}
	}()

	return out
}

func needsContinuation(text string) bool {
	if text == "" {
		return false
	}

	if strings.Count(text, "```")%2 != 0 {
		return true
	}

	last := rune(text[len(text)-1])
	for _, mark := range []rune{'.', '!', '?', ':', ';', ')', ']', '}', '"', '\'', '`'} {
		if last == mark {
			return false
		}
	}

	return true
}
