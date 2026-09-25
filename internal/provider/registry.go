package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
	"time"
)

var requestSequence atomic.Uint64

type Registry struct {
	providers  map[string]Provider
	fallback   []string
	models     map[string]string
	limitMu    sync.Mutex
	requestMu  map[string]*sync.Mutex
	lastCall   map[string]time.Time
	retryUntil map[string]time.Time
	attempts   map[string]int
	telemetry  *Telemetry
}

func NewRegistry(fallback []string, defaults map[string]string, providers ...Provider) *Registry {
	registered := map[string]Provider{}
	requestMu := map[string]*sync.Mutex{}
	for _, p := range providers {
		registered[p.Name()] = p
		requestMu[p.Name()] = &sync.Mutex{}
	}
	return &Registry{providers: registered, fallback: append([]string(nil), fallback...), models: defaults, requestMu: requestMu, lastCall: map[string]time.Time{}, retryUntil: map[string]time.Time{}, attempts: map[string]int{}, telemetry: NewTelemetry()}
}
func (r *Registry) Get(name string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q is not configured", name)
	}
	return p, nil
}

func (r *Registry) Capabilities(name string) (Capabilities, error) {
	p, err := r.Get(name)
	if err != nil {
		return Capabilities{}, err
	}
	return CapabilitiesOf(p), nil
}

func (r *Registry) ListModels(ctx context.Context, name string) ([]string, error) {
	p, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	return p.ListModels(ctx)
}

func (r *Registry) Send(ctx context.Context, name string, messages []Message, opts RequestOptions) (*Response, error) {
	if name != "auto" {
		requestMessages, request := adaptRequest(name, messages, opts)
		request.Model = r.resolveRequestModel(ctx, name, request.Model)
		if request.Model == "" {
			return nil, fmt.Errorf("no usable model is available for provider %s", name)
		}
		if capabilities, capabilityErr := r.Capabilities(name); capabilityErr != nil { return nil, capabilityErr } else if !SupportsRequest(capabilities, request) { return nil, fmt.Errorf("provider %s does not support the requested capabilities", name) }
		if err := validateRequestBudget(name, requestMessages, request); err != nil {
			return nil, err
		}
		response, err := r.sendWithRetry(ctx, name, requestMessages, request)
		if err == nil {
			return r.continueResponse(ctx, name, requestMessages, request, response)
		}
		if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) || errors.Is(err, ErrRequestTooLarge) || errors.Is(err, ErrModelNotFound) {
			for _, candidate := range r.fallback {
				if candidate == name {
					continue
				}
				fallbackMessages, fallbackRequest := adaptRequest(candidate, messages, opts)
				fallbackRequest.Model = r.resolveRequestModel(ctx, candidate, fallbackRequest.Model)
				if fallbackRequest.Model == "" {
					continue
				}
				if budgetErr := validateRequestBudget(candidate, fallbackMessages, fallbackRequest); budgetErr != nil {
					continue
				}
				fallbackResponse, fallbackErr := r.sendWithRetry(ctx, candidate, fallbackMessages, fallbackRequest)
				if fallbackErr == nil {
					return r.continueResponse(ctx, candidate, fallbackMessages, fallbackRequest, fallbackResponse)
				}
			}
		}
		return nil, err
	}
	routed, routeErr := r.Route(ctx, messages, RoutingRequest{Options: opts})
	candidates := r.fallback
	if routeErr == nil {
		ordered := make([]string, 0, len(routed.Candidates))
		for _, candidate := range routed.Candidates {
			ordered = append(ordered, candidate.Provider)
		}
		candidates = ordered
	}
	var last error
	for _, candidate := range candidates {
		requestMessages, request := adaptRequest(candidate, messages, opts)
		request.Model = r.resolveRequestModel(ctx, candidate, request.Model)
		if request.Model == "" {
			last = fmt.Errorf("no usable model is available for provider %s", candidate)
			continue
		}
		if err := validateRequestBudget(candidate, requestMessages, request); err != nil {
			last = err
			if errors.Is(err, ErrRequestTooLarge) || errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) || errors.Is(err, ErrModelNotFound) {
				continue
			}
			return nil, err
		}
		response, err := r.sendWithRetry(ctx, candidate, requestMessages, request)
		if err == nil {
			return r.continueResponse(ctx, candidate, requestMessages, request, response)
		}
		if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) || errors.Is(err, ErrRequestTooLarge) || errors.Is(err, ErrModelNotFound) {
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
	if strings.TrimSpace(opts.RequestID) == "" {
		opts.RequestID = fmt.Sprintf("%s-%d-%d", name, time.Now().UnixNano(), requestSequence.Add(1))
	}
	lock := r.providerLock(name)
	lock.Lock()
	defer lock.Unlock()
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if err := r.wait(ctx, name); err != nil {
			return nil, err
		}
		p, err := r.Get(name)
		if err != nil {
			return nil, err
		}
		started := time.Now()
		response, err := p.Send(ctx, messages, opts)
		if err != nil && opts.JSONMode && opts.JSONSchema != nil && isStructuredJSONCompatibilityError(err) {
			fallback := opts
			fallback.JSONSchemaStrict = false
			response, err = p.Send(ctx, messages, fallback)
			if err != nil && isStructuredJSONCompatibilityError(err) {
				objectMode := opts
				objectMode.JSONSchema = nil
				objectMode.JSONSchemaStrict = false
				response, err = p.Send(ctx, messages, objectMode)
			}
		}
		if err == nil && response != nil && strings.TrimSpace(response.Content) == "" {
			err = &ProviderError{Kind: ErrorProviderUnavailable, Provider: name, Message: "model returned an empty response"}
		}
		r.observe(name, err)
		var usage Usage
		if response != nil {
			usage = response.Usage
		}
		r.telemetry.RecordWithRequestID(opts.RequestID, name, err, usage, time.Since(started), false, opts.Model)
		if err == nil {
			return response, nil
		}
		last = err
		if isModelAvailabilityError(err) {
			if model := r.resolveModel(ctx, name, opts.Model); model != "" && model != opts.Model {
				opts.Model = model
				continue
			}
		}
		if !isRetryableProviderError(err) {
			return nil, err
		}
	}
	return nil, last
}

