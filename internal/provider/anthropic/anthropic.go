package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

type Provider struct {
	APIKey, BaseURL string
	HTTPClient      *http.Client
}

func New(apiKey, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &Provider{APIKey: apiKey, BaseURL: strings.TrimRight(baseURL, "/"), HTTPClient: http.DefaultClient}
}
func (p *Provider) Name() string { return "anthropic" }

type req struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []provider.Message `json:"messages"`
	Temperature float64            `json:"temperature,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
}
type resp struct {
	Model   string `json:"model"`
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

func (p *Provider) headers() map[string]string {
	return map[string]string{"x-api-key": p.APIKey, "anthropic-version": "2023-06-01"}
}
func norm(m []provider.Message) (string, []provider.Message) {
	var sys string
	out := []provider.Message{}
	for _, x := range m {
		if x.Role == "system" {
			sys = x.Content
		} else {
			role := x.Role
			if role != "user" && role != "assistant" {
				role = "user"
			}
			out = append(out, provider.Message{Role: role, Content: x.Content})
		}
	}
	return sys, out
}
func (p *Provider) Send(ctx context.Context, m []provider.Message, o provider.RequestOptions) (*provider.Response, error) {
	sys, msgs := norm(m)
	var out resp
	err := provider.DoJSON(ctx, p.HTTPClient, http.MethodPost, p.BaseURL+"/messages", p.headers(), req{Model: o.Model, MaxTokens: o.MaxTokens, System: sys, Messages: msgs, Temperature: o.Temperature}, &out, p.Name())
	if err != nil {
		return nil, err
	}
	if len(out.Content) == 0 {
		return nil, fmt.Errorf("anthropic: response contained no content")
	}
	return &provider.Response{Content: out.Content[0].Text, Model: out.Model, ProviderName: p.Name()}, nil
}
func (p *Provider) Stream(ctx context.Context, m []provider.Message, o provider.RequestOptions) (<-chan provider.StreamChunk, error) {
	sys, msgs := norm(m)
	b, err := json.Marshal(req{Model: o.Model, MaxTokens: o.MaxTokens, System: sys, Messages: msgs, Temperature: o.Temperature, Stream: true})
	if err != nil {
		return nil, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/messages", strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	for k, v := range p.headers() {
		r.Header.Set(k, v)
	}
	res, err := p.HTTPClient.Do(r)
	if err != nil {
		return nil, &provider.ProviderError{Kind: provider.ErrorProviderUnavailable, Provider: p.Name(), Message: "request failed", Err: err}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, providerError(res, p.Name())
	}
	ch := make(chan provider.StreamChunk)
	go func() {
		defer close(ch)
		defer res.Body.Close()
		s := bufio.NewScanner(res.Body)
		s.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for s.Scan() {
			line := strings.TrimSpace(strings.TrimPrefix(s.Text(), "data:"))
			if line == "" {
				continue
			}
			var v struct {
				Type  string `json:"type"`
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal([]byte(line), &v) == nil && v.Delta.Text != "" {
				ch <- provider.StreamChunk{Delta: v.Delta.Text}
			}
			if v.Type == "message_stop" {
				ch <- provider.StreamChunk{Done: true}
				return
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
	err := provider.DoJSON(ctx, p.HTTPClient, http.MethodGet, p.BaseURL+"/models", p.headers(), nil, &out, p.Name())
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		models = append(models, m.ID)
	}
	return models, nil
}
func providerError(res *http.Response, name string) error {
	kind := provider.ErrorUnknown
	switch res.StatusCode {
	case 401, 403:
		kind = provider.ErrorUnauthorized
	case 429:
		kind = provider.ErrorRateLimited
	case 502, 503, 504:
		kind = provider.ErrorProviderUnavailable
	case 400, 422:
		kind = provider.ErrorBadRequest
	}
	return &provider.ProviderError{Kind: kind, Provider: name, StatusCode: res.StatusCode, Message: fmt.Sprintf("HTTP %d", res.StatusCode)}
}
