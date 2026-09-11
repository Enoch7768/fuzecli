package app

import (
	"github.com/Enoch7768/fuzecli/internal/provider"
	"github.com/Enoch7768/fuzecli/internal/provider/anthropic"
	"github.com/Enoch7768/fuzecli/internal/provider/gemini"
	"github.com/Enoch7768/fuzecli/internal/provider/groq"
	"github.com/Enoch7768/fuzecli/internal/provider/llamacpp"
	"github.com/Enoch7768/fuzecli/internal/provider/openai"
)

func (a *App) ReloadProviders() {
	c := a.Config
	procs := []provider.Provider{
		openai.New(c.Providers["openai"].APIKey, c.Providers["openai"].BaseURL),
		gemini.New(c.Providers["gemini"].APIKey, c.Providers["gemini"].BaseURL),
		groq.New(c.Providers["groq"].APIKey, c.Providers["groq"].BaseURL),
		anthropic.New(c.Providers["anthropic"].APIKey, c.Providers["anthropic"].BaseURL),
		llamacpp.New(c.Providers["llamacpp"].BaseURL),
	}
	defaults := make(map[string]string, len(c.Providers))
	for name, cfg := range c.Providers {
		defaults[name] = cfg.DefaultModel
	}
	a.Registry = provider.NewRegistry(c.FallbackOrder, defaults, procs...)
}
