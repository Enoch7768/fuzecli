package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"fuzecli/internal/app"
	"fuzecli/internal/config"
	"fuzecli/internal/profile"
)

var version = "dev"

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
	case "profile":
		return profileCommand(args[1:])
	case "history":
		a, err := app.Load()
		if err != nil {
			return err
		}
		defer a.Close()
		if err := a.AttachWorkspace("."); err != nil {
			return err
		}
		return app.ShowHistory(".")
	case "ask":
		return askCommand(args[1:])
	case "chat":
		return chatCommand(args[1:])
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usageText)
	}
}

func askCommand(args []string) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	providerName := fs.String("provider", "", "provider name or auto")
	model := fs.String("model", "", "model override")
	yes := fs.Bool("yes", false, "apply changes without confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prompt, err := app.ReadPrompt(fs.Args())
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
	_, err = a.Ask(context.Background(), prompt, *providerName, *model, *yes)
	return err
}
func chatCommand(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "apply /code changes without confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.AttachWorkspace("."); err != nil {
		return err
	}
	return a.Chat(context.Background(), *yes)
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
		for name, p := range c.Providers {
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
		b, _ := json.MarshalIndent(p, "", "  ")
		fmt.Println(string(b))
		return nil
	case "reset":
		return profile.Save(profile.Default())
	default:
		return fmt.Errorf("unknown profile command %q", args[0])
	}
}
func usage() error { fmt.Println(usageText); return nil }

const usageText = `FuzeCLI - unified AI coding CLI

Commands:
  aicli init
  aicli config set <key> <value>
  aicli config show
  aicli chat [--yes]
  aicli ask "prompt" [--provider name|auto] [--model name] [--yes]
  aicli profile show
  aicli profile reset
  aicli history
  aicli version`
