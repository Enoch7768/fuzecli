package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type compatibleProvider struct {
	name    string
	apiKey  string
	baseURL string
	http    *http.Client
	env     string
}

func newCompatibleProvider(spec compatibleSpec, cfg configuredProvider) *compatibleProvider {
	return &compatibleProvider{
		name:    spec.Name,
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(firstNonEmpty(cfg.BaseURL, spec.BaseURL), "/"),
		http:    http.DefaultClient,
		env:     spec.EnvKey,
	}
}

func (p *compatibleProvider) Name() string { return p.name }

type compatibleRequest struct {
	Model          string    `json:"model"`
	Messages       []Message `json:"messages"`
	Temperature    float64   `json:"temperature,omitempty"`
	MaxTokens      int       `json:"max_tokens,omitempty"`
	Stream         bool      `json:"stream,omitempty"`
	ResponseFormat any       `json:"response_format,omitempty"`
}

type compatibleResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

func (p *compatibleProvider) Send(ctx context.Context, messages []Message, opts RequestOptions) (*Response, error) {
	model := opts.Model
	if model == "" || strings.EqualFold(model, "auto") {
		models, err := p.ListModels(ctx)
		if err != nil {
			return nil, err
		}
		if len(models) == 0 {
			return nil, fmt.Errorf("%s: no model available; configure default_model", p.name)
		}
		model = models[0]
	}

	req := compatibleRequest{Model: model, Messages: messages, Temperature: opts.Temperature, MaxTokens: opts.MaxTokens}
	if opts.JSONMode {
		req.ResponseFormat = map[string]any{"type": "json_object"}
	}

	var out compatibleResponse
	headers := p.headers()
	if err := DoJSON(ctx, p.http, http.MethodPost, p.baseURL+"/chat/completions", headers, req, &out, p.name); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("%s: response contained no choices", p.name)
	}
	return &Response{Content: out.Choices[0].Message.Content, Model: firstNonEmpty(out.Model, model), ProviderName: p.name, Usage: out.Usage}, nil
}

func (p *compatibleProvider) Stream(ctx context.Context, messages []Message, opts RequestOptions) (<-chan StreamChunk, error) {
	model := opts.Model
	if model == "" || strings.EqualFold(model, "auto") {
		models, err := p.ListModels(ctx)
		if err != nil {
			return nil, err
		}
		if len(models) == 0 {
			return nil, fmt.Errorf("%s: no model available; configure default_model", p.name)
		}
		model = models[0]
	}

	body := compatibleRequest{Model: model, Messages: messages, Temperature: opts.Temperature, MaxTokens: opts.MaxTokens, Stream: true}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range p.headers() {
		req.Header.Set(k, v)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, &ProviderError{Kind: ErrorProviderUnavailable, Provider: p.name, Message: "request failed", Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ParseHTTPResponseError(resp, p.name)
	}

	out := make(chan StreamChunk)
	go func() {
		defer close(out)
		err := ReadSSE(ctx, resp, func(data string) error {
			if data == "[DONE]" {
				out <- StreamChunk{Done: true}
				return nil
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				return err
			}
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				out <- StreamChunk{Delta: chunk.Choices[0].Delta.Content}
			}
			return nil
		})
		if err != nil {
			out <- StreamChunk{Error: err}
			return
		}
		out <- StreamChunk{Done: true}
	}()
	return out, nil
}

func (p *compatibleProvider) ListModels(ctx context.Context) ([]string, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := DoJSON(ctx, p.http, http.MethodGet, p.baseURL+"/models", p.headers(), nil, &out, p.name); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(out.Data))
	for _, item := range out.Data {
		if item.ID != "" {
			models = append(models, item.ID)
		}
	}
	return models, nil
}

func (p *compatibleProvider) headers() map[string]string {
	headers := map[string]string{}
	if p.apiKey != "" {
		headers["Authorization"] = "Bearer " + p.apiKey
	}
	return headers
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
