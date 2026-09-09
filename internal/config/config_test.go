package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaskSecret(t *testing.T) {
	if got := MaskSecret("sk-123456"); got != "*****3456" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultConfigShape(t *testing.T) {
	c := Default()
	if c.DefaultProvider != "groq" || len(c.FallbackOrder) == 0 {
		t.Fatal("unexpected default config")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	old := os.Getenv("XDG_CONFIG_HOME")
	_ = os.Setenv("XDG_CONFIG_HOME", tmp)
	defer os.Setenv("XDG_CONFIG_HOME", old)
	c := Default()
	c.Providers["openai"] = ProviderConfig{APIKey: "secret", DefaultModel: "model"}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers["openai"].DefaultModel != "model" {
		t.Fatal("round trip failed")
	}
	if _, err := os.Stat(filepath.Join(tmp, "aicli", "config.yaml")); err != nil {
		t.Fatal(err)
	}
}
