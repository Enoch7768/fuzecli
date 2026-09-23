package groq

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

type Provider struct {
	APIKey string
	BaseURL string
	HTTPClient *http.Client
}

const (
	defaultBaseURL = "https://api.groq.com/openai/v1"
	requestTokenBudget = 7600
	maxOutputTokens = 8192
	jsonOutputTokenBudget = 8192
)

func New(apiKey, baseURL string) *Provider {
	if baseURL == "" { baseURL = defaultBaseURL }
	return &Provider{APIKey: apiKey, BaseURL: strings.TrimRight(baseURL, "/"), HTTPClient: &http.Client{Timeout: 120 * time.Second}}
}

func (p *Provider) Name() string { return "groq" }

type chatRequest struct {
	Model string `json:"model"`
	Messages []provider.Message `json:"messages"`
	Temperature float64 `json:"temperature,omitempty"`
	MaxCompletionTokens int `json:"max_completion_tokens,omitempty"`
	Stream bool `json:"stream,omitempty"`
	ResponseFormat any `json:"response_format,omitempty"`
}

type chatResponse struct {
	Model string `json:"model"`
	Choices []struct {
		Message struct { Content string `json:"content"` } `json:"message"`
	} `json:"choices"`
	Usage struct {
		Prompt int `json:"prompt_tokens"`
		Completion int `json:"completion_tokens"`
		Total int `json:"total_tokens"`
	} `json:"usage"`
}

func (p *Provider) Send(ctx context.Context, messages []provider.Message, opts provider.RequestOptions) (*provider.Response, error) {
	opts = p.adapt(messages, opts)
	var out chatResponse
	err := provider.DoJSON(ctx, p.HTTPClient, http.MethodPost, p.BaseURL+"/chat/completions", p.headers(), chatRequest{Model: opts.Model, Messages: messages, Temperature: opts.Temperature, MaxCompletionTokens: opts.MaxTokens, ResponseFormat: p.responseFormat(opts)}, &out, p.Name())
	if err != nil { return nil, err }
	if len(out.Choices) == 0 { return nil, fmt.Errorf("groq: response contained no choices") }
	return &provider.Response{Content: out.Choices[0].Message.Content, Model: out.Model, ProviderName: p.Name(), Usage: provider.Usage{PromptTokens: out.Usage.Prompt, CompletionTokens: out.Usage.Completion, TotalTokens: out.Usage.Total}}, nil
}

func (p *Provider) Stream(ctx context.Context, messages []provider.Message, opts provider.RequestOptions) (<-chan provider.StreamChunk, error) {
	opts = p.adapt(messages, opts)
	body, err := json.Marshal(chatRequest{Model: opts.Model, Messages: messages, Temperature: opts.Temperature, MaxCompletionTokens: opts.MaxTokens, ResponseFormat: p.responseFormat(opts), Stream: true})
	if err != nil { return nil, fmt.Errorf("groq: encode stream request: %w", err) }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", strings.NewReader(string(body)))
	if err != nil { return nil, err }
	for key, value := range p.headers() { req.Header.Set(key, value) }
	resp, err := p.HTTPClient.Do(req)
	if err != nil { return nil, &provider.ProviderError{Kind: provider.ErrorProviderUnavailable, Provider: p.Name(), Message: "request failed", Err: err} }
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { err := provider.ParseHTTPResponseError(resp, p.Name()); _ = resp.Body.Close(); return nil, err }
	ch := make(chan provider.StreamChunk)
	go func() {
		defer close(ch); defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body); scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") { continue }
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" { ch <- provider.StreamChunk{Done: true}; return }
			var event struct { Choices []struct { Delta struct { Content string `json:"content"` } `json:"delta"` } `json:"choices"` }
			if err := json.Unmarshal([]byte(data), &event); err != nil { ch <- provider.StreamChunk{Error: fmt.Errorf("groq: parse stream event: %w", err)}; return }
			if len(event.Choices) > 0 && event.Choices[0].Delta.Content != "" { ch <- provider.StreamChunk{Delta: event.Choices[0].Delta.Content} }
		}
		if err := scanner.Err(); err != nil { ch <- provider.StreamChunk{Error: fmt.Errorf("groq: stream read failed: %w", err)}; return }
		ch <- provider.StreamChunk{Done: true}
	}()
	return ch, nil
}

func (p *Provider) ListModels(ctx context.Context) ([]string, error) {
	var out struct { Data []struct { ID string `json:"id"` } `json:"data"` }
	if err := provider.DoJSON(ctx, p.HTTPClient, http.MethodGet, p.BaseURL+"/models", p.headers(), nil, &out, p.Name()); err != nil { return nil, err }
	models := make([]string, 0, len(out.Data)); for _, model := range out.Data { if model.ID != "" { models = append(models, model.ID) } }; return models, nil
}

func (p *Provider) headers() map[string]string { return map[string]string{"Authorization": "Bearer "+p.APIKey, "Content-Type": "application/json"} }

func (p *Provider) responseFormat(opts provider.RequestOptions) any {
	if !opts.JSONMode { return nil }
	if opts.JSONSchema == nil { return map[string]any{"type": "json_object"} }
	return map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "fuzecli_response", "strict": true, "schema": sanitizeSchema(opts.JSONSchema)}}
}

func sanitizeSchema(value any) any {
	switch schema := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(schema)); for key, item := range schema { if key != "propertyOrdering" { out[key] = sanitizeSchema(item) } }; return out
	case []any:
		out := make([]any, len(schema)); for i, item := range schema { out[i] = sanitizeSchema(item) }; return out
	default: return value
	}
}

func (p *Provider) adapt(messages []provider.Message, opts provider.RequestOptions) provider.RequestOptions {
	if opts.MaxTokens <= 0 || opts.MaxTokens > maxOutputTokens { opts.MaxTokens = maxOutputTokens }
	if opts.JSONMode && opts.MaxTokens > jsonOutputTokenBudget { opts.MaxTokens = jsonOutputTokenBudget }
	if opts.RequestTokenLimit <= 0 { opts.RequestTokenLimit = requestTokenBudget }
	inputTokens := estimateTokens(messages); available := opts.RequestTokenLimit - inputTokens - 256
	if available > 0 && opts.MaxTokens > available { opts.MaxTokens = available }
	return opts
}

func estimateTokens(messages []provider.Message) int {
	chars := 0; for _, message := range messages { chars += len(message.Role)+len(message.Content)+16 }; if chars == 0 { return 1 }; return (chars+2)/3
}
