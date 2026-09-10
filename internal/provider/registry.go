package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Registry struct {
	providers  map[string]Provider
	fallback   []string
	models     map[string]string
	limitMu    sync.Mutex
	requestMu  map[string]*sync.Mutex
	lastCall   map[string]time.Time
	retryUntil map[string]time.Time
	attempts   map[string]int
}

func NewRegistry(
	fallback []string,
	defaults map[string]string,
	providers ...Provider,
) *Registry {
	registered := map[string]Provider{}
	requestMu := map[string]*sync.Mutex{}

	for _, provider := range providers {
		registered[provider.Name()] = provider
		requestMu[provider.Name()] = &sync.Mutex{}
	}

	return &Registry{
		providers:  registered,
		fallback:   append([]string(nil), fallback...),
		models:     defaults,
		requestMu:  requestMu,
		lastCall:   map[string]time.Time{},
		retryUntil: map[string]time.Time{},
		attempts:   map[string]int{},
	}
}

func (r *Registry) Get(name string) (Provider, error) {
	provider, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q is not configured", name)
	}
	return provider, nil
}

func (r *Registry) ListModels(ctx context.Context, name string) ([]string, error) {
	provider, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	return provider.ListModels(ctx)
}

func (r *Registry) Send(ctx context.Context, name string, messages []Message, opts RequestOptions) (*Response, error) {
	if name != "auto" {
		requestMessages, request := adaptRequest(name, messages, opts)
		request.Model = r.model(name, request.Model)
		if request.Model == "" {
			return nil, fmt.Errorf("no default model configured for provider %s", name)
		}
		if err := validateRequestBudget(name, requestMessages, request); err != nil {
			return nil, err
		}
		return r.sendWithRetry(ctx, name, requestMessages, request)
	}

	var last error
	for _, candidate := range r.fallback {
		requestMessages, request := adaptRequest(candidate, messages, opts)
		request.Model = r.model(candidate, request.Model)
		if request.Model == "" {
			last = fmt.Errorf("no default model configured for provider %s", candidate)
			continue
		}
		if err := validateRequestBudget(candidate, requestMessages, request); err != nil {
			last = err
			if errors.Is(err, ErrRequestTooLarge) || errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) {
				continue
			}
			return nil, err
		}
		response, err := r.sendWithRetry(ctx, candidate, requestMessages, request)
		if err == nil {
			return response, nil
		}
		if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) || errors.Is(err, ErrRequestTooLarge) {
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

func (r *Registry) sendWithRetry(ctx context.Context, name string, messages []Message, opts RequestOptions) (*Response, error) {
	lock := r.providerLock(name)
	lock.Lock()
	defer lock.Unlock()

	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if err := r.wait(ctx, name); err != nil {
			return nil, err
		}
		provider, err := r.Get(name)
		if err != nil {
			return nil, err
		}
		response, err := provider.Send(ctx, messages, opts)
		r.observe(name, err)
		if err == nil {
			return response, nil
		}
		last = err
		if !isRetryableProviderError(err) {
			return nil, err
		}
	}
	return nil, last
}

