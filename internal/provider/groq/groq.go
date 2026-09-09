package groq

import (
	"context"
	"fuzecli/internal/provider"
	"fuzecli/internal/provider/openai"
	"net/http"
	"strings"
)

type Provider struct{ inner *openai.Provider }

func New(apiKey, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "https://api.groq.com/openai/v1"
	}
	return &Provider{inner: openai.New(apiKey, baseURL)}
}
func (p *Provider) Name() string { return "groq" }
func (p *Provider) Send(ctx context.Context, m []provider.Message, o provider.RequestOptions) (*provider.Response, error) {
	r, e := p.inner.Send(ctx, m, o)
	if r != nil {
		r.ProviderName = p.Name()
	}
	return r, e
}
func (p *Provider) Stream(ctx context.Context, m []provider.Message, o provider.RequestOptions) (<-chan provider.StreamChunk, error) {
	return p.inner.Stream(ctx, m, o)
}
func (p *Provider) ListModels(ctx context.Context) ([]string, error) { return p.inner.ListModels(ctx) }

var _ = http.MethodGet
var _ = strings.TrimSpace
