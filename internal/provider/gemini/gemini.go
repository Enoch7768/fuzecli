package gemini

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"fuzecli/internal/provider"
)

type Provider struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

func New(apiKey, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &Provider{APIKey: apiKey, BaseURL: strings.TrimRight(baseURL, "/"), HTTPClient: http.DefaultClient}
}
func (p *Provider) Name() string { return "gemini" }

type part struct {
	Text string `json:"text,omitempty"`
}
type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}
type req struct {
	Contents          []content      `json:"contents"`
	SystemInstruction *content       `json:"systemInstruction,omitempty"`
	GenerationConfig  map[string]any `json:"generationConfig,omitempty"`
}
type resp struct {
	Candidates []struct {
		Content content `json:"content"`
	} `json:"candidates"`
}

func convert(messages []provider.Message) ([]content, *content) {
	var sys *content
	var out []content
	for _, m := range messages {
		if m.Role == "system" {
			v := content{Role: "user", Parts: []part{{Text: m.Content}}}
			sys = &v
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		out = append(out, content{Role: role, Parts: []part{{Text: m.Content}}})
	}
	return out, sys
}

func (p *Provider) Send(ctx context.Context, m []provider.Message, o provider.RequestOptions) (*provider.Response, error) {
	cs, sys := convert(m)
	payload := req{Contents: cs, SystemInstruction: sys, GenerationConfig: map[string]any{"temperature": o.Temperature, "maxOutputTokens": o.MaxTokens}}
	var out resp
	err := provider.DoJSON(ctx, p.HTTPClient, http.MethodPost, p.BaseURL+"/models/"+o.Model+":generateContent", map[string]string{"x-goog-api-key": p.APIKey}, payload, &out, p.Name())
	if err != nil {
		return nil, err
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini: response contained no candidates")
	}
	return &provider.Response{Content: out.Candidates[0].Content.Parts[0].Text, Model: o.Model, ProviderName: p.Name()}, nil
}

func (p *Provider) Stream(ctx context.Context, m []provider.Message, o provider.RequestOptions) (<-chan provider.StreamChunk, error) {
	cs, sys := convert(m)
	payload := req{Contents: cs, SystemInstruction: sys, GenerationConfig: map[string]any{"temperature": o.Temperature, "maxOutputTokens": o.MaxTokens}}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode gemini stream request: %w", err)
	}
	reqq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/models/"+o.Model+":streamGenerateContent?alt=sse", strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	reqq.Header.Set("Content-Type", "application/json")
	reqq.Header.Set("x-goog-api-key", p.APIKey)
	r, err := p.HTTPClient.Do(reqq)
	if err != nil {
		return nil, &provider.ProviderError{Kind: provider.ErrorProviderUnavailable, Provider: p.Name(), Message: "request failed", Err: err}
	}
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return nil, fmt.Errorf("gemini stream failed with HTTP %d", r.StatusCode)
	}
	ch := make(chan provider.StreamChunk)
	go func() {
		defer close(ch)
		defer r.Body.Close()
		s := bufio.NewScanner(r.Body)
		s.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for s.Scan() {
			line := strings.TrimSpace(strings.TrimPrefix(s.Text(), "data:"))
			if line == "" {
				continue
			}
			var v resp
			if json.Unmarshal([]byte(line), &v) == nil && len(v.Candidates) > 0 && len(v.Candidates[0].Content.Parts) > 0 {
				ch <- provider.StreamChunk{Delta: v.Candidates[0].Content.Parts[0].Text}
			}
		}
	}()
	return ch, nil
}

func (p *Provider) ListModels(ctx context.Context) ([]string, error) {
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	err := provider.DoJSON(ctx, p.HTTPClient, http.MethodGet, p.BaseURL+"/models", map[string]string{"x-goog-api-key": p.APIKey}, nil, &out, p.Name())
	if err != nil {
		return nil, err
	}
	r := []string{}
	for _, m := range out.Models {
		r = append(r, strings.TrimPrefix(m.Name, "models/"))
	}
	return r, nil
}
