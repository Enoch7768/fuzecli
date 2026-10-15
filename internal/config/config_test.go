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

	if c.DefaultProvider != "gemini" {
		t.Fatalf("unexpected default provider: %q", c.DefaultProvider)
	}

	expectedFallback := []string{
		"gemini",
		"groq",
		"openai",
		"llamacpp",
	}

	if !reflect.DeepEqual(c.FallbackOrder, expectedFallback) {
		t.Fatalf("unexpected fallback order: %#v", c.FallbackOrder)
	}

	if c.Providers["gemini"].DefaultModel != "gemini-3.8-flash" {
		t.Fatalf("unexpected Gemini default model: %q", c.Providers["gemini"].DefaultModel)
	}

	if c.Providers["llamacpp"].BaseURL != "http://localhost:8080" {
		t.Fatalf("unexpected llama.cpp base URL: %q", c.Providers["llamacpp"].BaseURL)
	}

	if c.Verification.SelfCorrectionAttempts != 2 {
		t.Fatalf("unexpected self-correction attempts: %d", c.Verification.SelfCorrectionAttempts)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	setTestConfigHome(t, tmp)

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
	if got.Providers["openai"].APIKey != "secret" {
		t.Fatal("API key round trip failed")
	}
	if !reflect.DeepEqual(got.FallbackOrder, c.FallbackOrder) {
		t.Fatal("fallback order round trip failed")
	}
	if got.DefaultProvider != c.DefaultProvider {
		t.Fatal("default provider round trip failed")
	}
	if _, err := os.Stat(Path()); err != nil {
		t.Fatal(err)
	}
}

func TestSetFallbackOrder(t *testing.T) {
	tmp := t.TempDir()
	setTestConfigHome(t, tmp)

	if err := Set("fallback_order", "[gemini, groq, openai, llamacpp]"); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"gemini", "groq", "openai", "llamacpp"}
	if !reflect.DeepEqual(got.FallbackOrder, expected) {
		t.Fatalf("unexpected fallback order: %#v", got.FallbackOrder)
	}
}


func TestGitHubTokenConfiguredOverridesEnvironment(t *testing.T) {
	tmp := t.TempDir()
	setTestConfigHome(t, tmp)
	oldGitHub := os.Getenv("GITHUB_TOKEN")
	oldGH := os.Getenv("GH_TOKEN")
	_ = os.Setenv("GITHUB_TOKEN", "old-environment-token")
	_ = os.Setenv("GH_TOKEN", "older-environment-token")
	defer func() {
		_ = os.Setenv("GITHUB_TOKEN", oldGitHub)
		_ = os.Setenv("GH_TOKEN", oldGH)
	}()

	if err := SetGitHubToken("new-ui-token"); err != nil {
		t.Fatal(err)
	}
	got, err := GitHubToken()
	if err != nil {
		t.Fatal(err)
	}
	if got != "new-ui-token" {
		t.Fatalf("configured token should override environment token, got %q", got)
	}
	if err := ClearGitHubToken(); err != nil {
		t.Fatal(err)
	}
	got, err = GitHubToken()
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("clearing the stored token should enable gh fallback, got %q", got)
	}
	configured, err := GitHubTokenConfigured()
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		t.Fatal("clearing the stored token should not mark an explicit disconnect")
	}
	if err := DisconnectGitHub(); err != nil {
		t.Fatal(err)
	}
	configured, err = GitHubTokenConfigured()
	if err != nil {
		t.Fatal(err)
	}
	if !configured {
		t.Fatal("explicit disconnect should suppress gh fallback")
	}
	got, err = GitHubToken()
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("explicit disconnect should not return a token, got %q", got)
	}
}


func setTestConfigHome(t *testing.T, dir string) {
	t.Helper()
	type environmentValue struct {
		name string
		value string
		set bool
	}
	values := make([]environmentValue, 0, 3)
	for _, name := range []string{"APPDATA", "XDG_CONFIG_HOME", "HOME"} {
		value, set := os.LookupEnv(name)
		values = append(values, environmentValue{name: name, value: value, set: set})
		if err := os.Setenv(name, dir); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, value := range values {
			if value.set {
				_ = os.Setenv(value.name, value.value)
			} else {
				_ = os.Unsetenv(value.name)
			}
		}
	})
}
