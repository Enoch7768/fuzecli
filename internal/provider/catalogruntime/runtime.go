package catalogruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

const (
	maxCompletionResponseBytes int64 = 16 << 20
	maxErrorResponseBytes      int64 = 1 << 20
	maxModelListResponseBytes  int64 = 4 << 20
)

type Provider struct {
	name, apiKey, baseURL, env string
	maxInput, maxOutput, jsonOutput int
	client *http.Client
}

func New(name, apiKey, baseURL, env string, maxInput, maxOutput, jsonOutput int) provider.Provider {
	if apiKey == "" {
		apiKey = os.Getenv(env)
	}
	return &Provider{
		name: name,
		apiKey: apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		env: env,
		maxInput: maxInput,
		maxOutput: maxOutput,
		jsonOutput: jsonOutput,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Streaming: true, StructuredJSON: true, ListModels: true,
		MaxInputChars: p.maxInput, MaxOutputTokens: p.maxOutput,
		JSONOutputTokens: p.jsonOutput,
	}
}

func (p *Provider) headers() map[string]string {
	headers := map[string]string{}
	if p.apiKey != "" {
		headers["Authorization"] = "Bearer " + p.apiKey
	}
	return headers
}

func (p *Provider) call(ctx context.Context, payload any) ([]byte, int, http.Header, error) {
	if p.baseURL == "" {
		return nil, 0, nil, fmt.Errorf("%s: base URL is not configured", p.name)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", strings.NewReader(string(data)))
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range p.headers() {
		req.Header.Set(key, value)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()

	limit := maxCompletionResponseBytes
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		limit = maxErrorResponseBytes
	}
	body, err := readLimitedBody(resp.Body, limit)
	if err != nil {
		return nil, resp.StatusCode, resp.Header.Clone(), fmt.Errorf("%s: could not read provider response: %w", p.name, err)
	}
	return body, resp.StatusCode, resp.Header.Clone(), nil
}

func (p *Provider) Send(ctx context.Context, messages []provider.Message, opts provider.RequestOptions) (*provider.Response, error) {
	model := strings.TrimSpace(opts.Model)
	if model == "" || strings.EqualFold(model, "auto") || strings.EqualFold(model, "default") {
		return nil, &provider.ProviderError{Kind: provider.ErrorModelNotFound, Provider: p.name, Message: "a concrete model is required"}
	}
	payload := map[string]any{
		"model": model, "messages": messages,
		"temperature": opts.Temperature, "max_tokens": opts.MaxTokens,
	}
	if opts.JSONMode {
		payload["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": "fuzecli_response", "strict": opts.JSONSchemaStrict,
				"schema": opts.JSONSchema,
			},
		}
	}
	body, status, headers, err := p.call(ctx, payload)
	if err != nil {
		return nil, &provider.ProviderError{Kind: provider.ErrorProviderUnavailable, Provider: p.name, Message: "request failed", Err: err}
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, classifyHTTPError(p.name, status, headers, body)
	}
	var response struct {
		Model string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage provider.Usage `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("%s: invalid completion response: %w", p.name, err)
	}
	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("%s: response contained no choices", p.name)
	}
	return &provider.Response{
		Content: response.Choices[0].Message.Content,
		Model: response.Model, ProviderName: p.name, Usage: response.Usage,
	}, nil
}

func (p *Provider) Stream(ctx context.Context, messages []provider.Message, opts provider.RequestOptions) (<-chan provider.StreamChunk, error) {
	if p.baseURL == "" {
		return nil, fmt.Errorf("%s: base URL is not configured", p.name)
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" || strings.EqualFold(model, "auto") || strings.EqualFold(model, "default") {
		return nil, &provider.ProviderError{Kind: provider.ErrorModelNotFound, Provider: p.name, Message: "a concrete model is required"}
	}
	payload := map[string]any{
		"model": model, "messages": messages, "temperature": opts.Temperature,
		"max_tokens": opts.MaxTokens, "stream": true,
	}
	if opts.JSONMode {
		payload["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": "fuzecli_response", "strict": opts.JSONSchemaStrict,
				"schema": opts.JSONSchema,
			},
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for key, value := range p.headers() {
		req.Header.Set(key, value)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, &provider.ProviderError{Kind: provider.ErrorProviderUnavailable, Provider: p.name, Message: "stream request failed", Err: err}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		return nil, readProviderError(p.name, resp)
	}

	out := make(chan provider.StreamChunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				sendStreamChunk(ctx, out, provider.StreamChunk{Done: true})
				return
			}
			var event struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				sendStreamChunk(ctx, out, provider.StreamChunk{Error: fmt.Errorf("%s: invalid stream event: %w", p.name, err)})
				return
			}
			for _, choice := range event.Choices {
				if choice.Delta.Content != "" && !sendStreamChunk(ctx, out, provider.StreamChunk{Delta: choice.Delta.Content}) {
					return
				}
			}
		}
		if err := scanner.Err(); err != nil {
			sendStreamChunk(ctx, out, provider.StreamChunk{Error: fmt.Errorf("%s: stream read failed: %w", p.name, err)})
			return
		}
		sendStreamChunk(ctx, out, provider.StreamChunk{Done: true})
	}()
	return out, nil
}

func sendStreamChunk(ctx context.Context, out chan<- provider.StreamChunk, chunk provider.StreamChunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

func (p *Provider) ListModels(ctx context.Context) ([]string, error) {
	if p.baseURL == "" {
		return nil, fmt.Errorf("%s: base URL is not configured", p.name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	for key, value := range p.headers() {
		req.Header.Set(key, value)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, &provider.ProviderError{Kind: provider.ErrorProviderUnavailable, Provider: p.name, Message: "model discovery request failed", Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, readProviderError(p.name, resp)
	}
	body, err := readLimitedBody(resp.Body, maxModelListResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("%s: could not read model discovery response: %w", p.name, err)
	}
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("%s: invalid model discovery response: %w", p.name, err)
	}
	models := make([]string, 0, len(response.Data))
	seen := make(map[string]bool, len(response.Data))
	for _, item := range response.Data {
		model := strings.TrimSpace(item.ID)
		if model == "" || strings.EqualFold(model, "default") || strings.EqualFold(model, "auto") || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	return models, nil
}

func readLimitedBody(body io.Reader, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("response body limit cannot be negative")
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response body exceeds the %d-byte limit", limit)
	}
	return data, nil
}

func readProviderError(name string, resp *http.Response) error {
	body, err := readLimitedBody(resp.Body, maxErrorResponseBytes)
	if err != nil {
		return &provider.ProviderError{
			Kind: provider.ErrorProviderUnavailable, Provider: name,
			StatusCode: resp.StatusCode, Message: "could not read provider error response", Err: err,
		}
	}
	return classifyHTTPError(name, resp.StatusCode, resp.Header, body)
}

func classifyHTTPError(name string, status int, headers http.Header, body []byte) *provider.ProviderError {
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(status)
	}
	if len(message) > 4096 {
		message = message[:4096]
	}
	kind := provider.ErrorBadRequest
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = provider.ErrorUnauthorized
	case http.StatusNotFound:
		kind = provider.ErrorModelNotFound
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		kind = provider.ErrorRateLimited
	case http.StatusConflict:
		kind = provider.ErrorOverloaded
	default:
		if status >= 500 {
			kind = provider.ErrorProviderUnavailable
		} else if status >= 400 {
			kind = provider.ErrorBadRequest
		}
	}
	retryAfter := 0
	if value := strings.TrimSpace(headers.Get("Retry-After")); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
			retryAfter = seconds
		}
	}
	return &provider.ProviderError{
		Kind: kind, Provider: name, StatusCode: status,
		Message: message, RetryAfter: retryAfter,
	}
}
