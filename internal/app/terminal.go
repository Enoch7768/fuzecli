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
	fmt.Println("\n\x1b[1;38;5;117mFuzeCLI SESSION\x1b[0m")
	fmt.Println("\x1b[38;5;244m────────────────────────────────────────────────────────────\x1b[0m")
	fmt.Printf("Provider: %s    Model: %s\n", providerName, model)
	fmt.Println("Your workspace is available to FuzeCLI, and no shell command will be executed without your action.")
	fmt.Println("\n[M] Continue with memory   [F] Start fresh")
	fmt.Print("\n\x1b[38;5;111mChoice\x1b[0m: ")
	for scanner.Scan() {
		choice := strings.ToLower(strings.TrimSpace(scanner.Text()))
		switch choice {
		case "m", "memory", "continue":
			fmt.Println("\x1b[38;5;244mMemory retained.\x1b[0m")
		case "f", "fresh", "clear", "new":
			if err := a.Store.ClearMemory(); err != nil {
				return fmt.Errorf("clear conversation memory: %w", err)
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
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	welcome, err := a.SessionWelcome(ctx, providerName, model)
	if err != nil {
		return fmt.Errorf("session welcome failed: %w", err)
	}
	fmt.Printf("\n%s\n", welcome)
	fmt.Println("\n\x1b[38;5;244mSession ready. Talk naturally. For project changes, FuzeCLI validates structured JSON before applying anything.\x1b[0m")
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

func formatTerminalError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("\x1b[38;5;214mError:\x1b[0m %s\n\x1b[38;5;244mDetails are intentionally shown so you can diagnose the problem. Use /status or /help for context.\x1b[0m", err.Error())
}

func (a *App) terminalStream(ctx context.Context, prompt, providerName, model string, attachments map[string]string) error {
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
	system := generation.StrictExecutionMode + "\n\nYou are FuzeCLI, a practical coding assistant for developers. The workspace context was read directly from the user's local workspace. Never ask the user to paste a file that exists there. Normal conversation may be natural language. If the model chooses structured output, use ONLY valid JSON with type=chat for ordinary conversation or type=edit for project changes. A chat envelope is {\"type\":\"chat\",\"response\":\"...\"}. An edit envelope is {\"type\":\"edit\",\"response\":\"\",\"files\":[{\"path\":\"relative/path.ext\",\"content\":\"full file content\",\"action\":\"create|modify|delete\"}],\"explanation\":\"brief explanation\",\"commands\":[]}. Never use markdown fences around structured JSON. Never return a request asking the user to provide source files that FuzeCLI already supplied. Generated commands are informational only and are never executed automatically."
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
		return fmt.Errorf("save user message: %w", err)
	}
	stream, err := a.Registry.Stream(ctx, name, msgs, provider.RequestOptions{Model: mdl, Temperature: 0.3, MaxTokens: 16000})
	if err != nil {
		return fmt.Errorf("provider request failed: %w", err)
	}

	var response strings.Builder
	var candidate strings.Builder
	jsonPossible := false
	var parsedJSON *generation.ChatResponse
	fmt.Print("\n")
	for chunk := range stream {
		if chunk.Error != nil {
			return fmt.Errorf("provider stream failed: %w", chunk.Error)
		}
		if chunk.Delta != "" {
			response.WriteString(chunk.Delta)
			trimmed := strings.TrimSpace(response.String())
			if !jsonPossible && generation.LooksLikeChatPlanPrefix(trimmed) {
				jsonPossible = true
			}
			if jsonPossible {
				candidate.WriteString(chunk.Delta)
				if structured, parseErr := generation.ParseChatResponse(candidate.String()); parseErr == nil {
					parsedJSON = &structured
					break
				}
				if candidate.Len() >= 512 && !strings.Contains(candidate.String(), `"files"`) && !strings.Contains(candidate.String(), `"response"`) && !strings.Contains(candidate.String(), `"explanation"`) {
					jsonPossible = false
					fmt.Print(candidate.String())
					candidate.Reset()
				}
			} else {
				fmt.Print(chunk.Delta)
			}
		}
		if chunk.Done {
			break
		}
	}

	if parsedJSON != nil {
		if parsedJSON.Type == "chat" {
			fmt.Printf("\r\x1b[K%s\n", parsedJSON.Response)
			if err := a.Store.AddMessage(provider.Message{Role: "assistant", Content: parsedJSON.Response}); err != nil {
				return fmt.Errorf("save assistant response: %w", err)
			}
			fmt.Println("\x1b[38;5;244mResponse complete · validated chat JSON.\x1b[0m")
			return nil
		}
		if parsedJSON.Plan == nil {
			return fmt.Errorf("validated edit response did not contain a file plan")
		}
		written, applyErr := generation.ApplyChatPlan(a.Store.Root, *parsedJSON.Plan)
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
		if len(parsedJSON.Plan.Files) == 1 {
			fmt.Printf("\n\x1b[38;5;111m✓ Applied\x1b[0m %s\n", parsedJSON.Plan.Files[0].Path)
		} else {
			fmt.Printf("\n\x1b[38;5;111m✓ Applied\x1b[0m %d files\n", len(parsedJSON.Plan.Files))
		}
		if parsedJSON.Explanation != "" {
			fmt.Printf("%s\n", parsedJSON.Explanation)
		}
		fmt.Println("\x1b[38;5;244mResponse complete · validated edit JSON.\x1b[0m")
		return a.Store.AddMessage(provider.Message{Role: "assistant", Content: parsedJSON.Explanation})
	}

	text := strings.TrimSpace(response.String())
	if text == "" {
		return fmt.Errorf("provider returned an empty response; no assistant content was received")
	}
	if jsonPossible {
		return fmt.Errorf("provider returned incomplete or invalid structured JSON; the response was not applied. Raw response length: %d bytes", len(response.String()))
	}
	fmt.Print("\n\x1b[38;5;244mResponse complete.\x1b[0m\n")
	return a.Store.AddMessage(provider.Message{Role: "assistant", Content: response.String()})
}
