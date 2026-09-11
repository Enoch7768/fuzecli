package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/provider"
)

const terminalAttachmentLimit = 65536

func (a *App) TerminalChat(ctx context.Context, _ bool) error {
	if a.Store == nil {
		return fmt.Errorf("workspace not initialized; run aicli init")
	}
	providerName, model, _ := a.ProviderAndModel("", "")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	if err := a.terminalSessionPreflight(ctx, providerName, model, scanner); err != nil {
		return err
	}
	attachments := make(map[string]string)
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
					fmt.Println(formatTerminalError(err))
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
			case "/file":
				if value == "" {
					paths, err := pickTerminalFiles(a.Store.Root)
					if err != nil {
						fmt.Println(formatTerminalError(err))
						continue
					}
					for _, selected := range paths {
						if err := attachSelectedTerminalFile(a.Store.Root, selected, attachments); err != nil {
							fmt.Println(formatTerminalError(err))
							continue
						}
					}
					continue
				}
				if strings.EqualFold(value, "list") {
					printAttachedFiles(attachments)
					continue
				}
				if strings.EqualFold(value, "clear") {
					clearAttachments(attachments)
					fmt.Println("\x1b[38;5;244mAttached files cleared.\x1b[0m")
					continue
				}
				path, err := attachTerminalFile(a.Store.Root, value)
				if err != nil {
					fmt.Println(formatTerminalError(err))
					continue
				}
				data, err := os.ReadFile(path)
				if err != nil {
					fmt.Println(formatTerminalError(err))
					continue
				}
				rel := filepathSlash(value)
				attachments[rel] = string(data)
				fmt.Printf("\x1b[38;5;111mAttached\x1b[0m %s\n", rel)
			default:
				fmt.Printf("Unknown command %q. Type /help for commands.\n", command)
			}
			continue
		}
		if err := a.terminalStream(ctx, line, providerName, model, attachments); err != nil {
			fmt.Println(formatTerminalError(err))
		}
	}
	go a.RunProfileExtraction(context.Background())
	return scanner.Err()
}

func pickTerminalFiles(root string) ([]string, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("interactive file upload is supported on Windows; use /file <relative-path>")
	}
	script := `$ErrorActionPreference = 'Stop'; Add-Type -AssemblyName System.Windows.Forms; $dialog = New-Object System.Windows.Forms.OpenFileDialog; $dialog.Multiselect = $true; $dialog.CheckFileExists = $true; $dialog.InitialDirectory = $env:FUZECLI_ROOT; $dialog.Filter = 'All files (*.*)|*.*'; if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { $dialog.FileNames }`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-Command", script)
	cmd.Env = append(os.Environ(), "FUZECLI_ROOT="+root)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("file picker failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("file picker failed: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line != "" {
			paths = append(paths, line)
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}
	return paths, nil
}

func attachSelectedTerminalFile(root, selected string, attachments map[string]string) error {
	absolute, err := filepath.Abs(selected)
	if err != nil {
		return err
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, absolute)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("selected file must be inside the workspace: %s", selected)
	}
	rel = filepath.ToSlash(rel)
	path, err := attachTerminalFile(root, rel)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	attachments[rel] = string(data)
	fmt.Printf("\x1b[38;5;111mAttached\x1b[0m %s\n", rel)
	return nil
}

func printAttachedFiles(attachments map[string]string) {
	if len(attachments) == 0 {
		fmt.Println("No attached files.")
		return
	}
	fmt.Printf("Attached files: %d\n", len(attachments))
	for path := range attachments {
		fmt.Printf("  ✓ %s\n", path)
	}
}

func attachTerminalFile(root, rel string) (string, error) {
	path, err := generation.Resolve(root, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot attach %s: %w", rel, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("cannot attach directory %s", rel)
	}
	if info.Size() > terminalAttachmentLimit {
		return "", fmt.Errorf("file %s is larger than 64 KiB", rel)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", rel, err)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("file %s is not valid UTF-8 text", rel)
	}
	return path, nil
}

func clearAttachments(attachments map[string]string) {
	for path := range attachments {
		delete(attachments, path)
	}
}

func filepathSlash(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
}

func (a *App) terminalSessionPreflight(ctx context.Context, providerName, model string, scanner *bufio.Scanner) error {
	fmt.Println("\n\x1b[1;38;5;117mFuzeCLI SESSION PREFLIGHT\x1b[0m")
	fmt.Println("\x1b[38;5;244m────────────────────────────────────────────────────────────\x1b[0m")
	fmt.Println("Choose how this session should handle conversation memory.")
	fmt.Println()
	fmt.Println("\x1b[1mMemory mode\x1b[0m")
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
	fmt.Println("\x1b[38;5;244mPreparing strict instructions, memory, and workspace access.\x1b[0m")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	remaining := 5
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
	fmt.Println("\n\x1b[38;5;111mFuzeCLI\x1b[0m is ready for chat.")
	welcome, err := a.SessionWelcome(ctx, providerName, model)
	if err != nil {
		return err
	}
	fmt.Printf("\n%s\n", welcome)
	fmt.Println("\n\x1b[38;5;244mSession ready. Use natural language for both conversation and project changes.\x1b[0m")
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
	fmt.Println("\x1b[38;5;244mChat only · /file · /provider · /model · /status · /clear · /help · /exit\x1b[0m")
}

