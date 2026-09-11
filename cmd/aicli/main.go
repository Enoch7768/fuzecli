package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/api"
	"github.com/Enoch7768/fuzecli/internal/app"
	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/mcpserver"
	"github.com/Enoch7768/fuzecli/internal/profile"
)

var version = "Revision 2.3"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "\nFuzeCLI could not complete that request.")
		fmt.Fprintln(os.Stderr, "Reason:", err)
		fmt.Fprintln(os.Stderr, "\nUseful next steps:")
		fmt.Fprintln(os.Stderr, "  aicli doctor    Diagnose provider/workspace problems")
		fmt.Fprintln(os.Stderr, "  aicli setup     Configure your provider and model")
		fmt.Fprintln(os.Stderr, "  aicli --help   Show all commands")
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return entryScreen()
	}
	switch args[0] {
	case "version", "--version":
		fmt.Println("FuzeCLI", version)
		return nil
	case "init":
		return app.InitWorkspace(".")
	case "setup":
		return setupCommand()
	case "config":
		return configCommand(args[1:])
	case "models":
		return modelsCommand(args[1:])
	case "profile":
		return profileCommand(args[1:])
	case "history":
		return historyCommand()
	case "ask":
		return askCommand(args[1:])
	case "chat":
		return chatCommand(args[1:])
	case "api":
		return apiCommand(args[1:])
	case "mcp":
		return mcpCommand()
	case "doctor":
		return doctorCommand()
	case "snapshot":
		return snapshotCommand()
	case "restore":
		return restoreCommand(args[1:])
	case "status":
		return gitStatusCommand()
	case "diff":
		return gitDiffCommand()
	case "help", "--help", "-h":
		return usage()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func entryScreen() error {
	fmt.Println()
	fmt.Println("\x1b[1;38;5;117m  F U Z E C L I\x1b[0m")
	fmt.Println("\x1b[38;5;244m  AI coding workspace for developers\x1b[0m")
	fmt.Println("\x1b[38;5;239m  ────────────────────────────────────────────────────────────\x1b[0m")
	fmt.Println()
	fmt.Println("  Build, inspect, debug and change your project from one terminal.")
	fmt.Println("  FuzeCLI keeps the developer in control: generated commands are never")
	fmt.Println("  executed automatically, workspace paths are validated, and changes")
	fmt.Println("  can be reviewed and recovered locally.")
	fmt.Println()
	fmt.Println("\x1b[1m  Get started\x1b[0m")
	fmt.Println("    aicli setup        Configure your AI provider")
	fmt.Println("    aicli init         Initialize this workspace")
	fmt.Println("    aicli chat         Start an interactive coding session")
	fmt.Println("    aicli doctor       Check your environment")
	fmt.Println()
	fmt.Println("  Already configured?")
	fmt.Println("    aicli chat         Jump straight into your workspace")
	fmt.Println("    aicli ask \"...\"   Run a one-shot request")
	fmt.Println()
	fmt.Println("  Tip: run 'aicli --help' for the complete command reference.")
	return nil
}

func setupCommand() error {
	reader := bufio.NewReader(os.Stdin)
	current, err := config.Load()
	if err != nil {
		return err
	}

	fmt.Println("\n\x1b[1;38;5;117mFuzeCLI SETUP\x1b[0m")
	fmt.Println("\x1b[38;5;244mConfigure the provider FuzeCLI should use by default.\x1b[0m")
	fmt.Println("\x1b[38;5;244mYour API key is stored locally in the FuzeCLI config directory.\x1b[0m")
	fmt.Println()

	fmt.Printf("Provider [%s] (gemini/openai/groq/anthropic/llamacpp): ", current.DefaultProvider)
	providerName, err := readSetupLine(reader)
	if err != nil {
		return err
	}
	if providerName == "" {
		providerName = current.DefaultProvider
	}
	if _, ok := current.Providers[providerName]; !ok {
		return fmt.Errorf("unknown provider %q; choose gemini, openai, groq, anthropic, or llamacpp", providerName)
	}

	providerConfig := current.Providers[providerName]
	fmt.Printf("Model [%s]: ", providerConfig.DefaultModel)
	model, err := readSetupLine(reader)
	if err != nil {
		return err
	}
	if model != "" {
		providerConfig.DefaultModel = model
	}

	if providerName != "llamacpp" {
		fmt.Print("API key (leave blank to keep the current key): ")
		key, err := readSetupLine(reader)
		if err != nil {
			return err
		}
		if key != "" {
			providerConfig.APIKey = key
		}
	}

	current.Providers[providerName] = providerConfig
	current.DefaultProvider = providerName
	if err := config.Save(current); err != nil {
		return err
	}
	if err := app.InitWorkspace("."); err != nil {
		return err
	}

	fmt.Println("\n\x1b[38;5;111m✓ Setup saved.\x1b[0m")
	fmt.Printf("  Provider: %s\n  Model:    %s\n", providerName, providerConfig.DefaultModel)
	fmt.Println("\nNext steps:")
	fmt.Println("  aicli doctor   Verify the provider and workspace")
	fmt.Println("  aicli chat     Start coding with FuzeCLI")
	return nil
}

func readSetupLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func askCommand(args []string) error {
	var providerName, model string
	var yes bool
	var promptParts []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --provider")
			}
			providerName = args[i+1]
			i++
		case "--model":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --model")
			}
			model = args[i+1]
			i++
		case "--yes":
			yes = true
		case "--help", "-h":
			fmt.Println(`Usage:
  aicli ask "prompt" [--provider name|auto] [--model name] [--yes]`)
			return nil
		default:
			promptParts = append(promptParts, args[i])
		}
	}
	prompt, err := app.ReadPrompt(promptParts)
	if err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("ask requires a prompt or stdin input")
	}
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		a.RunProfileExtraction(ctx)
	}()
	_, err = a.Ask(context.Background(), prompt, providerName, model, yes)
	return err
}

