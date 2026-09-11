package gemininormalizer

import (
	"github.com/Enoch7768/fuzecli/internal/provider/gemini"
)

type Provider struct {
	*gemini.Provider
}

func New(apiKey, baseURL string) *Provider {
	return &Provider{Provider: gemini.New(apiKey, baseURL)}
}

func (p *Provider) Name() string { return "gemini-normalizer" }
