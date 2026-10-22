package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/agent"
	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/provider"
	"github.com/Enoch7768/fuzecli/internal/platform"
)

const fuzeAIModelPrefix = "r7194714R/fuzeai-qwen-7b-gguf"

func IsFuzeAIModel(providerName, model string) bool {
	return strings.EqualFold(strings.TrimSpace(providerName), "llamacpp") &&
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), strings.ToLower(fuzeAIModelPrefix))
}

// RunAgent executes a FuzeAI tool-protocol turn using FuzeCLI's registered
// tools. The model may request actions, but FuzeCLI validates and executes them.
func (a *App) RunAgent(
	ctx context.Context,
	prompt, providerName, model, extraContext string,
	authorize agent.ToolAuthorizer,
) (agent.RunResult, error) {
	if a == nil || a.Store == nil {
		return agent.RunResult{}, fmt.Errorf("workspace not initialized; run aicli init")
	}
	if a.AgentTools == nil {
		return agent.RunResult{}, fmt.Errorf("FuzeCLI agent tools are not initialized; reopen the workspace")
	}
	name, mdl, err := a.ProviderAndModel(providerName, model)
	if err != nil {
		return agent.RunResult{}, err
	}
	if !IsFuzeAIModel(name, mdl) {
		return agent.RunResult{}, fmt.Errorf("the FuzeAI agent runner requires provider llamacpp and model %q", fuzeAIModelPrefix)
	}

	history, err := a.Store.History(40)
	if err != nil {
		return agent.RunResult{}, fmt.Errorf("load agent conversation history: %w", err)
	}
	workspaceContext, err := a.Store.WorkspaceContext()
	if err != nil {
		return agent.RunResult{}, fmt.Errorf("read workspace context: %w", err)
	}
	system := generation.SessionSystemPrompt() +
		"\nYou are FuzeAI, the coding model integrated with FuzeCLI. Use the supplied FuzeCLI tools to inspect and change the actual workspace. Tool results are the source of truth. Never claim a command ran, a file changed, a test passed, or a GitHub action succeeded unless the tool result confirms it. Keep answers clear and concise. FuzeCLI enforces permissions and executes tools; never invent tool names or arguments."
	if profileText := a.Profile.Condensed(); profileText != "" {
		system += "\nDeveloper profile:\n" + profileText
	}
	if workspaceContext != "" {
		system += "\nRelevant workspace context:\n" + workspaceContext
	}
	if a.Repository != nil {
		if indexed := a.Repository.Context(prompt, 12); indexed != "" {
			system += "\nRepository index context:\n" + indexed
		}
	}
	if a.Platform != nil {
		rules, _ := platform.LoadRules(a.Store.Root)
		skills, _ := platform.LoadSkills(a.Store.Root)
		if extra, contextErr := a.Platform.Context(ctx, prompt, rules, skills); contextErr == nil && extra != "" {
			system += "\nProject rules and skills:\n" + extra
		}
	}
	if strings.TrimSpace(extraContext) != "" {
		system += "\nAdditional user-attached context:\n" + extraContext
	}

	messages := make([]provider.Message, 0, len(history)+2)
	messages = append(messages, provider.Message{Role: "system", Content: system})
	messages = append(messages, history...)
	messages = append(messages, provider.Message{Role: "user", Content: prompt})
	runner := agent.Runner{Sender: a.Registry, Tools: a.AgentTools}
	result, err := runner.Run(ctx, name, messages, provider.RequestOptions{
		Model: mdl, Temperature: 0.2, MaxTokens: 16000,
	}, authorize)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(result.Response) == "" {
		return result, fmt.Errorf("FuzeAI finished without a final response")
	}

	if err := a.Store.AddMessage(provider.Message{Role: "user", Content: prompt}); err != nil {
		return result, fmt.Errorf("save user message: %w", err)
	}
	if err := a.Store.AddMessage(provider.Message{Role: "assistant", Content: result.Response}); err != nil {
		return result, fmt.Errorf("save assistant response: %w", err)
	}

	var changed []string
	for _, toolResult := range result.ToolResults {
		if toolResult.Status != agent.ToolResultSuccess || toolResult.Name != "workspace.apply_edits" {
			continue
		}
		var applied struct {
			Changed []string `json:"changed"`
			Applied bool `json:"applied"`
		}
		if json.Unmarshal(toolResult.Result, &applied) == nil && applied.Applied {
			changed = append(changed, applied.Changed...)
		}
	}
	if len(changed) > 0 {
		seen := make(map[string]bool, len(changed))
		unique := changed[:0]
		for _, path := range changed {
			if !seen[path] {
				seen[path] = true
				unique = append(unique, path)
			}
		}
		if err := a.Store.MarkTouched(unique); err != nil {
			return result, fmt.Errorf("record changed files: %w", err)
		}
		if err := a.Store.RefreshHashes(unique); err != nil {
			return result, fmt.Errorf("refresh changed-file hashes: %w", err)
		}
		if a.Repository != nil {
			_ = a.Repository.Refresh()
		}
	}
	return result, nil
}