func printTerminalHelp() {
	fmt.Println("\n\x1b[1mChat Commands\x1b[0m")
	fmt.Println("  /file                 Open the interactive file picker")
	fmt.Println("  /file <relative-path> Attach a workspace text file")
	fmt.Println("  /file list            Show attached files")
	fmt.Println("  /file clear           Clear attached files")
	fmt.Println("  /provider <name>      Change provider for this session")
	fmt.Println("  /model <name>         Change model for this session")
	fmt.Println("  /status               Show provider, model and workspace")
	fmt.Println("  /clear                Clear the terminal view")
	fmt.Println("  /exit                 Close the session")
	fmt.Println()
	fmt.Println("Attach files, then use natural language for your prompt. Attached files are supplied directly to the model for the session.")
	fmt.Println("Project changes are requested naturally in chat. When the model returns valid file JSON, FuzeCLI writes it automatically.")
}

func printTerminalStatus(providerName, model, root string) {
	fmt.Println("\n\x1b[1mRuntime\x1b[0m")
	fmt.Printf("  Provider  %s\n", providerName)
	fmt.Printf("  Model     %s\n", model)
	fmt.Printf("  Workspace %s\n", root)
}

func formatTerminalError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("\x1b[38;5;214mError:\x1b[0m %s", err.Error())
}

func (a *App) terminalStream(ctx context.Context, prompt, providerName, model string, attachments map[string]string) error {
	start := time.Now()
	history, err := a.Store.History(400)
	if err != nil {
		return err
	}
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
	system := generation.StrictExecutionMode + "\n\nYou are FuzeCLI, a practical coding assistant. The workspace context was read directly from the user's local workspace. Never ask the user to paste a file that exists there. Use natural language for ordinary conversation. For any request that creates, modifies, or deletes project files, return ONLY one valid JSON object with this shape: {\"files\":[{\"path\":\"relative/path.ext\",\"line_start\":1,\"line_end\":1000,\"content\":\"full file content\"}],\"explanation\":\"brief explanation\",\"commands\":[]}. The line_start and line_end fields are optional metadata. The action field is optional; when it is absent, FuzeCLI infers create or modify from the actual workspace. Never use markdown fences around generation JSON. Never return a request asking the user to provide source files that FuzeCLI already supplied."
	if profileText := a.Profile.Condensed(); profileText != "" {
		system += "\nDeveloper profile:\n" + profileText
	}
	if workspaceContext != "" {
		system += "\nRelevant workspace files read from disk:\n" + workspaceContext
	}
	if len(attachments) > 0 {
		system += "\nUser-attached workspace files:\n"
		for path, content := range attachments {
			system += "\n--- " + path + " ---\n" + content + "\n--- end " + path + " ---\n"
		}
	}
	engine := generation.Engine{Registry: a.Registry, Profile: &a.Profile, MaxContextChars: 120000}
	msgs := engine.Messages(a.Profile.Condensed(), workspaceContext, history, prompt)
	msgs[0].Content = system
	if err := a.Store.AddMessage(provider.Message{Role: "user", Content: prompt}); err != nil {
		return err
	}
	stream, err := a.Registry.Stream(ctx, name, msgs, provider.RequestOptions{Model: mdl, Temperature: 0.3, MaxTokens: 16000})
	if err != nil {
		return err
	}
	var response strings.Builder
	for chunk := range stream {
		if chunk.Error != nil {
			return chunk.Error
		}
		response.WriteString(chunk.Delta)
	}
	text := strings.TrimSpace(response.String())
	if text == "" {
		return fmt.Errorf("provider returned an empty response")
	}
	if plan, parseErr := generation.ParseChatPlan(text); parseErr == nil {
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
		_ = a.Store.SaveState(st)
		_ = a.Store.RefreshHashes(written)
		if len(written) == 1 {
			fmt.Printf("\n\x1b[38;5;111mFuzeCLI\x1b[0m\nWritten 1 file: %s\n", written[0])
		} else {
			fmt.Printf("\n\x1b[38;5;111mFuzeCLI\x1b[0m\nWritten %d files:\n", len(written))
			for _, path := range written {
				fmt.Printf("  ✓ %s\n", path)
			}
		}
		if plan.Explanation != "" {
			fmt.Printf("\n%s\n", plan.Explanation)
		}
	} else {
		fmt.Printf("\n\x1b[38;5;111mFuzeCLI\x1b[0m\n%s\n", response.String())
	}
	_ = start
	return a.Store.AddMessage(provider.Message{Role: "assistant", Content: response.String()})
}