func (r *Registry) Stream(ctx context.Context, name string, messages []Message, opts RequestOptions) (<-chan StreamChunk, error) {
	if name != "auto" {
		streamMessages, streamOptions := adaptRequest(name, messages, opts)
		streamOptions.Model = r.model(name, streamOptions.Model)
		if streamOptions.Model == "" {
			return nil, fmt.Errorf("no default model configured for provider %s", name)
		}
		if err := validateRequestBudget(name, streamMessages, streamOptions); err != nil {
			return nil, err
		}
		stream, err := r.streamWithRetry(ctx, name, streamMessages, streamOptions)
		if err != nil {
			return nil, err
		}
		return r.continueStream(ctx, name, streamMessages, streamOptions, stream), nil
	}

	var last error
	for _, candidate := range r.fallback {
		streamMessages, streamOptions := adaptRequest(candidate, messages, opts)
		streamOptions.Model = r.model(candidate, streamOptions.Model)
		if streamOptions.Model == "" {
			last = fmt.Errorf("no default model configured for provider %s", candidate)
			continue
		}
		if err := validateRequestBudget(candidate, streamMessages, streamOptions); err != nil {
			last = err
			if errors.Is(err, ErrRequestTooLarge) || errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) {
				continue
			}
			return nil, err
		}
		stream, err := r.streamWithRetry(ctx, candidate, streamMessages, streamOptions)
		if err == nil {
			return r.continueStream(ctx, candidate, streamMessages, streamOptions, stream), nil
		}
		if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) || errors.Is(err, ErrRequestTooLarge) {
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

func (r *Registry) streamWithRetry(ctx context.Context, name string, messages []Message, opts RequestOptions) (<-chan StreamChunk, error) {
	lock := r.providerLock(name)
	lock.Lock()
	defer lock.Unlock()

	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if err := r.wait(ctx, name); err != nil {
			return nil, err
		}
		provider, err := r.Get(name)
		if err != nil {
			return nil, err
		}
		stream, err := provider.Stream(ctx, messages, opts)
		r.observe(name, err)
		if err == nil {
			return stream, nil
		}
		last = err
		if !isRetryableProviderError(err) {
			return nil, err
		}
	}
	return nil, last
}

func (r *Registry) providerLock(name string) *sync.Mutex {
	r.limitMu.Lock()
	defer r.limitMu.Unlock()
	lock, ok := r.requestMu[name]
	if !ok {
		lock = &sync.Mutex{}
		r.requestMu[name] = lock
	}
	return lock
}

func isRetryableProviderError(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable)
}

func (r *Registry) model(name, requested string) string {
	if requested != "" {
		return requested
	}
	return r.models[name]
}

func (r *Registry) wait(ctx context.Context, name string) error {
	r.limitMu.Lock()
	readyAt := r.lastCall[name].Add(providerMinInterval(name))
	if r.retryUntil[name].After(readyAt) {
		readyAt = r.retryUntil[name]
	}
	r.limitMu.Unlock()

	waitFor := time.Until(readyAt)
	if waitFor <= 0 {
		return nil
	}

	timer := time.NewTimer(waitFor)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *Registry) observe(name string, err error) {
	r.limitMu.Lock()
	defer r.limitMu.Unlock()

	r.lastCall[name] = time.Now()
	if err == nil {
		r.attempts[name] = 0
		r.retryUntil[name] = time.Time{}
		return
	}

	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		return
	}

	switch providerErr.Kind {
	case ErrorRateLimited:
		r.attempts[name]++
		step := r.attempts[name]
		if step > 6 {
			step = 6
		}
		delay := time.Duration(providerErr.RetryAfter) * time.Second
		if delay <= 0 {
			delay = time.Duration(1<<uint(step-1)) * time.Second
		}
		if delay < providerMinInterval(name) {
			delay = providerMinInterval(name)
		}
		if delay > 120*time.Second {
			delay = 120 * time.Second
		}
		delay += time.Duration(time.Now().UnixNano()%1500) * time.Millisecond
		r.retryUntil[name] = time.Now().Add(delay)
	case ErrorProviderUnavailable, ErrorOverloaded:
		r.attempts[name]++
		step := r.attempts[name]
		if step > 5 {
			step = 5
		}
		delay := time.Duration(1<<uint(step-1)) * time.Second
		if delay < providerMinInterval(name) {
			delay = providerMinInterval(name)
		}
		delay += time.Duration(time.Now().UnixNano()%1200) * time.Millisecond
		r.retryUntil[name] = time.Now().Add(delay)
	default:
		r.attempts[name] = 0
	}
}

func providerMinInterval(name string) time.Duration {
	switch strings.ToLower(name) {
	case "groq":
		return 10 * time.Second
	case "gemini":
		return 7 * time.Second
	case "openai":
		return 4 * time.Second
	case "anthropic":
		return 4 * time.Second
	default:
		return 3 * time.Second
	}
}

func adaptRequest(name string, messages []Message, opts RequestOptions) ([]Message, RequestOptions) {
	budget := providerBudget(name)
	request := opts
	if request.MaxTokens <= 0 || request.MaxTokens > budget.maxOutputTokens {
		request.MaxTokens = budget.maxOutputTokens
	}
	if request.JSONMode && request.MaxTokens > budget.jsonOutputTokens {
		request.MaxTokens = budget.jsonOutputTokens
	}
	return trimProviderMessages(messages, budget.maxInputChars), request
}

