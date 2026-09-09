package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMaskSecret(t *testing.T) {
	if got := MaskSecret("sk-123456"); got != "*****3456" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultConfigShape(t *testing.T) {
	c := Default()

	if c.DefaultProvider != "llamacpp" {
		t.Fatalf("unexpected default provider: %q", c.DefaultProvider)
	}

	expectedFallback := []string{
		"llamacpp",
		"gemini",
		"groq",
		"openai",
	}

	if !reflect.DeepEqual(
		c.FallbackOrder,
		expectedFallback,
	) {
		t.Fatalf(
			"unexpected fallback order: %#v",
			c.FallbackOrder,
		)
	}

	if c.Providers["gemini"].DefaultModel != "gemini-2.5-flash" {
		t.Fatalf(
			"unexpected Gemini default model: %q",
			c.Providers["gemini"].DefaultModel,
		)
	}

	if c.Providers["llamacpp"].BaseURL != "http://localhost:8080" {
		t.Fatalf(
			"unexpected llama.cpp base URL: %q",
			c.Providers["llamacpp"].BaseURL,
		)
	}

	if c.Verification.SelfCorrectionAttempts != 2 {
		t.Fatalf(
			"unexpected self-correction attempts: %d",
			c.Verification.SelfCorrectionAttempts,
		)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()

	oldAppData := os.Getenv("APPDATA")
	oldXDG := os.Getenv("XDG_CONFIG_HOME")

	_ = os.Setenv("APPDATA", tmp)
	_ = os.Setenv("XDG_CONFIG_HOME", tmp)

	defer func() {
		_ = os.Setenv("APPDATA", oldAppData)
		_ = os.Setenv("XDG_CONFIG_HOME", oldXDG)
	}()

	c := Default()

	c.Providers["openai"] = ProviderConfig{
		APIKey:       "secret",
		DefaultModel: "model",
	}

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

	if got.Providers["openai"].APIKey != "secret" {
		t.Fatal("API key round trip failed")
	}

	if !reflect.DeepEqual(
		got.FallbackOrder,
		c.FallbackOrder,
	) {
		t.Fatal("fallback order round trip failed")
	}

	if got.DefaultProvider != c.DefaultProvider {
		t.Fatal("default provider round trip failed")
	}

	if _, err := os.Stat(
		filepath.Join(
			tmp,
			"aicli",
			"config.yaml",
		),
	); err != nil {
		t.Fatal(err)
	}
}

func TestSetFallbackOrder(t *testing.T) {
	tmp := t.TempDir()

	oldAppData := os.Getenv("APPDATA")
	oldXDG := os.Getenv("XDG_CONFIG_HOME")

	_ = os.Setenv("APPDATA", tmp)
	_ = os.Setenv("XDG_CONFIG_HOME", tmp)

	defer func() {
		_ = os.Setenv("APPDATA", oldAppData)
		_ = os.Setenv("XDG_CONFIG_HOME", oldXDG)
	}()

	if err := Set(
		"fallback_order",
		"[llamacpp, gemini, groq, openai]",
	); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{
		"llamacpp",
		"gemini",
		"groq",
		"openai",
	}

	if !reflect.DeepEqual(
		got.FallbackOrder,
		expected,
	) {
		t.Fatalf(
			"unexpected fallback order: %#v",
			got.FallbackOrder,
		)
	}
}