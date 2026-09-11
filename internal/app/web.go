package app

import (
	"context"
	"errors"
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
	system := generation.StrictExecutionMode + "\n\nYou are FuzeCLI, a practical coding assistant. For ordinary questions, answer naturally in plain text. For requests that create, modify, or delete project files, return ONLY one valid JSON object with this shape: {\"files\":[{\"path\":\"relative/path.ext\",\"line_start\":1,\"line_end\":1000,\"content\":\"full file content\",\"action\":\"create|modify|delete\"}],\"explanation\":\"brief explanation\",\"commands\":[]}. The line_start and line_end fields are optional metadata. The action field is optional; when absent, FuzeCLI infers create or modify from the actual workspace. Never use markdown fences around generation JSON. Never return shell commands for FuzeCLI to execute automatically. Generated commands are informational only."
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
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := a.Registry.Stream(streamCtx, name, msgs, provider.RequestOptions{Model: mdl, Temperature: 0.3, MaxTokens: 16000})
	if err != nil {
		return err
	}
	var response strings.Builder
	var candidate strings.Builder
	jsonPossible := false
	applied := false
	var appliedPlan generation.Plan

	applyPlan := func(plan generation.Plan) error {
		written, applyErr := generation.ApplyChatPlan(a.Store.Root, plan)
		if applyErr != nil {
			return applyErr
		}
		if err := a.Store.MarkTouched(written); err != nil {
			return err
		}
		st, _ := a.Store.LoadState()
		st.ActiveProvider = name
		st.ActiveModel = mdl
		if err := a.Store.SaveState(st); err != nil {
			return err
		}
		if err := a.Store.RefreshHashes(written); err != nil {
			return err
		}
		appliedPlan = plan
		applied = true
		cancel()
		return nil
	}

	for chunk := range stream {
		if chunk.Error != nil {
			if applied && errors.Is(chunk.Error, context.Canceled) {
				break
			}
			return chunk.Error
		}
		if chunk.Delta == "" {
			if chunk.Done {
				break
			}
			continue
		}
		response.WriteString(chunk.Delta)
		trimmed := strings.TrimSpace(response.String())
		if !jsonPossible && len(trimmed) > 0 {
			if trimmed[0] == '{' {
				jsonPossible = true
				candidate.Reset()
			}
		}
		if jsonPossible {
			candidate.WriteString(chunk.Delta)
			if plan, parseErr := generation.ParseChatPlan(candidate.String()); parseErr == nil {
				if err := applyPlan(plan); err != nil {
					return err
				}
				break
			}
			if candidate.Len() >= 512 && !strings.Contains(candidate.String(), `"files"`) && !strings.Contains(candidate.String(), `"explanation"`) {
				jsonPossible = false
				if onDelta != nil {
					onDelta(candidate.String())
				}
				candidate.Reset()
			}
			continue
		}
		if onDelta != nil {
			onDelta(chunk.Delta)
		}
	}

	if applied {
		summary := appliedPlan.Explanation
		if summary == "" {
			summary = "Workspace updated successfully."
		}
		return a.Store.AddMessage(provider.Message{Role: "assistant", Content: summary})
	}

	text := strings.TrimSpace(response.String())
	if text == "" {
		return errors.New("provider returned an empty response")
	}
	if jsonPossible {
		if plan, parseErr := generation.ParseChatPlan(candidate.String()); parseErr == nil {
			if err := applyPlan(plan); err != nil {
				return err
			}
			return a.Store.AddMessage(provider.Message{Role: "assistant", Content: plan.Explanation})
		}
	}
	return a.Store.AddMessage(provider.Message{Role: "assistant", Content: response.String()})
}
