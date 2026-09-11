package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

type Provider struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

func New(apiKey, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &Provider{APIKey: apiKey, BaseURL: strings.TrimRight(baseURL, "/"), HTTPClient: http.DefaultClient}
}
func (p *Provider) Name() string { return "openai" }

type request struct {
	Model       string             `json:"model"`
	Messages    []provider.Message `json:"messages"`
	Temperature float64            `json:"temperature,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}
type response struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		Prompt     int `json:"prompt_tokens"`
		Completion int `json:"completion_tokens"`
		Total      int `json:"total_tokens"`
	} `json:"usage"`
}

func (p *Provider) Send(ctx context.Context, messages []provider.Message, opts provider.RequestOptions) (*provider.Response, error) {
	var out response
	err := provider.DoJSON(ctx, p.HTTPClient, http.MethodPost, p.BaseURL+"/chat/completions", map[string]string{"Authorization": "Bearer " + p.APIKey}, request{Model: opts.Model, Messages: messages, Temperature: opts.Temperature, MaxTokens: opts.MaxTokens}, &out, p.Name())
	if err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("openai: response contained no choices")
	}
	return &provider.Response{Content: out.Choices[0].Message.Content, Model: out.Model, ProviderName: p.Name(), Usage: provider.Usage{PromptTokens: out.Usage.Prompt, CompletionTokens: out.Usage.Completion, TotalTokens: out.Usage.Total}}, nil
}

func (p *Provider) Stream(ctx context.Context, messages []provider.Message, opts provider.RequestOptions) (<-chan provider.StreamChunk, error) {
	reqBody, err := json.Marshal(request{Model: opts.Model, Messages: messages, Temperature: opts.Temperature, MaxTokens: opts.MaxTokens, Stream: true})
	if err != nil {
		return nil, fmt.Errorf("openai: encode stream request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, &provider.ProviderError{Kind: provider.ErrorProviderUnavailable, Provider: p.Name(), Message: "request failed", Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, providerErrFromResponse(resp, p.Name())
	}
	ch := make(chan provider.StreamChunk)
	go func() {
		defer close(ch)
		streamErr := provider.ReadSSE(ctx, resp, func(data string) error {
			if data == "[DONE]" {
				ch <- provider.StreamChunk{Done: true}
				return nil
			}
			var v struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &v); err != nil {
				return fmt.Errorf("parse openai stream: %w", err)
			}
			if len(v.Choices) > 0 && v.Choices[0].Delta.Content != "" {
				ch <- provider.StreamChunk{Delta: v.Choices[0].Delta.Content}
			}
			return nil
		})
		if streamErr != nil {
			select {
			case ch <- provider.StreamChunk{Error: streamErr}:
			case <-ctx.Done():
			}
		}
	}()
	return ch, nil
}

func (p *Provider) ListModels(ctx context.Context) ([]string, error) {
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	err := provider.DoJSON(ctx, p.HTTPClient, http.MethodGet, p.BaseURL+"/models", map[string]string{"Authorization": "Bearer " + p.APIKey}, nil, &out, p.Name())
	if err != nil {
		return nil, err
	}
	r := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		r = append(r, m.ID)
	}
	return r, nil
}

func providerErrFromResponse(resp *http.Response, name string) error {
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	kind := provider.ErrorUnknown
	switch resp.StatusCode {
	case 401, 403:
		kind = provider.ErrorUnauthorized
	case 429:
		kind = provider.ErrorRateLimited
	case 502, 503, 504:
		kind = provider.ErrorProviderUnavailable
	case 400, 422:
		kind = provider.ErrorBadRequest
	}
	return &provider.ProviderError{Kind: kind, Provider: name, StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(b))}
}
