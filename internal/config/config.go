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
		"gemini-normalizer": {DefaultModel: "gemini-2.5-flash"},
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

func Path() string { return filepath.Join(Dir(), "config.yaml") }
func ProfilePath() string { return filepath.Join(Dir(), "profile.json") }

func Load() (Config, error) {
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		c := Default()
		if err := Save(c); err != nil { return Config{}, err }
		syncProviderConfiguration(c)
		return c, nil
	}
	if err != nil { return Config{}, fmt.Errorf("read config: %w", err) }
	c := Default()
	if err := parseYAML(string(b), &c); err != nil { return Config{}, fmt.Errorf("parse config: %w", err) }
	syncProviderConfiguration(c)
	return c, nil
}

func syncProviderConfiguration(c Config) {
	for name, cfg := range c.Providers { provider.Configure(name, cfg.APIKey, cfg.BaseURL) }
}

func Save(c Config) error {
	if err := os.MkdirAll(Dir(), 0700); err != nil { return fmt.Errorf("create config directory: %w", err) }
	data := renderYAML(c)
	tempPath := Path() + ".tmp"
	if err := os.WriteFile(tempPath, []byte(data), 0600); err != nil { return fmt.Errorf("write config: %w", err) }
	if err := os.Rename(tempPath, Path()); err != nil { _ = os.Remove(tempPath); return fmt.Errorf("replace config: %w", err) }
	syncProviderConfiguration(c)
	return nil
}

func Set(key, value string) error {
	c, err := Load(); if err != nil { return err }
	parts := strings.Split(key, ".")
	switch {
	case key == "default_provider":
		if value == "" { return fmt.Errorf("default_provider cannot be empty") }; c.DefaultProvider = value
	case key == "fallback_order":
		order := parseList(value); if len(order) == 0 { return fmt.Errorf("fallback_order cannot be empty") }; c.FallbackOrder = order
	case len(parts) == 3 && parts[0] == "providers":
		name, field := parts[1], parts[2]; p := c.Providers[name]
		switch field { case "api_key": p.APIKey = value; case "default_model": p.DefaultModel = value; case "base_url": p.BaseURL = value; default: return fmt.Errorf("unsupported config field %q", field) }
		c.Providers[name] = p
	case key == "verification.self_correction_attempts":
		n, err := strconv.Atoi(value); if err != nil || n < 0 { return fmt.Errorf("invalid self-correction attempt count %q", value) }; c.Verification.SelfCorrectionAttempts = n
	default: return fmt.Errorf("unsupported config key %q", key)
	}
	return Save(c)
}

func MaskSecret(s string) string { if s == "" { return "" }; if len(s) <= 4 { return strings.Repeat("*", len(s)) }; return strings.Repeat("*", len(s)-4) + s[len(s)-4:] }
func Timestamp() string { return time.Now().UTC().Format(time.RFC3339) }

func renderYAML(c Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "default_provider: %s\n", yamlScalar(c.DefaultProvider))
	b.WriteString("providers:\n")
	names := []string{"openai", "gemini", "gemini-normalizer", "groq", "anthropic", "llamacpp"}
	seen := map[string]bool{}
	for _, name := range names {
		p, ok := c.Providers[name]; if !ok { continue }; seen[name] = true
		fmt.Fprintf(&b, "  %s:\n", name)
		if p.APIKey != "" { fmt.Fprintf(&b, "    api_key: %s\n", yamlScalar(p.APIKey)) }
		if p.DefaultModel != "" { fmt.Fprintf(&b, "    default_model: %s\n", yamlScalar(p.DefaultModel)) }
		if p.BaseURL != "" { fmt.Fprintf(&b, "    base_url: %s\n", yamlScalar(p.BaseURL)) }
	}
	otherNames := make([]string, 0, len(c.Providers))
	for name := range c.Providers { if !seen[name] { otherNames = append(otherNames, name) } }
	sort.Strings(otherNames)
	for _, name := range otherNames { p := c.Providers[name]; fmt.Fprintf(&b, "  %s:\n", name); if p.APIKey != "" { fmt.Fprintf(&b, "    api_key: %s\n", yamlScalar(p.APIKey)) }; if p.DefaultModel != "" { fmt.Fprintf(&b, "    default_model: %s\n", yamlScalar(p.DefaultModel)) }; if p.BaseURL != "" { fmt.Fprintf(&b, "    base_url: %s\n", yamlScalar(p.BaseURL)) } }
	order := make([]string, len(c.FallbackOrder); for i, providerName := range c.FallbackOrder { order[i] = yamlScalar(providerName) }
	fmt.Fprintf(&b, "fallback_order: [%s]\n", strings.Join(order, ", "))
	fmt.Fprintf(&b, "verification:\n  self_correction_attempts: %d\n", c.Verification.SelfCorrectionAttempts)
	return b.String()
}

func yamlScalar(s string) string { if s == "" { return "\"\"" }; needsQuote := strings.ContainsAny(s, ":#[]{}\\\",'\t "); if !needsQuote { return s }; return strconv.Quote(s) }

func parseYAML(text string, c *Config) error {
	section, current := "", ""
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t"); if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") { continue }
		trimmed := strings.TrimSpace(line); indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 { switch { case strings.HasPrefix(trimmed, "default_provider:"): c.DefaultProvider = unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, "default_provider:"))); section, current = "", ""; case trimmed == "providers:": section, current = "providers", ""; case strings.HasPrefix(trimmed, "fallback_order:"): c.FallbackOrder = parseList(strings.TrimSpace(strings.TrimPrefix(trimmed, "fallback_order:"))); section, current = "", ""; case trimmed == "verification:": section, current = "verification", ""; default: section, current = "", "" }; continue }
		if section == "providers" && indent == 2 && strings.HasSuffix(trimmed, ":") { current = strings.TrimSuffix(trimmed, ":"); if _, ok := c.Providers[current]; !ok { c.Providers[current] = ProviderConfig{} }; continue }
		if section == "providers" && indent >= 4 && current != "" { key, value := splitKV(trimmed); p := c.Providers[current]; switch key { case "api_key": p.APIKey = unquote(value); case "default_model": p.DefaultModel = unquote(value); case "base_url": p.BaseURL = unquote(value) }; c.Providers[current] = p; continue }
		if section == "verification" && indent >= 2 { key, value := splitKV(trimmed); if key == "self_correction_attempts" { n, err := strconv.Atoi(unquote(value)); if err != nil { return fmt.Errorf("parse self_correction_attempts: %w", err) }; c.Verification.SelfCorrectionAttempts = n } }
	}
	return nil
}

func splitKV(s string) (string, string) { index := strings.IndexByte(s, ':'); if index < 0 { return s, "" }; return strings.TrimSpace(s[:index]), strings.TrimSpace(s[index+1:]) }
func unquote(s string) string { s = strings.TrimSpace(s); if value, err := strconv.Unquote(s); err == nil { return value }; return s }
func parseList(s string) []string { s = strings.TrimSpace(s); if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") { s = strings.TrimSpace(s[1:len(s)-1]) }; if s == "" { return nil }; parts := strings.Split(s, ","); result := make([]string, 0, len(parts)); for _, part := range parts { value := unquote(strings.TrimSpace(part)); if value != "" { result = append(result, value) } }; return result }
