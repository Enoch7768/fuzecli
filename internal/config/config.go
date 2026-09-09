package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
	return Config{DefaultProvider: "groq", Providers: map[string]ProviderConfig{"openai": {DefaultModel: "gpt-4o"}, "gemini": {DefaultModel: "gemini-1.5-pro"}, "groq": {DefaultModel: "llama3-70b-8192"}, "anthropic": {DefaultModel: "claude-sonnet-4-6"}, "llamacpp": {BaseURL: "http://localhost:8080", DefaultModel: "local"}}, FallbackOrder: []string{"groq", "gemini", "openai"}, Verification: VerificationConfig{SelfCorrectionAttempts: 2}}
}
func Dir() string         { d, _ := os.UserConfigDir(); return filepath.Join(d, "aicli") }
func Path() string        { return filepath.Join(Dir(), "config.yaml") }
func ProfilePath() string { return filepath.Join(Dir(), "profile.json") }

func Load() (Config, error) {
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		c := Default()
		if err := Save(c); err != nil {
			return Config{}, err
		}
		return c, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	c := Default()
	if err := parseYAML(string(b), &c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return c, nil
}
func Save(c Config) error {
	if err := os.MkdirAll(Dir(), 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data := renderYAML(c)
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, Path()); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func Set(key, value string) error {
	c, err := Load()
	if err != nil {
		return err
	}
	parts := strings.Split(key, ".")
	switch {
	case key == "default_provider":
		c.DefaultProvider = value
	case len(parts) == 2 && parts[0] == "providers":
		return fmt.Errorf("provider config requires providers.<name>.<field>")
	case len(parts) == 2:
		{
			name, field := parts[0], parts[1]
			p, ok := c.Providers[name]
			if !ok {
				return fmt.Errorf("unknown provider %q", name)
			}
			switch field {
			case "api_key":
				p.APIKey = value
			case "default_model":
				p.DefaultModel = value
			case "base_url":
				p.BaseURL = value
			default:
				return fmt.Errorf("unsupported config field %q", field)
			}
			c.Providers[name] = p
		}
	case len(parts) == 3 && parts[0] == "providers":
		{
			name, field := parts[1], parts[2]
			p, ok := c.Providers[name]
			if !ok {
				return fmt.Errorf("unknown provider %q", name)
			}
			switch field {
			case "api_key":
				p.APIKey = value
			case "default_model":
				p.DefaultModel = value
			case "base_url":
				p.BaseURL = value
			default:
				return fmt.Errorf("unsupported config field %q", field)
			}
			c.Providers[name] = p
		}
	case key == "verification.self_correction_attempts":
		n, e := strconv.Atoi(value)
		if e != nil || n < 0 {
			return fmt.Errorf("invalid attempt count")
		}
		c.Verification.SelfCorrectionAttempts = n
	default:
		return fmt.Errorf("unsupported config key %q", key)
	}
	return Save(c)
}
func MaskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}
func Timestamp() string { return time.Now().UTC().Format(time.RFC3339) }

func renderYAML(c Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "default_provider: %s\nproviders:\n", yamlScalar(c.DefaultProvider))
	names := []string{"openai", "gemini", "groq", "anthropic", "llamacpp"}
	seen := map[string]bool{}
	for _, name := range names {
		p, ok := c.Providers[name]
		if !ok {
			continue
		}
		seen[name] = true
		fmt.Fprintf(&b, "  %s:\n", name)
		if p.APIKey != "" {
			fmt.Fprintf(&b, "    api_key: %s\n", yamlScalar(p.APIKey))
		}
		if p.DefaultModel != "" {
			fmt.Fprintf(&b, "    default_model: %s\n", yamlScalar(p.DefaultModel))
		}
		if p.BaseURL != "" {
			fmt.Fprintf(&b, "    base_url: %s\n", yamlScalar(p.BaseURL))
		}
	}
	for name, p := range c.Providers {
		if seen[name] {
			continue
		}
		fmt.Fprintf(&b, "  %s:\n", name)
		if p.APIKey != "" {
			fmt.Fprintf(&b, "    api_key: %s\n", yamlScalar(p.APIKey))
		}
		if p.DefaultModel != "" {
			fmt.Fprintf(&b, "    default_model: %s\n", yamlScalar(p.DefaultModel))
		}
		if p.BaseURL != "" {
			fmt.Fprintf(&b, "    base_url: %s\n", yamlScalar(p.BaseURL))
		}
	}
	order := make([]string, len(c.FallbackOrder))
	for i, x := range c.FallbackOrder {
		order[i] = yamlScalar(x)
	}
	fmt.Fprintf(&b, "fallback_order: [%s]\nverification:\n  self_correction_attempts: %d\n", strings.Join(order, ", "), c.Verification.SelfCorrectionAttempts)
	return b.String()
}
func yamlScalar(s string) string {
	if s == "" {
		return "\"\""
	}
	needsQuote := strings.ContainsAny(s, ":#[]{}\",'\t ")
	if !needsQuote {
		return s
	}
	return strconv.Quote(s)
}
func parseYAML(text string, c *Config) error {
	section := ""
	current := ""
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		trim := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			switch {
			case strings.HasPrefix(trim, "default_provider:"):
				c.DefaultProvider = unquote(strings.TrimSpace(strings.TrimPrefix(trim, "default_provider:")))
				current = ""
			case trim == "providers:":
				section = "providers"
				current = ""
			case strings.HasPrefix(trim, "fallback_order:"):
				c.FallbackOrder = parseList(strings.TrimSpace(strings.TrimPrefix(trim, "fallback_order:")))
			case trim == "verification:":
				section = "verification"
				current = ""
			default:
				section = ""
			}
		} else if section == "providers" && indent == 2 && strings.HasSuffix(trim, ":") {
			current = strings.TrimSuffix(trim, ":")
			if _, ok := c.Providers[current]; !ok {
				c.Providers[current] = ProviderConfig{}
			}
		} else if section == "providers" && indent >= 4 && current != "" {
			key, val := splitKV(trim)
			p := c.Providers[current]
			switch key {
			case "api_key":
				p.APIKey = unquote(val)
			case "default_model":
				p.DefaultModel = unquote(val)
			case "base_url":
				p.BaseURL = unquote(val)
			}
			c.Providers[current] = p
		} else if section == "verification" && indent >= 2 {
			key, val := splitKV(trim)
			if key == "self_correction_attempts" {
				n, e := strconv.Atoi(unquote(val))
				if e != nil {
					return e
				}
				c.Verification.SelfCorrectionAttempts = n
			}
		}
	}
	return nil
}
func splitKV(s string) (string, string) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return s, ""
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
}
func unquote(s string) string {
	if v, e := strconv.Unquote(s); e == nil {
		return v
	}
	return strings.TrimSpace(s)
}
func parseList(s string) []string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := []string{}
	for _, p := range parts {
		if v := unquote(strings.TrimSpace(p)); v != "" {
			out = append(out, v)
		}
	}
	return out
}
