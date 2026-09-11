package main

import (
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
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "version", "--version":
		fmt.Println("FuzeCLI", version)
		return nil
	case "init":
		return app.InitWorkspace(".")
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
	case "help", "--help", "-h":
		return usage()
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usageText)
	}
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
		names := []string{"openai", "gemini", "gemini-normalizer", "groq", "anthropic", "llamacpp"}
		for _, name := range names {
			p, ok := c.Providers[name]
			if !ok {
				continue
			}
			fmt.Printf("providers.%s.api_key: %s\n", name, config.MaskSecret(p.APIKey))
			fmt.Printf("providers.%s.default_model: %s\n", name, p.DefaultModel)
			if p.BaseURL != "" {
				fmt.Printf("providers.%s.base_url: %s\n", name, p.BaseURL)
			}
		}
		fmt.Println("default_provider:", c.DefaultProvider)
		fmt.Println("fallback_order:", strings.Join(c.FallbackOrder, ", "))
		fmt.Println("verification.self_correction_attempts:", c.Verification.SelfCorrectionAttempts)
		return nil
	case "--help", "-h":
		fmt.Println("Usage: aicli config set <key> <value>\naicli config show")
		return nil
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func profileCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("profile requires show or reset")
	}
	switch args[0] {
	case "show":
		p, err := profile.Load()
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	case "reset":
		return profile.Save(profile.Default())
	case "--help", "-h":
		fmt.Println("Usage: aicli profile show\naicli profile reset")
		return nil
	default:
		return fmt.Errorf("unknown profile command %q", args[0])
	}
}

func usage() error {
	fmt.Println(usageText)
	return nil
}

const usageText = `FuzeCLI - unified AI coding workspace

Commands:
  aicli init
  aicli config set <key> <value>
  aicli config show
  aicli models [--provider name]
  aicli chat [--yes]
  aicli ask "prompt" [--provider name|auto] [--model name] [--yes]
  aicli api [--addr 127.0.0.1:8787]
  aicli mcp
  aicli doctor
  aicli snapshot
  aicli restore [snapshot.zip]
  aicli profile show
  aicli profile reset
  aicli history
  aicli version

Safety:
  .aicliignore adds project-specific exclusions.
  Known secret-bearing files are always excluded from AI workspace context.
  snapshot/restore provides explicit local recovery points.

Chat commands:
  /file
  /file <relative-path>
  /file list
  /file clear
  /provider <name>
  /model <name>
  /status
  /clear
  /help
  /exit

Options:
  --help
  --version`