func chatCommand(args []string) error {
	var yes bool
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--yes":
			yes = true
		case "--help", "-h":
			fmt.Println(`Usage:
  aicli chat [--yes]

Commands inside chat:
  /file
  /file <relative-path>
  /file list
  /file clear
  /provider <name>
  /model <name>
  /status
  /clear
  /help
  /exit`)
			return nil
		default:
			return fmt.Errorf("unknown chat option %q", args[i])
		}
	}
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return a.TerminalChat(context.Background(), yes)
}

func apiCommand(args []string) error {
	addr := "127.0.0.1:8787"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--addr":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --addr")
			}
			addr = args[i+1]
			i++
		case "--help", "-h":
			fmt.Println("Usage: aicli api [--addr 127.0.0.1:8787]")
			return nil
		default:
			return fmt.Errorf("unknown api option %q", args[i])
		}
	}
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	server := api.NewServer(api.NewService(a), api.TokenFromEnvironment())
	fmt.Printf("FuzeCLI API listening on http://%s\n", addr)
	return server.ListenAndServe(addr)
}

func mcpCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return mcpserver.Run(context.Background(), a)
}

func doctorCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return a.Doctor()
}

func snapshotCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	paths, err := a.Store.Touched()
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no touched files are available for a snapshot")
	}
	snapshot, err := a.Store.CreateSnapshot(paths)
	if err != nil {
		return err
	}
	fmt.Println("Snapshot created:", snapshot)
	return nil
}

func restoreCommand(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: aicli restore [snapshot.zip]")
	}
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	snapshot := ""
	if len(args) == 1 {
		snapshot = args[0]
	} else {
		snapshot, err = a.Store.LatestSnapshot()
		if err != nil {
			return err
		}
	}
	paths, err := a.Store.RestoreSnapshot(snapshot)
	if err != nil {
		return err
	}
	fmt.Printf("Restored %d file(s) from %s\n", len(paths), snapshot)
	return nil
}

func gitStatusCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return app.GitStatus(a.Store.Root)
}

func gitDiffCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return app.GitDiff(a.Store.Root)
}

func modelsCommand(args []string) error {
	providerName := "gemini"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --provider")
			}
			providerName = args[i+1]
			i++
		case "--help", "-h":
			fmt.Println("Usage: aicli models [--provider name]")
			return nil
		default:
			return fmt.Errorf("unknown models option %q", args[i])
		}
	}
	if providerName == "auto" {
		return fmt.Errorf("models requires a specific provider")
	}
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	models, err := a.Registry.ListModels(context.Background(), providerName)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		fmt.Printf("No models available for %s.\n", providerName)
		return nil
	}
	fmt.Printf("%s models:\n", providerName)
	for _, model := range models {
		fmt.Println("  " + model)
	}
	return nil
}

func historyCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return app.ShowHistory(".")
}

func configCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("config requires set or show")
	}
	switch args[0] {
	case "set":
		if len(args) != 3 {
			return fmt.Errorf("usage: aicli config set <key> <value>")
		}
		return config.Set(args[1], args[2])
	case "show":
		c, err := config.Load()
		if err != nil {
			return err
		}
		names := []string{"openai", "gemini", "groq", "anthropic", "llamacpp"}
		fmt.Println("Default provider:", c.DefaultProvider)
		for _, name := range names {
			if p, ok := c.Providers[name]; ok {
				key := "<not set>"
				if p.APIKey != "" {
					key = "<configured>"
				}
				fmt.Printf("%s: model=%s api_key=%s\n", name, p.DefaultModel, key)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func profileCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("profile requires show or extract")
	}
	switch args[0] {
	case "show":
		p, err := profile.Load(".")
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	case "extract":
		a, err := app.Load()
		if err != nil {
			return err
		}
		defer a.Close()
		if err := a.AttachWorkspace("."); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return a.RunProfileExtraction(ctx)
	default:
		return fmt.Errorf("unknown profile command %q", args[0])
	}
}
