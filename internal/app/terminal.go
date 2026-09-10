package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Enoch7768/fuzecli/internal/diagnostics"
	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/provider"
)

func (a *App) TerminalChat(ctx context.Context, yes bool) error {
	if a.Store == nil {
		return fmt.Errorf("workspace not initialized; run aicli init")
	}
	providerName, model, _ := a.ProviderAndModel("", "")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	if err := a.terminalSessionPreflight(ctx, providerName, model, scanner); err != nil {
		return err
	}
	printTerminalHeader(providerName, model, a.Store.Root)
	for {
		fmt.Print("\n\x1b[38;5;111m❯\x1b[0m ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			command, value := splitTerminalCommand(line)
			switch command {
			case "/exit", "/quit", "/q":
				fmt.Println("\n\x1b[38;5;244mSession closed.\x1b[0m")
				return scanner.Err()
			case "/help", "/?":
				printTerminalHelp()
			case "/provider":
				if value == "" {
					fmt.Printf("Current provider: %s\n", providerName)
					continue
				}
				if _, _, err := a.ProviderAndModel(value, ""); err != nil {
					fmt.Println(formatTerminalError(err, providerName, model))
					continue
				}
				providerName = strings.TrimSpace(value)
				_, model, _ = a.ProviderAndModel(providerName, "")
				fmt.Printf("\x1b[38;5;111mProvider\x1b[0m  %s\n\x1b[38;5;244mModel\x1b[0m     %s\n", providerName, model)
			case "/model":
				if value == "" {
					fmt.Printf("Current model: %s\n", model)
					continue
				}
				model = strings.TrimSpace(value)
				fmt.Printf("Model changed to %s\n", model)
			case "/status":
				printTerminalStatus(providerName, model, a.Store.Root)
			case "/clear":
				fmt.Print("\x1b[2J\x1b[H")
				printTerminalHeader(providerName, model, a.Store.Root)
			case "/code":
				if value == "" {
					fmt.Println("\x1b[38;5;214mUsage:\x1b[0m /code <request>")
					continue
				}
				if err := a.terminalCode(ctx, value, providerName, model, yes); err != nil {
					fmt.Println(formatTerminalError(err, providerName, model))
				}
			default:
				fmt.Printf("Unknown command %q. Type /help for commands.\n", command)
			}
			continue
		}
		if err := a.terminalStream(ctx, line, providerName, model); err != nil {
			fmt.Println(formatTerminalError(err, providerName, model))
		}
	}
	go a.RunProfileExtraction(context.Background())
	return scanner.Err()
}

