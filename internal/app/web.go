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

func (a *App) SessionWelcome(ctx context.Context, providerName, model string) (string, error) {
	if a.Store == nil {
		return "", context.Canceled
	}
	name, mdl, err := a.ProviderAndModel(providerName, model)
	if err != nil {
		return "", err
	}
	history, err := a.Store.History(400)
	if err != nil {
		return "", err
	}
	welcomePrompt := "Before the user sends their first request, respond with a brief, warm, professional welcoming message. Welcome them to FuzeCLI, state that you are ready to follow their instructions exactly, and then wait for their request. Do not solve a task, propose features, or ask unnecessary questions."
	msgs := []provider.Message{{Role: "system", Content: generation.StrictExecutionMode}}
	msgs = append(msgs, history...)
	msgs = append(msgs, provider.Message{Role: "user", Content: welcomePrompt})
	msgs = generationTrim(msgs, 18000)
	stream, err := a.Registry.Stream(ctx, name, msgs, provider.RequestOptions{Model: mdl, Temperature: 0.2, MaxTokens: 300})
	if err != nil {
		return "", err
	}
	var response strings.Builder
	for chunk := range stream {
		if chunk.Error != nil {
			return "", chunk.Error
		}
		if chunk.Delta != "" {
			response.WriteString(chunk.Delta)
		}
	}
	text := strings.TrimSpace(response.String())
	if text == "" {
		return "", context.Canceled
	}
	st, _ := a.Store.LoadState()
	st.ActiveProvider = name
	st.ActiveModel = mdl
	_ = a.Store.SaveState(st)
	return text, nil
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
	system := generation.StrictExecutionMode + "\n\nYou are FuzeCLI, a practical coding assistant. For ordinary questions, answer naturally in plain text. When the user asks you to create, modify, or delete files and the response can be represented by the FuzeCLI generation schema, return ONLY that valid generation JSON so FuzeCLI can apply it safely. Never use markdown fences for generation JSON."
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
	var possibleJSON *bool
	var delayed strings.Builder
	for chunk := range stream {
		if chunk.Error != nil {
			return chunk.Error
		}
		if chunk.Delta == "" {
			continue
		}
		response.WriteString(chunk.Delta)
		if possibleJSON == nil {
			prefix := strings.TrimSpace(response.String())
			if prefix == "" {
				continue
			}
			isJSON := strings.HasPrefix(prefix, "{") || strings.HasPrefix(prefix, "[")
			possibleJSON = &isJSON
			if isJSON {
				delayed.WriteString(chunk.Delta)
				continue
			}
			if onDelta != nil {
				onDelta(delayed.String())
				onDelta(chunk.Delta)
			}
			delayed.Reset()
			continue
		}
		if *possibleJSON {
			delayed.WriteString(chunk.Delta)
			continue
		}
		if onDelta != nil {
			onDelta(chunk.Delta)
		}
	}
	text := strings.TrimSpace(response.String())
	if text == "" {
		return context.Canceled
	}
	if plan, parseErr := generation.ParsePlan(response.String()); parseErr == nil {
		written, applyErr := generation.Apply(a.Store.Root, plan)
		if applyErr != nil {
			return applyErr
		}
		if err := a.Store.MarkTouched(written); err != nil {
			return err
		}
		st, _ := a.Store.LoadState()
		st.ActiveProvider = name
		st.ActiveModel = mdl
		_ = a.Store.SaveState(st)
		_ = a.Store.RefreshHashes(written)
	} else if possibleJSON != nil && *possibleJSON && onDelta != nil {
		onDelta(delayed.String())
	}
	return a.Store.AddMessage(provider.Message{Role: "assistant", Content: response.String()})
}
