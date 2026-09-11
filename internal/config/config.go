package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/provider"
)

type ProviderConfig struct {
	APIKey       string `yaml:"api_key,omitempty"`
	DefaultModel string `yaml:"default_model,omitempty"`
	BaseURL      string `yaml:"base_url,omitempty"`
}

type Config struct {
	DefaultProvider string                    `yaml:"default_provider"`
	Providers       map[string]ProviderConfig `yaml:"providers"`
	FallbackOrder   []string                  `yaml:"fallback_order"`
	Verification    VerificationConfig        `yaml:"verification"`
}

type VerificationConfig struct {
	SelfCorrectionAttempts int `yaml:"self_correction_attempts"`
}

func Default() Config {
	providers := map[string]ProviderConfig{
		"openai": {DefaultModel: "gpt-4o"},
		"gemini": {DefaultModel: "gemini-2.5-flash"},
		"groq": {DefaultModel: "openai/gpt-oss-20b"},
		"anthropic": {DefaultModel: "claude-sonnet-4-6"},
		"llamacpp": {BaseURL: "http://localhost:8080", DefaultModel: "local"},
	}
	for _, name := range provider.CompatibleProviderNames() {
		if _, ok := providers[name]; !ok {
			providers[name] = ProviderConfig{DefaultModel: "auto"}
		}
	}
	return Config{
		DefaultProvider: "gemini",
		Providers:       providers,
		FallbackOrder:   []string{"gemini", "groq", "openai", "llamacpp"},
		Verification:    VerificationConfig{SelfCorrectionAttempts: 2},
	}
}

func Dir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "aicli")
}

func Path() string {
	return filepath.Join(Dir(), "config.yaml")
}

func ProfilePath() string {
	return filepath.Join(Dir(), "profile.json")
}

func Load() (Config, error) {
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		c := Default()
		if err := Save(c); err != nil {
			return Config{}, err
		}
		syncProviderConfiguration(c)
		return c, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	c := Default()
	if err := parseYAML(string(b), &c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	syncProviderConfiguration(c)
	return c, nil
}

func syncProviderConfiguration(c Config) {
	for name, cfg := range c.Providers {
		provider.Configure(name, cfg.APIKey, cfg.BaseURL)
	}
}

func Save(c Config) error {
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data := renderYAML(c)
	tempPath := Path() + ".tmp"
	if err := os.WriteFile(tempPath, []byte(data), 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tempPath, Path()); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace config: %w", err)
	}
	syncProviderConfiguration(c)
	return nil
}

func Set(key, value string) error {
	c, err := Load()
	if err != nil {
		return err
	}
	parts := strings.Split(key, ".")
	switch {
	case len(parts) == 1 && parts[0] == "default_provider":
		c.DefaultProvider = value
	case len(parts) == 1 && parts[0] == "fallback_order":
		c.FallbackOrder = splitList(value)
	case len(parts) == 2 && parts[0] == "verification" && parts[1] == "self_correction_attempts":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid self-correction attempts: %w", err)
		}
		c.Verification.SelfCorrectionAttempts = n
	case len(parts) == 2 && parts[0] == "providers":
		name := parts[1]
		pc, ok := c.Providers[name]
		if !ok {
			pc = ProviderConfig{}
		}
		if value == "" {
			return fmt.Errorf("value is required")
		}
		pc.APIKey = value
		c.Providers[name] = pc
	case len(parts) == 2 && strings.HasPrefix(parts[0], "provider"):
		return fmt.Errorf("unknown configuration key: %s", key)
	case len(parts) == 3 && parts[0] == "providers" && parts[2] == "api_key":
		pc := c.Providers[parts[1]]
		pc.APIKey = value
		c.Providers[parts[1]] = pc
	case len(parts) == 3 && parts[0] == "providers" && parts[2] == "default_model":
		pc := c.Providers[parts[1]]
		pc.DefaultModel = value
		c.Providers[parts[1]] = pc
	case len(parts) == 3 && parts[0] == "providers" && parts[2] == "base_url":
		pc := c.Providers[parts[1]]
		pc.BaseURL = value
		c.Providers[parts[1]] = pc
	default:
		return fmt.Errorf("unknown configuration key: %s", key)
	}
	return Save(c)
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func MaskSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "****"
	}
	return "*****" + value[len(value)-4:]
}

func renderYAML(c Config) string {
	var b strings.Builder
	b.WriteString("default_provider: " + c.DefaultProvider + "\n")
	b.WriteString("providers:\n")
	names := make([]string, 0, len(c.Providers))
	for name := range c.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := c.Providers[name]
		b.WriteString("  " + name + ":\n")
		if p.APIKey != "" {
			b.WriteString("    api_key: " + yamlQuote(p.APIKey) + "\n")
		}
		if p.DefaultModel != "" {
			b.WriteString("    default_model: " + yamlQuote(p.DefaultModel) + "\n")
		}
		if p.BaseURL != "" {
			b.WriteString("    base_url: " + yamlQuote(p.BaseURL) + "\n")
		}
	}
	b.WriteString("fallback_order: [" + strings.Join(c.FallbackOrder, ", ") + "]\n")
	b.WriteString("verification:\n  self_correction_attempts: " + strconv.Itoa(c.Verification.SelfCorrectionAttempts) + "\n")
	return b.String()
}

func yamlQuote(value string) string {
	if value == "" {
		return "\"\""
	}
	if strings.IndexAny(value, ":#[]{}&*!|>'\"%@`\n\r\t, ") >= 0 {
		return strconv.Quote(value)
	}
	return value
}

func parseYAML(data string, c *Config) error {
	lines := strings.Split(data, "\n")
	section := ""
	providerName := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if indent == 0 {
			providerName = ""
			if strings.HasPrefix(line, "default_provider:") {
				c.DefaultProvider = strings.TrimSpace(strings.TrimPrefix(line, "default_provider:"))
				continue
			}
			if strings.HasPrefix(line, "fallback_order:") {
				value := strings.TrimSpace(strings.TrimPrefix(line, "fallback_order:"))
				value = strings.TrimPrefix(value, "[")
				value = strings.TrimSuffix(value, "]")
				c.FallbackOrder = splitList(value)
				continue
			}
			if line == "providers:" {
				section = "providers"
				continue
			}
			if line == "verification:" {
				section = "verification"
				continue
			}
			continue
		}
		if section == "providers" && indent == 2 && strings.HasSuffix(line, ":") {
			providerName = strings.TrimSuffix(line, ":")
			if _, ok := c.Providers[providerName]; !ok {
				c.Providers[providerName] = ProviderConfig{}
			}
			continue
		}
		if section == "providers" && indent >= 4 && providerName != "" {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			p := c.Providers[providerName]
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, "\"")
			switch key {
			case "api_key":
				p.APIKey = value
			case "default_model":
				p.DefaultModel = value
			case "base_url":
				p.BaseURL = value
			}
			c.Providers[providerName] = p
			continue
		}
		if section == "verification" && indent >= 2 && strings.HasPrefix(line, "self_correction_attempts:") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "self_correction_attempts:")))
			if err != nil {
				return fmt.Errorf("invalid self-correction attempts: %w", err)
			}
			c.Verification.SelfCorrectionAttempts = n
		}
	}
	return nil
}

func init() {
	_ = time.Now()
}
