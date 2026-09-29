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
	"unicode/utf8"

	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/provider"
)

const terminalAttachmentLimit = 65536

func (a *App) TerminalChat(ctx context.Context, _ bool) error {
	if a.Store == nil {
		return fmt.Errorf("workspace not initialized; run aicli init")
	}
	providerName, model, providerErr := a.ProviderAndModel("", "")
	if providerErr != nil {
		fmt.Println(formatTerminalError(providerErr))
		fmt.Println("\x1b[38;5;244mRun 'aicli setup' to configure a provider and model, then retry 'aicli chat'.\x1b[0m")
		return nil
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	billingMode, err := a.terminalSessionPreflight(ctx, providerName, model, scanner)
	if err != nil {
		return err
	}
	attachments := make(map[string]string)
	printTerminalWorkspace(providerName, model, a.Store.Root)
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
				fmt.Println("\n\x1b[38;5;244mSession closed. Your workspace remains untouched unless a change was explicitly applied.\x1b[0m")
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
				printTerminalWorkspace(providerName, model, a.Store.Root)
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
		if err := a.terminalStream(ctx, line, providerName, model, billingMode, attachments); err != nil {
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

func (a *App) terminalSessionPreflight(ctx context.Context, providerName, model string, scanner *bufio.Scanner) (string, error) {
	fmt.Println("\n\x1b[1;38;5;117mFuzeCLI SESSION\x1b[0m")
	fmt.Println("\x1b[38;5;244m────────────────────────────────────────────────────────────\x1b[0m")
	fmt.Printf("Provider: %s    Model: %s\n", providerName, model)
	fmt.Println("FuzeCLI uses conservative provider-aware request budgets and handles transient limits with retry and fallback.")
	fmt.Println("\n[M] Keep memory   [R] Refresh memory   [F] Start fresh")
	fmt.Print("\n\x1b[38;5;111mMemory choice\x1b[0m: ")
	for scanner.Scan() {
		choice := strings.ToLower(strings.TrimSpace(scanner.Text()))
		switch choice {
		case "m", "memory", "continue":
			fmt.Println("\x1b[38;5;244mMemory retained.\x1b[0m")
		case "r", "refresh", "reload":
			if _, err := a.Store.History(400); err != nil { return "", fmt.Errorf("refresh conversation memory: %w", err) }
			fmt.Println("\x1b[38;5;244mConversation memory refreshed from disk.\x1b[0m")
		case "f", "fresh", "clear", "new":
			if err := a.Store.ClearMemory(); err != nil { return "", fmt.Errorf("clear conversation memory: %w", err) }
			fmt.Println("\x1b[38;5;244mConversation memory cleared.\x1b[0m")
		default:
			fmt.Print("\x1b[38;5;214mChoose M, R, or F:\x1b[0m ")
			continue
		}
		break
	}
	if err := scanner.Err(); err != nil { return "", err }
	fmt.Println("\n\x1b[38;5;111mAPI access mode\x1b[0m")
	fmt.Println("[A] Automatic safety profile   [F] Free/low-quota profile   [P] Paid/API-key profile")
	fmt.Print("\x1b[38;5;111mChoice\x1b[0m: ")
	mode := "auto"
	for scanner.Scan() {
		choice := strings.ToLower(strings.TrimSpace(scanner.Text()))
		switch choice {
		case "a", "auto": mode = "auto"
		case "f", "free": mode = "free"
		case "p", "paid", "apikey": mode = "paid"
		default:
			fmt.Print("\x1b[38;5;214mChoose A, F, or P:\x1b[0m")
			continue
		}
		break
	}
	if err := scanner.Err(); err != nil { return "", err }
	fmt.Printf("\x1b[38;5;244mSafety profile: %s\x1b[0m\n", mode)
	select { case <-ctx.Done(): return "", ctx.Err(); default: }
	welcome, err := a.SessionWelcome(ctx, providerName, model)
	if err != nil { return "", fmt.Errorf("session welcome failed: %w", err) }
	fmt.Printf("\n%s\n", welcome)
	fmt.Println("\n\x1b[38;5;244mSession ready. Structured responses are validated locally before files are applied.\x1b[0m")
	return mode, nil
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
	fmt.Println("\x1b[38;5;244mChat · /file · /provider · /model · /status · /clear · /help · /exit\x1b[0m")
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
	fmt.Println("  /help                 Show this help")
	fmt.Println("  /exit                 Close the session")
	fmt.Println()
	fmt.Println("Normal conversation may be returned as natural language or as a validated JSON chat envelope.")
	fmt.Println("Project changes use a validated JSON edit envelope before files are applied.")
	fmt.Println("Model-provided shell commands are informational only and are never executed automatically.")
}

func printTerminalStatus(providerName, model, root string) {
	fmt.Println("\n\x1b[1mRuntime\x1b[0m")
	fmt.Printf("  Provider  %s\n", providerName)
	fmt.Printf("  Model     %s\n", model)
	fmt.Printf("  Workspace %s\n", root)
	fmt.Println("  JSON      strict parser enabled")
	fmt.Println("  Commands  never executed automatically")
}

type terminalProgress struct {
	last int
}

func newTerminalProgress() *terminalProgress {
	return &terminalProgress{}
}

func (p *terminalProgress) update(percent int, label string) {
	if percent <= p.last {
		return
	}
	if percent > 100 {
		percent = 100
	}
	p.last = percent
	width := 28
	filled := percent * width / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	fmt.Printf("\r\x1b[K\x1b[38;5;111m[%s]\x1b[0m %3d%%  %s", bar, percent, label)
}

func (p *terminalProgress) finish() {
	p.update(100, "Complete")
	fmt.Print("\n")
}

func formatTerminalError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("\x1b[38;5;214mError:\x1b[0m %s\n\x1b[38;5;244mDetails are intentionally shown so you can diagnose the problem. Use /status or /help for context.\x1b[0m", err.Error())
}

func (a *App) terminalStream(ctx context.Context, prompt, providerName, model, billingMode string, attachments map[string]string) error {
	history, err := a.Store.History(400)
	if err != nil {
		return fmt.Errorf("load chat history: %w", err)
	}
	name, mdl, err := a.ProviderAndModel(providerName, model)
	if err != nil {
		return fmt.Errorf("resolve provider/model: %w", err)
	}
	history, err = a.compactHistory(ctx, name, mdl, history)
	if err != nil {
		return fmt.Errorf("prepare chat history: %w", err)
	}
	workspaceContext, err := a.Store.WorkspaceContext()
	if err != nil {
		return fmt.Errorf("read workspace context: %w", err)
	}
	system := generation.SessionSystemPrompt() + "\n\nSTRUCTURED RESPONSE CONTRACT: Return one JSON object for every request. For conversation use type=chat with response (or compatible message/content/text). For workspace changes use type=edit with plan.files containing path, action, and complete content; include explanation when useful. Never truncate file content. The parser accepts compatible field aliases, but prefer the canonical contract.\n\nYou are FuzeCLI, a practical coding assistant for developers. The workspace context was read directly from the user's local workspace. Never ask the user to paste a file that exists there. Never return a request asking the user to provide source files that FuzeCLI already supplied. Generated commands are informational only and are never executed automatically."
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
	engine := generation.Engine{Registry: a.Registry, Profile: &a.Profile, MaxContextChars: 240000}
	msgs := engine.Messages(a.Profile.Condensed(), workspaceContext, history, prompt)
	msgs[0].Content = system
	if err := a.Store.AddMessage(provider.Message{Role: "user", Content: prompt}); err != nil {
		return fmt.Errorf("save user message: %w", err)
	}

	fmt.Printf("\n%sYou%s\n%s\n\n%sFuze%s\n", uiBold, uiReset, prompt, uiAccent, uiReset)
	progress := newTerminalProgress()
	progress.update(8, "Connecting")
	defer progress.finish()

	opts := provider.RequestOptions{Model: mdl, Temperature: 0.3, MaxTokens: 32768, JSONMode: true, JSONSchema: generation.ChatResponseSchema(), BillingMode: billingMode}
	stream, streamErr := a.Registry.Stream(ctx, name, msgs, opts)
	var response strings.Builder
	var usage provider.Usage
	if streamErr == nil {
		for chunk := range stream {
			if chunk.Error != nil {
				return fmt.Errorf("provider stream failed: %w", chunk.Error)
			}
			if chunk.Delta != "" {
				response.WriteString(chunk.Delta)
			}
			usage.PromptTokens += chunk.Usage.PromptTokens
			usage.CompletionTokens += chunk.Usage.CompletionTokens
			usage.TotalTokens += chunk.Usage.TotalTokens
			usage.CostUSD += chunk.Usage.CostUSD
			percent := 18
			if response.Len() > 8192 { percent = 45 }
			if response.Len() > 32768 { percent = 68 }
			progress.update(percent, "Generating response")
		}
	} else {
		progress.update(45, "Retrying request")
		resp, err := a.Registry.Send(ctx, name, msgs, opts)
		if err != nil {
			return fmt.Errorf("provider request failed: %w", err)
		}
		response.WriteString(resp.Content)
		usage = resp.Usage
	}

	progress.update(82, "Validating response")
	raw := strings.TrimSpace(response.String())
	if raw == "" {
		return fmt.Errorf("provider returned an empty response; no assistant content was received")
	}
	parsed, parseErr := generation.ParseChatResponse(raw)
	if parseErr != nil {
		return fmt.Errorf("complete assistant response could not be validated: %w", parseErr)
	}

	if parsed.Type == "chat" {
		content := parsed.Response
		if content == "" { content = parsed.Message }
		if content == "" { content = parsed.Explanation }
		if strings.TrimSpace(content) == "" {
			return fmt.Errorf("validated chat response did not contain assistant content")
		}
		fmt.Printf("%s%s%s\n", uiTextLabel(""), content, uiReset)
		if err := a.Store.AddMessage(provider.Message{Role: "assistant", Content: content}); err != nil {
			return fmt.Errorf("save assistant response: %w", err)
		}
		_ = usage
		return nil
	}

	if parsed.Plan == nil {
		return fmt.Errorf("validated edit response did not contain a file plan")
	}
	written, applyErr := generation.ApplyChatPlan(a.Store.Root, *parsed.Plan)
	if applyErr != nil {
		return fmt.Errorf("validated edit could not be applied: %w", applyErr)
	}
	if err := a.Store.MarkTouched(written); err != nil {
		return fmt.Errorf("record changed files: %w", err)
	}
	st, _ := a.Store.LoadState()
	st.ActiveProvider = name
	st.ActiveModel = mdl
	if err := a.Store.SaveState(st); err != nil {
		return fmt.Errorf("save session state: %w", err)
	}
	if err := a.Store.RefreshHashes(written); err != nil {
		return fmt.Errorf("refresh workspace hashes: %w", err)
	}
	if len(written) == 1 {
		fmt.Printf("%s✓ Applied%s %s\n", uiGreen, uiReset, written[0])
	} else {
		fmt.Printf("%s✓ Applied%s %d files\n", uiGreen, uiReset, len(written))
	}
	if parsed.Explanation != "" {
		fmt.Printf("%s%s%s\n", uiMuted, parsed.Explanation, uiReset)
	}
	return a.Store.AddMessage(provider.Message{Role: "assistant", Content: parsed.Explanation})
}

