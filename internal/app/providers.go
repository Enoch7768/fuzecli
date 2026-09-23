package app

import (
	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/provider"
	"github.com/Enoch7768/fuzecli/internal/provider/anthropic"
	"github.com/Enoch7768/fuzecli/internal/provider/gemini"
	"github.com/Enoch7768/fuzecli/internal/provider/groq"
	"github.com/Enoch7768/fuzecli/internal/provider/llamacpp"
	"github.com/Enoch7768/fuzecli/internal/provider/openai"
	"github.com/Enoch7768/fuzecli/internal/providerfactory"
)

func buildProviders(c config.Config) []provider.Provider {
	procs := []provider.Provider{
		openai.New(c.Providers["openai"].APIKey, ""),
		gemini.New(c.Providers["gemini"].APIKey, ""),
		groq.New(c.Providers["groq"].APIKey, ""),
		anthropic.New(c.Providers["anthropic"].APIKey, ""),
		llamacpp.New(c.Providers["llamacpp"].BaseURL),
	}
	return append(procs, providerfactory.New(c)...)
}

func (a *App) ReloadProviders() {
	c := a.Config
	defaults := make(map[string]string, len(c.Providers))
	for name, cfg := range c.Providers {
		defaults[name] = cfg.DefaultModel
	}
	a.Registry = provider.NewRegistry(
		c.FallbackOrder,
		defaults,
		buildProviders(c)...,
	)
}
