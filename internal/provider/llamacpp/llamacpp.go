package llamacpp

import (
	"context"
	"fuzecli/internal/provider"
	"fuzecli/internal/provider/openai"
	"strings"
)

type Provider struct{ inner *openai.Provider }

func New(baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	return &Provider{inner: openai.New("", strings.TrimRight(baseURL, "/")+"/v1")}
}
func (p *Provider) Name() string { return "llamacpp" }
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
