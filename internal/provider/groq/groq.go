package groq

import (
	"context"

	"github.com/Enoch7768/fuzecli/internal/provider"
	"github.com/Enoch7768/fuzecli/internal/provider/openai"
)

type Provider struct{ inner *openai.Provider }

const safeRequestTokenBudget = 7600

func New(apiKey, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "https://api.groq.com/openai/v1"
	}
	return &Provider{inner: openai.New(apiKey, baseURL)}
}

func (p *Provider) Name() string { return "groq" }

func (p *Provider) Send(ctx context.Context, m []provider.Message, o provider.RequestOptions) (*provider.Response, error) {
	// Groq exposes model/org-specific TPM limits. Keep a conservative request
	// budget so a large prompt plus max_tokens does not get rejected simply
	// because the client requested an unnecessarily large completion allowance.
	if o.RequestTokenLimit <= 0 {
		o.RequestTokenLimit = safeRequestTokenBudget
	}
	r, err := p.inner.Send(ctx, m, o)
	if r != nil {
		r.ProviderName = p.Name()
	}
	return r, err
}

func (p *Provider) Stream(ctx context.Context, m []provider.Message, o provider.RequestOptions) (<-chan provider.StreamChunk, error) {
	if o.RequestTokenLimit <= 0 {
		o.RequestTokenLimit = safeRequestTokenBudget
	}
	return p.inner.Stream(ctx, m, o)
}

func (p *Provider) ListModels(ctx context.Context) ([]string, error) { return p.inner.ListModels(ctx) }