type requestBudget struct {
	maxInputChars    int
	maxOutputTokens  int
	jsonOutputTokens int
}

func providerBudget(name string) requestBudget {
	switch strings.ToLower(name) {
	case "groq":
		return requestBudget{maxInputChars: 19000, maxOutputTokens: 3000, jsonOutputTokens: 1000}
	case "gemini":
		return requestBudget{maxInputChars: 12000, maxOutputTokens: 2600, jsonOutputTokens: 2000}
	case "openai":
		return requestBudget{maxInputChars: 24000, maxOutputTokens: 3500, jsonOutputTokens: 3000}
	case "anthropic":
		return requestBudget{maxInputChars: 24000, maxOutputTokens: 4000, jsonOutputTokens: 3500}
	default:
		return requestBudget{maxInputChars: 20000, maxOutputTokens: 3000, jsonOutputTokens: 2500}
	}
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

	first := messages[0]
	last := messages[len(messages)-1]
	if first.Role != "system" || last.Role != "user" {
		return messages
	}

	used := len(first.Content) + len(last.Content)
	result := []Message{first}
	for i := len(messages) - 2; i >= 1; i-- {
		remaining := maxChars - used
		if remaining <= 0 {
			break
		}
		if len(messages[i].Content) <= remaining {
			result = append([]Message{messages[i]}, result...)
			used += len(messages[i].Content)
		}
	}
	result = append(result, last)
	return result
}

func validateRequestBudget(name string, messages []Message, opts RequestOptions) error {
	return nil
}

func truncateMessage(message Message, maxChars int) Message {
	if maxChars <= 0 {
		return Message{Role: message.Role}
	}
	if len(message.Content) <= maxChars {
		return message
	}
	message.Content = message.Content[:maxChars]
	return message
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (r *Registry) continueStream(ctx context.Context, name string, messages []Message, opts RequestOptions, initial <-chan StreamChunk) <-chan StreamChunk {
	out := make(chan StreamChunk)

	go func() {
		defer close(out)

		current := initial
		combined := strings.Builder{}
		continuations := 0
		threshold := int(float64(opts.MaxTokens) * 3.2)
		if threshold < 1800 {
			threshold = 1800
		}

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
			if !finished || opts.JSONMode || continuations >= 2 || combined.Len() < threshold || !needsContinuation(partial) {
				out <- StreamChunk{Done: true}
				return
			}

			continuationMessages := make([]Message, 0, len(messages)+2)
			continuationMessages = append(continuationMessages, messages...)
			continuationMessages = append(continuationMessages, Message{Role: "assistant", Content: combined.String()})
			continuationMessages = append(continuationMessages, Message{Role: "user", Content: "Continue the previous response exactly where it stopped. Do not repeat any content already given. Finish the requested answer completely."})
			continuationMessages, continuationOptions := adaptRequest(name, continuationMessages, opts)
			if err := validateRequestBudget(name, continuationMessages, continuationOptions); err != nil {
				out <- StreamChunk{Error: err}
				return
			}

			next, err := r.streamWithRetry(ctx, name, continuationMessages, continuationOptions)
			if err != nil {
				out <- StreamChunk{Error: err}
				return
			}

			current = next
			continuations++
		}
	}()

	return out
}

func needsContinuation(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if strings.Count(text, "```")%2 != 0 {
		return true
	}
	last := text[len(text)-1]
	if strings.ContainsRune("([<{", rune(last)) {
		return true
	}
	if strings.HasSuffix(text, ":") || strings.HasSuffix(text, ",") || strings.HasSuffix(text, ";") || strings.HasSuffix(text, "-") {
		return true
	}
	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		return false
	}
	switch words[len(words)-1] {
	case "and", "or", "but", "because", "with", "to", "of", "for", "from", "into", "is", "are", "was", "were", "the", "a", "an", "that", "which", "then", "when", "while":
		return true
	default:
		return false
	}
}
