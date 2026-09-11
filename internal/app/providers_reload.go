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
	for name, cfg := range a.Config.Providers {
		provider.Configure(name, cfg.APIKey, cfg.BaseURL)
	}

	providers := []provider.Provider{
		openai.New(a.Config.Providers["openai"].APIKey, a.Config.Providers["openai"].BaseURL),
		gemini.New(a.Config.Providers["gemini"].APIKey, a.Config.Providers["gemini"].BaseURL),
		groq.New(a.Config.Providers["groq"].APIKey, a.Config.Providers["groq"].BaseURL),
		anthropic.New(a.Config.Providers["anthropic"].APIKey, a.Config.Providers["anthropic"].BaseURL),
		llamacpp.New(a.Config.Providers["llamacpp"].BaseURL),
	}

	defaults := make(map[string]string, len(a.Config.Providers))
	for name, cfg := range a.Config.Providers {
		defaults[name] = cfg.DefaultModel
	}

	a.Registry = provider.NewRegistry(a.Config.FallbackOrder, defaults, providers...)
}