func (a *App) terminalSessionPreflight(ctx context.Context, providerName, model string, scanner *bufio.Scanner) error {
	fmt.Println("\n\x1b[1;38;5;117mFuzeCLI SESSION PREFLIGHT\x1b[0m")
	fmt.Println("\x1b[38;5;244m────────────────────────────────────────────────────────────\x1b[0m")
	fmt.Println("Choose how this session should handle conversation memory.")
	fmt.Println()
	fmt.Println("\n\x1b[1mMemory mode\x1b[0m")
	fmt.Println("  [M] Continue with memory")
	fmt.Println("  [F] Start fresh and clear memory")
	fmt.Print("\n\x1b[38;5;111mChoice\x1b[0m: ")

	for scanner.Scan() {
		choice := strings.ToLower(strings.TrimSpace(scanner.Text()))
		switch choice {
		case "m", "memory", "continue":
			fmt.Println("\x1b[38;5;244mMemory retained.\x1b[0m")
		case "f", "fresh", "clear", "new":
			if err := a.Store.ClearMemory(); err != nil {
				return err
			}
			fmt.Println("\x1b[38;5;244mConversation memory cleared.\x1b[0m")
		default:
			fmt.Print("\x1b[38;5;214mChoose M or F:\x1b[0m ")
			continue
		}
		break
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	fmt.Println("\n\x1b[1mPreflight\x1b[0m")
	fmt.Println("\x1b[38;5;244mYour request stays locked while FuzeCLI prepares the strict briefing and selected memory.\x1b[0m")

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	remaining := 60
	for remaining > 0 {
		fmt.Printf("\r\x1b[K\x1b[38;5;111mSession prepares in %02d seconds\x1b[0m", remaining)
		select {
		case <-ctx.Done():
			fmt.Println()
			return ctx.Err()
		case <-ticker.C:
			remaining--
		}
	}
	fmt.Print("\r\x1b[K")

	fmt.Println("\n\x1b[38;5;111mFuzeCLI\x1b[0m is sending the strict execution briefing with your selected memory…")
	welcome, err := a.SessionWelcome(ctx, providerName, model)
	if err != nil {
		return err
	}
	fmt.Printf("\n\x1b[38;5;111mFuzeCLI\x1b[0m\n%s\n", welcome)
	fmt.Println("\n\x1b[38;5;244mSession ready. Your next input will be sent as your request.\x1b[0m")
	return nil
}

func splitTerminalCommand(line string) (string, string) {
	parts := strings.SplitN(line, " ", 2)
	command := strings.ToLower(strings.TrimSpace(parts[0]))
	if len(parts) == 1 {
		return command, ""
	}
	return command, strings.TrimSpace(parts[1])
}

func printTerminalHeader(providerName, model, root string) {
	fmt.Println("\n\x1b[1;38;5;117mF U Z E C L I\x1b[0m  \x1b[38;5;244m· local coding workspace\x1b[0m")
	fmt.Println("\x1b[38;5;239m────────────────────────────────────────────────────────────\x1b[0m")
	fmt.Printf("\x1b[38;5;111mProvider\x1b[0m  %s    \x1b[38;5;111mModel\x1b[0m  %s\n", providerName, model)
	fmt.Printf("\x1b[38;5;244mWorkspace\x1b[0m %s\n", root)
	fmt.Println("\x1b[38;5;244mType /help for commands · /code for project generation · /exit to leave\x1b[0m")
}

func printTerminalHelp() {
	fmt.Println("\n\x1b[1mCommands\x1b[0m")
	fmt.Println("  /code <request>   Generate or resume files in the workspace")
	fmt.Println("  /provider <name>  Change provider for this session")
	fmt.Println("  /model <name>     Change model for this session")
	fmt.Println("  /status           Show provider, model and workspace")
	fmt.Println("  /clear            Clear the terminal view")
	fmt.Println("  /exit             Close the session")
}

func printTerminalStatus(providerName, model, root string) {
	fmt.Println("\n\x1b[1mRuntime\x1b[0m")
	fmt.Printf("  Provider  %s\n", providerName)
	fmt.Printf("  Model     %s\n", model)
	fmt.Printf("  Workspace %s\n", root)
}

func (a *App) terminalStream(ctx context.Context, prompt, providerName, model string) error {
	start := time.Now()
	stop := make(chan struct{})
	var once sync.Once
	phase := "Preparing request"
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Printf("\r\x1b[K\x1b[38;5;244m· %s · %s · %s\x1b[0m", phase, providerName, formatTerminalElapsed(time.Since(start)))
			case <-stop:
				return
			}
		}
	}()
	defer func() {
		once.Do(func() { close(stop) })
		fmt.Print("\r\x1b[K")
	}()

	history, err := a.Store.History(400)
	if err != nil {
		return err
	}
	phase = "Preparing conversation context"
	name, mdl, err := a.ProviderAndModel(providerName, model)
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
	phase = "Assembling workspace context"
	system := generation.StrictExecutionMode + "\n\nYou are FuzeCLI, a practical coding assistant. The workspace context below was read directly from the user's local workspace by FuzeCLI. You have access to those files through this supplied context and must use them when the request concerns the project. Do not ask the user to paste files that are present in the workspace. Do not claim you lack access to the local project when FuzeCLI has supplied the relevant file contents. For ordinary questions, answer naturally in plain text. When the user asks you to create, modify, or delete files and the response can be represented by the FuzeCLI generation schema, return ONLY that valid generation JSON so FuzeCLI can apply it safely. Never use markdown fences for generation JSON."
	if a.Profile.Condensed() != "" {
		system += "\nDeveloper profile:\n" + a.Profile.Condensed()
	}
	if workspaceContext != "" {
		system += "\nRelevant workspace files read from disk:\n" + workspaceContext
	}
	msgs := []provider.Message{
		{Role: "system", Content: system},
	}
	msgs = append(msgs, history...)
	msgs = append(msgs, provider.Message{Role: "user", Content: prompt})
	msgs = generationTrim(msgs, 120000)
	if err := a.Store.AddMessage(provider.Message{Role: "user", Content: prompt}); err != nil {
		return err
	}
	phase = "Waiting for provider"
	stream, err := a.Registry.Stream(ctx, name, msgs, provider.RequestOptions{Model: mdl, Temperature: 0.3, MaxTokens: 16000})
	if err != nil {
		return err
	}
	phase = "Receiving response"
	var response strings.Builder
	chunks := 0
	for chunk := range stream {
		if chunk.Error != nil {
			return chunk.Error
		}
		if chunk.Delta != "" {
			response.WriteString(chunk.Delta)
			chunks++
			if chunks%40 == 0 {
				phase = fmt.Sprintf("Receiving response · %d chunks", chunks)
			}
		}
	}
	phase = "Finalizing response"
	text := strings.TrimSpace(response.String())
	if text == "" {
		return context.Canceled
	}

	if plan, parseErr := generation.ParsePlan(response.String()); parseErr == nil {
		phase = "Applying generated files"
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
		fmt.Println("\n\x1b[38;5;111mFuzeCLI\x1b[0m")
		if len(written) == 1 {
			fmt.Printf("Generated 1 file: %s\n", written[0])
		} else {
			fmt.Printf("Generated %d files:\n", len(written))
			for _, path := range written {
				fmt.Printf("  ✓ %s\n", path)
			}
		}
		if plan.Explanation != "" {
			fmt.Printf("\n%s\n", plan.Explanation)
		}
	} else {
		fmt.Println("\n\x1b[38;5;111mFuzeCLI\x1b[0m")
		fmt.Println(response.String())
	}
	return a.Store.AddMessage(provider.Message{Role: "assistant", Content: response.String()})
}

