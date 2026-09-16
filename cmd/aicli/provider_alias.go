package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/provider"
)

func init() {
	if len(os.Args) >= 3 && os.Args[1] == "provider" {
		switch os.Args[2] {
		case "setup":
			if err := providerSetupCommand(); err != nil {
				fmt.Fprintln(os.Stderr, "provider setup:", err)
				os.Exit(1)
			}
			os.Exit(0)
		case "key":
			if len(os.Args) >= 4 && (os.Args[3] == "gemini-json" || os.Args[3] == "gemini-normalizer") {
				if err := geminiJSONKeyCommand(); err != nil {
					fmt.Fprintln(os.Stderr, "provider key:", err)
					os.Exit(1)
				}
				os.Exit(0)
			}
		}
	}
}

func providerSetupCommand() error {
	current, err := config.Load()
	if err != nil {
		return err
	}

	names := provider.CompatibleProviderNames()
	for _, name := range []string{"openai", "gemini", "gemini-normalizer", "groq", "anthropic", "llamacpp"} {
		found := false
		for _, existing := range names {
			if existing == name {
				found = true
				break
			}
		}
		if !found {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	fmt.Println()
	fmt.Println("FuzeCLI Provider Setup")
	fmt.Println("Select a provider. API keys are entered interactively and stored in the local FuzeCLI config.")
	fmt.Println()
	for i, name := range names {
		marker := " "
		if cfg, ok := current.Providers[name]; ok && cfg.APIKey != "" {
			marker = "✓"
		}
		label := name
		if name == "gemini-normalizer" {
			label = "gemini-normalizer  (Gemini JSON fixer / Gemini 3.6 Flash)"
		}
		fmt.Printf("  %2d. [%s] %s\n", i+1, marker, label)
	}
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Provider number: ")
	selection, err := readProviderLine(reader)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(selection)
	if err != nil || n < 1 || n > len(names) {
		return fmt.Errorf("invalid provider selection %q", selection)
	}
	name := names[n-1]
	cfg := current.Providers[name]

	fmt.Printf("\nSelected: %s\n", name)
	if name != "llamacpp" && name != "ollama" && name != "vllm" && name != "text-generation-inference" && name != "lmstudio" && name != "jan" && name != "litellm" {
		fmt.Print("API key (leave blank to keep current): ")
		key, err := readProviderLine(reader)
		if err != nil {
			return err
		}
		if key != "" {
			cfg.APIKey = key
		}
	}

	fmt.Printf("Model [%s]: ", cfg.DefaultModel)
	model, err := readProviderLine(reader)
	if err != nil {
		return err
	}
	if model != "" {
		cfg.DefaultModel = model
	}

	current.Providers[name] = cfg
	current.DefaultProvider = name
	if err := config.Save(current); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("✓ Provider configuration saved.")
	fmt.Printf("  Provider: %s\n  Model:    %s\n", name, cfg.DefaultModel)
	if cfg.APIKey != "" {
		fmt.Printf("  API key:  %s\n", config.MaskSecret(cfg.APIKey))
	}
	if name == "gemini-normalizer" {
		fmt.Println("  Role:     Dedicated structured-JSON repair engine")
	}
	return nil
}

func geminiJSONKeyCommand() error {
	current, err := config.Load()
	if err != nil {
		return err
	}
	cfg := current.Providers["gemini-normalizer"]
	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("FuzeCLI Gemini JSON Fixer")
	fmt.Println("This key is used by the dedicated Gemini 3.6 Flash structured-JSON repair engine.")
	fmt.Println("It is separate from your normal chat provider configuration.")
	fmt.Print("Gemini API key: ")
	key, err := readProviderLine(reader)
	if err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("API key cannot be empty")
	}
	cfg.APIKey = strings.TrimSpace(key)
	cfg.DefaultModel = "gemini-3.6-flash"
	current.Providers["gemini-normalizer"] = cfg
	if err := config.Save(current); err != nil {
		return err
	}
	fmt.Printf("✓ Gemini JSON fixer key saved: %s\n", config.MaskSecret(cfg.APIKey))
	fmt.Println("  Model: gemini-3.6-flash")
	return nil
}

func readProviderLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