func (r *Registry) Stream(ctx context.Context, name string, messages []Message, opts RequestOptions) (<-chan StreamChunk, error) {
	if name != "auto" {
		streamMessages, streamOptions := adaptRequest(name, messages, opts)
		streamOptions.Model = r.resolveRequestModel(ctx, name, streamOptions.Model)
		if streamOptions.Model == "" {
			return nil, fmt.Errorf("no usable model is available for provider %s", name)
		}
		if capabilities, capabilityErr := r.Capabilities(name); capabilityErr != nil { return nil, capabilityErr } else if !capabilities.Streaming { return nil, fmt.Errorf("provider %s does not support streaming", name) } else if !SupportsRequest(capabilities, streamOptions) { return nil, fmt.Errorf("provider %s does not support the requested capabilities", name) }
		if err := validateRequestBudget(name, streamMessages, streamOptions); err != nil {
			return nil, err
		}
		stream, err := r.streamWithRetry(ctx, name, streamMessages, streamOptions)
		if err == nil {
			return r.continueStream(ctx, name, streamMessages, streamOptions, stream), nil
		}
		if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable) || errors.Is(err, ErrRequestTooLarge) || errors.Is(err, ErrModelNotFound) {
			for _, candidate := range r.fallback {
				if candidate == name {
					continue
				}
				fallbackMessages, fallbackOptions := adaptRequest(candidate, messages, opts)
				fallbackOptions.Model = r.resolveRequestModel(ctx, candidate, fallbackOptions.Model)
				if fallbackOptions.Model == "" {
					continue
				}
				if budgetErr := validateRequestBudget(candidate, fallbackMessages, fallbackOptions); budgetErr != nil {
					continue
				}
				fallbackStream, fallbackErr := r.streamWithRetry(ctx, candidate, fallbackMessages, fallbackOptions)
				if fallbackErr == nil {
					return r.continueStream(ctx, candidate, fallbackMessages, fallbackOptions, fallbackStream), nil
				}
			}
		}
		return nil, err
	}
	routed, routeErr := r.Route(ctx, messages, RoutingRequest{Options: opts, RequireStreaming: true})
	candidates := r.fallback
	if routeErr == nil {
		ordered := make([]string, 0, len(routed.Candidates))
		for _, candidate := range routed.Candidates {
			ordered = append(ordered, candidate.Provider)
		}
		candidates = ordered
	}
	var last error
	for _, candidate := range candidates {
		streamMessages, streamOptions := adaptRequest(candidate, messages, opts)
		streamOptions.Model = r.resolveRequestModel(ctx, candidate, streamOptions.Model)
		if streamOptions.Model == "" {
			last = fmt.Errorf("no usable model is available for provider %s", candidate)
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
	if strings.TrimSpace(opts.RequestID) == "" {
		opts.RequestID = fmt.Sprintf("%s-%d-%d", name, time.Now().UnixNano(), requestSequence.Add(1))
	}
	lock := r.providerLock(name)
	lock.Lock()
	defer lock.Unlock()
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if err := r.wait(ctx, name); err != nil {
			return nil, err
		}
		p, err := r.Get(name)
		if err != nil {
			return nil, err
		}
		started := time.Now()
		stream, err := p.Stream(ctx, messages, opts)
		if err != nil && opts.JSONMode && opts.JSONSchema != nil && isStructuredJSONCompatibilityError(err) {
			fallback := opts
			fallback.JSONSchemaStrict = false
			stream, err = p.Stream(ctx, messages, fallback)
			if err != nil && isStructuredJSONCompatibilityError(err) {
				objectMode := opts
				objectMode.JSONSchema = nil
				objectMode.JSONSchemaStrict = false
				stream, err = p.Stream(ctx, messages, objectMode)
			}
		}
		r.observe(name, err)
		if err != nil {
			r.telemetry.RecordWithRequestID(opts.RequestID, name, err, Usage{}, time.Since(started), true, opts.Model)
		}
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

func isStructuredJSONCompatibilityError(err error) bool {
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != ErrorBadRequest {
		return false
	}
	message := strings.ToLower(providerErr.Error())
	for _, marker := range []string{"schema", "json_schema", "json schema", "response_format", "responsejsonschema", "response json schema", "structured output", "structured outputs", "unsupported", "not supported", "invalid parameter", "unknown parameter", "unrecognized parameter"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func isRetryableProviderError(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrProviderUnavailable)
}

func isModelAvailabilityError(err error) bool {
	if errors.Is(err, ErrModelNotFound) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"invalid model", "model not found", "model is not available", "not available in your subscription", "subscription tier", "does not have access to model", "unknown model"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func (r *Registry) resolveRequestModel(ctx context.Context, name, requested string) string {
	model := strings.TrimSpace(requested)
	if model != "" && !strings.EqualFold(model, "auto") && !strings.EqualFold(model, "default") {
		return model
	}
	if configured := strings.TrimSpace(r.models[name]); configured != "" &&
		!strings.EqualFold(configured, "auto") &&
		!strings.EqualFold(configured, "default") {
		return configured
	}
	return r.resolveModel(ctx, name, model)
}

func (r *Registry) resolveModel(ctx context.Context, name, current string) string {
	models, err := r.ListModels(ctx, name)
	if err != nil {
		return ""
	}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || strings.EqualFold(model, "default") || strings.EqualFold(model, "auto") {
			continue
		}
		return model
	}
	return ""
}

func (r *Registry) model(name, requested string) string {
	if requested != "" {
		return requested
	}
	return r.models[name]
}

func (r *Registry) DefaultModel(name string) string {
	return r.model(name, "")
}

func (r *Registry) Telemetry() TelemetrySnapshot {
	return r.telemetry.Snapshot()
}

func (r *Registry) ProviderTelemetry(name string) (ProviderTelemetry, bool) {
	return r.telemetry.Provider(name)
}

func (r *Registry) ResetTelemetry() {
	r.telemetry.Reset()
}

func (r *Registry) wait(ctx context.Context, name string) error {
	r.limitMu.Lock()
	readyAt := r.lastCall[name].Add(ProviderPolicy(name).MinInterval)
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
		if delay < ProviderPolicy(name).MinInterval {
			delay = ProviderPolicy(name).MinInterval
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
		if delay < ProviderPolicy(name).MinInterval {
			delay = ProviderPolicy(name).MinInterval
		}
		delay += time.Duration(time.Now().UnixNano()%1200) * time.Millisecond
		r.retryUntil[name] = time.Now().Add(delay)
	default:
		r.attempts[name] = 0
	}
}

func adaptRequest(name string, messages []Message, opts RequestOptions) ([]Message, RequestOptions) {
	budget := ProviderPolicy(name)
	request := opts
	inputChars := budget.MaxInputChars
	if strings.EqualFold(strings.TrimSpace(request.BillingMode), "free") {
		inputChars = minInt(inputChars, 16000)
		if request.MaxTokens <= 0 || request.MaxTokens > 5000 {
			request.MaxTokens = 5000
		}
	} else if request.MaxTokens <= 0 {
		request.MaxTokens = budget.MaxOutputTokens
	}
	if request.MaxTokens > budget.MaxOutputTokens {
		request.MaxTokens = budget.MaxOutputTokens
	}
	if request.JSONMode && request.JSONSchema != nil {
		request.JSONSchemaStrict = true
	}
	if request.JSONMode && request.MaxTokens > budget.JSONOutputTokens {
		request.MaxTokens = budget.JSONOutputTokens
	}
	if request.RequestTokenLimit <= 0 {
		request.RequestTokenLimit = inputChars / 3
	}
	messages = trimProviderMessages(messages, inputChars)
	messages = trimToRequestTokenBudget(messages, request.RequestTokenLimit)
	return messages, request
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func estimateMessageTokens(messages []Message) int {
	chars := 0
	for _, message := range messages {
		chars += len(message.Role) + len(message.Content) + 16
	}
	if chars == 0 {
		return 1
	}
	return (chars + 2) / 3
}

func trimToRequestTokenBudget(messages []Message, limit int) []Message {
	if limit <= 0 || estimateMessageTokens(messages)+256 <= limit {
		return messages
	}
	target := limit - 256
	if target < 1000 {
		target = 1000
	}
	return trimProviderMessages(messages, target*3)
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
		return trimHistory(messages, maxChars)
	}
	if strings.Contains(first.Content, "--- ") {
		first = trimWorkspaceMessage(first, last.Content, maxChars)
	}
	result := make([]Message, 0, len(messages))
	result = append(result, first)
	for i := 1; i < len(messages)-1; i++ {
		result = append(result, messages[i])
	}
	result = append(result, last)
	return trimHistory(result, maxChars)
}

func trimHistory(messages []Message, maxChars int) []Message {
	if len(messages) == 0 {
		return messages
	}
	first := messages[0]
	last := messages[len(messages)-1]
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

type workspaceBlock struct {
	path  string
	body  string
	score int
}

func parseWorkspaceBlocks(content string) (string, []workspaceBlock) {
	lines := strings.Split(content, "\n")
	prefixLines := make([]string, 0)
	blocks := make([]workspaceBlock, 0)
	current := -1
	var body strings.Builder
	flush := func() {
		if current < 0 {
			return
		}
		blocks[current].body = body.String()
		body.Reset()
		current = -1
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--- ") && strings.HasSuffix(trimmed, " ---") {
			flush()
			path := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "--- "), " ---"))
			if path != "" {
				blocks = append(blocks, workspaceBlock{path: path})
				current = len(blocks) - 1
				continue
			}
		}
		if current < 0 {
			prefixLines = append(prefixLines, line)
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	flush()
	return strings.Join(prefixLines, "\n"), blocks
}

func trimWorkspaceMessage(message Message, prompt string, maxChars int) Message {
	prefix, blocks := parseWorkspaceBlocks(message.Content)
	if len(blocks) == 0 {
		return truncateMessage(message, maxChars/2)
	}
	terms := contextTerms(prompt)
	for i := range blocks {
		blocks[i].score = pathScore(blocks[i].path, terms)
		lowerBody := strings.ToLower(blocks[i].body)
		for _, term := range terms {
			if strings.Contains(lowerBody, term) {
				blocks[i].score++
			}
		}
	}
	sort.SliceStable(blocks, func(i, j int) bool {
		if blocks[i].score == blocks[j].score {
			return blocks[i].path < blocks[j].path
		}
		return blocks[i].score > blocks[j].score
	})
	manifest := prefix + "Workspace files available locally. The complete repository remains on disk; relevant files are loaded into each request.\n"
	for _, item := range blocks {
		manifest += "- " + item.path + "\n"
		if len(manifest) >= maxChars/3 {
			break
		}
	}
	if lowContextPrompt(prompt) {
		return Message{Role: message.Role, Content: truncateString(manifest, maxChars-len(prompt)-32)}
	}
	if len(manifest)+len(prompt)+512 >= maxChars {
		return Message{Role: message.Role, Content: truncateString(manifest, maxChars-len(prompt)-32)}
	}
	budget := maxChars - len(manifest) - len(prompt) - 256
	if budget < 1200 {
		return Message{Role: message.Role, Content: truncateString(manifest, maxChars-len(prompt)-32)}
	}
	var b strings.Builder
	b.WriteString(manifest)
	for _, item := range blocks {
		candidate := fmt.Sprintf("\n--- %s ---\n%s\n", item.path, item.body)
		if len(candidate) > budget {
			continue
		}
		b.WriteString(candidate)
		budget -= len(candidate)
		if budget < 1200 {
			break
		}
	}
	return Message{Role: message.Role, Content: truncateString(b.String(), maxChars-len(prompt)-32)}
}

func lowContextPrompt(prompt string) bool {
	text := strings.ToLower(strings.TrimSpace(prompt))
	if text == "" {
		return true
	}
	for _, term := range []string{"change", "modify", "fix", "update", "create", "add", "remove", "delete", "write", "build", "code", "file", "project", "website", "app", "application", "analyze", "debug", "refactor", "implement", "implementing"} {
		if strings.Contains(text, term) {
			return false
		}
	}
	return len(strings.Fields(text)) <= 6
}

func contextTerms(prompt string) []string {
	words := strings.Fields(strings.ToLower(prompt))
	seen := map[string]bool{}
	terms := make([]string, 0, len(words))
	for _, word := range words {
		word = strings.Trim(word, ".,:;!?()[]{}\"'`<>/\\")
		if len(word) < 2 || len(word) > 80 || seen[word] {
			continue
		}
		seen[word] = true
		terms = append(terms, word)
	}
	return terms
}

func pathScore(path string, terms []string) int {
	lower := strings.ToLower(path)
	score := 0
	for _, term := range terms {
		if strings.Contains(lower, term) {
			score += 10
		}
	}
	for _, marker := range []string{"main", "app", "index", "readme", "config", "route", "server", "package"} {
		if strings.Contains(lower, marker) {
			for _, term := range terms {
				if term == marker {
					score += 5
				}
			}
		}
	}
	return score
}

func truncateString(value string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(value) <= max {
		return value
	}
	end := max
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end]
}

func validateRequestBudget(name string, messages []Message, opts RequestOptions) error {
	policy := ProviderPolicy(name)
	if policy.MaxInputChars > 0 {
		total := 0
		for _, message := range messages {
			total += len(message.Content)
		}
		if total > policy.MaxInputChars {
			return fmt.Errorf("%w: %s request is %d characters but the provider budget is %d", ErrRequestTooLarge, name, total, policy.MaxInputChars)
		}
	}
	if opts.RequestTokenLimit > 0 && estimateMessageTokens(messages) > opts.RequestTokenLimit {
		return fmt.Errorf("%w: %s request is estimated at %d tokens but the request budget is %d", ErrRequestTooLarge, name, estimateMessageTokens(messages), opts.RequestTokenLimit)
	}
	return nil
}

func truncateMessage(message Message, maxChars int) Message {
	if maxChars <= 0 {
		return Message{Role: message.Role}
	}
	if len(message.Content) <= maxChars {
		return message
	}
	message.Content = truncateString(message.Content, maxChars)
	return message
}


func continuationRequest(name string, original []Message, combined, instruction string, opts RequestOptions) ([]Message, RequestOptions) {
	out := make([]Message, 0, 4)
	if len(original) > 0 && original[0].Role == "system" {
		out = append(out, original[0])
	}
	for i := len(original) - 1; i >= 0; i-- {
		if original[i].Role == "user" {
			out = append(out, original[i])
			break
		}
	}
	out = append(out, Message{Role: "assistant", Content: combined})
	out = append(out, Message{Role: "user", Content: instruction})
	request := opts
	if request.JSONMode {
		request.JSONSchema = nil
		request.JSONSchemaStrict = false
	}
	policy := ProviderPolicy(name)
	request.RequestTokenLimit = estimateMessageTokens(out) + 256
	if request.MaxTokens <= 0 || request.MaxTokens > policy.MaxOutputTokens {
		request.MaxTokens = policy.MaxOutputTokens
	}
	if request.JSONMode && request.MaxTokens > policy.JSONOutputTokens {
		request.MaxTokens = policy.JSONOutputTokens
	}
	return out, request
}

func (r *Registry) continueStream(ctx context.Context, name string, messages []Message, opts RequestOptions, initial <-chan StreamChunk) <-chan StreamChunk {
	out := make(chan StreamChunk)
	go func() {
		defer close(out)
		current := initial
		combined := strings.Builder{}
		continuations := 0
		for {
			finished := false
			var usage Usage
			for chunk := range current {
				if chunk.Error != nil {
					r.telemetry.RecordWithRequestID(opts.RequestID, name, chunk.Error, usage, 0, true, opts.Model)
					out <- chunk
					return
				}
				usage.PromptTokens += chunk.Usage.PromptTokens
				usage.CompletionTokens += chunk.Usage.CompletionTokens
				usage.TotalTokens += chunk.Usage.TotalTokens
				if chunk.Delta != "" {
					combined.WriteString(chunk.Delta)
					out <- StreamChunk{Delta: chunk.Delta, Usage: chunk.Usage}
				}
				if chunk.Done {
					finished = true
				}
			}
			partial := strings.TrimSpace(combined.String())
			if !finished || continuations >= 8 || !responseNeedsContinuation(partial) {
				r.telemetry.RecordWithRequestID(opts.RequestID, name, nil, usage, 0, true, opts.Model)
				out <- StreamChunk{Done: true, Usage: usage}
				return
			}
			continuationMessages, continuationOptions := continuationRequest(name, messages, combined.String(), "Continue the previous response exactly where it stopped. Do not repeat any content already given. Finish the requested answer completely.", opts)
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

func (r *Registry) continueResponse(ctx context.Context, name string, messages []Message, opts RequestOptions, response *Response) (*Response, error) {
	if response == nil || !responseNeedsContinuation(response.Content) {
		return response, nil
	}
	combined := response.Content
	for attempt := 0; attempt < 8; attempt++ {
		continuationMessages, continuationOptions := continuationRequest(name, messages, combined, "Continue the previous response exactly where it stopped. Do not repeat any content already given. Return only the missing continuation and finish the response completely. If the response is structured JSON, continue until the JSON object is complete and valid.", opts)
		next, err := r.sendWithRetry(ctx, name, continuationMessages, continuationOptions)
		if err != nil {
			return nil, err
		}
		if next == nil || strings.TrimSpace(next.Content) == "" {
			break
		}
		combined += next.Content
		response.Content = combined
		if next.Model != "" {
			response.Model = next.Model
		}
		if next.ProviderName != "" {
			response.ProviderName = next.ProviderName
		}
		response.Usage.PromptTokens += next.Usage.PromptTokens
		response.Usage.CompletionTokens += next.Usage.CompletionTokens
		response.Usage.TotalTokens += next.Usage.TotalTokens
		if !responseNeedsContinuation(combined) {
			return response, nil
		}
	}
	return response, nil
}

func responseNeedsContinuation(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		return !jsonDocumentComplete(text)
	}
	return needsContinuation(text)
}

func jsonDocumentComplete(text string) bool {
	start := strings.IndexAny(text, "{[")
	if start < 0 {
		return false
	}
	stack := make([]byte, 0, 16)
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		c := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		switch c {
		case '{', '[':
			stack = append(stack, c)
		case '}', ']':
			if len(stack) == 0 {
				return false
			}
			open := stack[len(stack)-1]
			if (open == '{' && c != '}') || (open == '[' && c != ']') {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}
	if inString || len(stack) != 0 {
		return false
	}
	var value any
	if err := json.Unmarshal([]byte(text[start:]), &value); err != nil {
		return false
	}
	return true
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
