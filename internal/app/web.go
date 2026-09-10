package app

import (
	"context"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/provider"
)

func (a *App) ChatRequest(ctx context.Context, prompt, providerName, model string) error {
	if providerName != "" {
		a.Config.DefaultProvider = providerName
	}
	if providerName != "" && model != "" {
		if pc, ok := a.Config.Providers[providerName]; ok {
			pc.DefaultModel = model
			a.Config.Providers[providerName] = pc
		}
	}
	return a.chatTurn(ctx, prompt)
}

func (a *App) ChatStreamRequest(ctx context.Context, prompt, providerName, model string, onDelta func(string)) error {
	if a.Store == nil {
		return context.Canceled
	}

	if providerName != "" {
		a.Config.DefaultProvider = providerName
	}
	if providerName != "" && model != "" {
		if pc, ok := a.Config.Providers[providerName]; ok {
			pc.DefaultModel = model
			a.Config.Providers[providerName] = pc
		}
	}

	name, mdl, err := a.ProviderAndModel(providerName, model)
	if err != nil {
		return err
	}

	history, err := a.Store.History(400)
	if err != nil {
		return err
	}
	history, err = a.compactHistory(ctx, name, mdl, history)
	if err != nil {
		return err
	}

	workspaceContext, err := a.Store.WorkspaceContext()
	if err != nil {
		return err
	}

	system := "You are FuzeCLI, a practical coding assistant. Answer clearly and concisely. Do not modify files in chat mode."
	if a.Profile.Condensed() != "" {
		system += "\nDeveloper profile:\n" + a.Profile.Condensed()
	}
	if workspaceContext != "" {
		system += "\nRelevant workspace files:\n" + workspaceContext
	}

	msgs := []provider.Message{{Role: "system", Content: system}}
	msgs = append(msgs, history...)
	msgs = append(msgs, provider.Message{Role: "user", Content: prompt})
	msgs = generationTrim(msgs, 120000)

	if err := a.Store.AddMessage(provider.Message{Role: "user", Content: prompt}); err != nil {
		return err
	}

	stream, err := a.Registry.Stream(ctx, name, msgs, provider.RequestOptions{Model: mdl, Temperature: 0.3, MaxTokens: 4000})
	if err != nil {
		return err
	}

	var response strings.Builder
	for chunk := range stream {
		if chunk.Error != nil {
			return chunk.Error
		}
		if chunk.Delta == "" {
			continue
		}
		response.WriteString(chunk.Delta)
		if onDelta != nil {
			onDelta(chunk.Delta)
		}
	}

	return a.Store.AddMessage(provider.Message{Role: "assistant", Content: response.String()})
}
