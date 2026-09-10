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

		messages, request = adaptRequest(name, messages, request)

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

		requestMessages, request := adaptRequest(candidate, messages, request)

		response, err := provider.Send(
			ctx,
			requestMessages,
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
		streamMessages, streamOptions := adaptRequest(name, messages, opts)
		stream, err := r.streamProvider(ctx, name, streamMessages, streamOptions)
		if err != nil {
			return nil, err
		}

		return r.continueStream(ctx, name, streamMessages, streamOptions, stream), nil
	}

	var last error

	for _, candidate := range r.fallback {
		streamMessages, streamOptions := adaptRequest(candidate, messages, opts)
		stream, err := r.streamProvider(ctx, candidate, streamMessages, streamOptions)
		if err == nil {
			return r.continueStream(ctx, candidate, streamMessages, streamOptions, stream), nil
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

func adaptRequest(
	name string,
	messages []Message,
	opts RequestOptions,
) ([]Message, RequestOptions) {
	if name != "groq" {
		return messages, opts
	}

	if opts.MaxTokens <= 0 || opts.MaxTokens > 3000 {
		opts.MaxTokens = 3000
	}

	return trimProviderMessages(messages, 12000), opts
}

func trimProviderMessages(messages []Message, maxChars int) []Message {
	if len(messages) == 0 || maxChars <= 0 {
		return messages
	}

	total := 0
	for _, message := range messages {
		total += len(message.Content)
	}

	if total <= maxChars {
		return messages
	}

	if len(messages) == 1 {
		return []Message{truncateMessage(messages[0], maxChars)}
	}

	first := messages[0]
	last := messages[len(messages)-1]
	budget := maxChars - len(first.Content)
	if budget <= 0 {
		return []Message{
			truncateMessage(first, maxChars/2),
			truncateMessage(last, maxChars-maxChars/2),
		}
	}

	if len(last.Content) >= budget {
		firstBudget := maxChars / 4
		lastBudget := maxChars - firstBudget
		return []Message{
			truncateMessage(first, firstBudget),
			truncateMessage(last, lastBudget),
		}
	}

	result := []Message{first}
	used := len(first.Content) + len(last.Content)

	for i := len(messages) - 2; i >= 1; i-- {
		remaining := maxChars - used
		if remaining <= 0 {
			break
		}
		if len(messages[i].Content) <= remaining {
			result = append([]Message{messages[i]}, result...)
			used += len(messages[i].Content)
			continue
		}
		result = append([]Message{truncateMessage(messages[i], remaining)}, result...)
		used = maxChars
		break
	}

	result = append(result, last)
	return result
}

func truncateMessage(message Message, maxChars int) Message {
	if maxChars <= 0 {
		return Message{Role: message.Role}
	}
	if len(message.Content) <= maxChars {
		return message
	}
	content := message.Content[:maxChars]
	if maxChars > 96 {
		content = content[:maxChars-96] + "\n\n[Earlier content trimmed by FuzeCLI to respect the provider token limit.]"
	}
	message.Content = content
	return message
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
		if limit < 12000 {
			limit = 12000
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

			continuationMessages, continuationOptions := adaptRequest(name, continuationMessages, opts)
			var err error
			current, err = r.streamProvider(ctx, name, continuationMessages, continuationOptions)
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