func formatTerminalElapsed(d time.Duration) string {
	total := int(d.Round(time.Second) / time.Second)
	return fmt.Sprintf("%dm%02ds", total/60, total%60)
}

func (a *App) terminalCode(ctx context.Context, prompt, providerName, model string, yes bool) error {
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := a.Ask(ctx, prompt, providerName, model, yes)
		done <- err
	}()
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				elapsed := time.Since(start).Round(time.Second)
				completed, total, current := terminalPlanProgress(a.Store.Root)
				if total > 0 {
					pct := completed * 100 / total
					fmt.Printf("\r\x1b[K\x1b[38;5;111m%s\x1b[0m %d%% · %d/%d · %s · %s", terminalSpinnerFrame(), pct, completed, total, current, elapsed)
				} else {
					fmt.Printf("\r\x1b[K\x1b[38;5;111m%s\x1b[0m Working · %s", terminalSpinnerFrame(), elapsed)
				}
			case <-stop:
				return
			}
		}
	}()
	err := <-done
	once.Do(func() { close(stop) })
	fmt.Print("\r\x1b[K")
	return err
}

func terminalPlanProgress(root string) (int, int, string) {
	data, err := os.ReadFile(generation.ProjectPlanPath(root))
	if err != nil {
		return 0, 0, "planning"
	}
	var plan generation.ProjectPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return 0, 0, "planning"
	}
	total := len(plan.Files)
	completed := total - len(generation.PendingFiles(plan))
	current := "verifying"
	pending := generation.PendingFiles(plan)
	if len(pending) > 0 {
		current = pending[0].Path
	}
	return completed, total, current
}

var terminalFrame uint64

func terminalSpinnerFrame() string {
	terminalFrame++
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[int(terminalFrame)%len(frames)]
}

func formatTerminalError(err error, providerName, model string) string {
	return "\n\x1b[1;38;5;203m✕ " + diagnostics.FormatTerminal(err, providerName, model) + "\x1b[0m"
}