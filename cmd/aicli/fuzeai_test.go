package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/config"
)

func TestFuzeAIHelpCommand(t *testing.T) {
	if err := run([]string{"fuzeai", "help"}); err != nil {
		t.Fatalf("FuzeAI help failed: %v", err)
	}
}

func TestFuzeAIStartRejectsInvalidPortBeforeLaunching(t *testing.T) {
	if err := startFuzeAI([]string{"--port", "0"}); err == nil {
		t.Fatal("expected invalid port to be rejected")
	}
	if err := startFuzeAI([]string{"--context", "10"}); err == nil {
		t.Fatal("expected too-small context size to be rejected")
	}
}

func TestFuzeAIEndpointRejectsRemoteHost(t *testing.T) {
	if _, err := serverResponds("https://example.com"); err == nil {
		t.Fatal("expected non-local model endpoint to be rejected")
	}
	if !isLocalHost("localhost") || !isLocalHost("127.0.0.1") || !isLocalHost("::1") {
		t.Fatal("expected loopback hostnames and addresses to be recognized")
	}
	if isLocalHost("192.0.2.10") {
		t.Fatal("non-loopback address was recognized as local")
	}
}

func TestConfigureFuzeAISelectsLocalModel(t *testing.T) {
	dir := t.TempDir()
	setFuzeAITestConfigHome(t, dir)

	if err := configureFuzeAI(); err != nil {
		t.Fatalf("configure FuzeAI: %v", err)
	}
	got, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got.DefaultProvider != "llamacpp" {
		t.Fatalf("default provider = %q, want llamacpp", got.DefaultProvider)
	}
	if got.Providers["llamacpp"].DefaultModel != fuzeAIModel {
		t.Fatalf("model = %q, want %q", got.Providers["llamacpp"].DefaultModel, fuzeAIModel)
	}
	if got.Providers["llamacpp"].BaseURL != fuzeAIURL {
		t.Fatalf("base URL = %q, want %q", got.Providers["llamacpp"].BaseURL, fuzeAIURL)
	}
}

func setFuzeAITestConfigHome(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"APPDATA", "XDG_CONFIG_HOME", "HOME"} {
		old, exists := os.LookupEnv(name)
		if err := os.Setenv(name, dir); err != nil {
			t.Fatal(err)
		}
		name, old, exists := name, old, exists
		t.Cleanup(func() {
			if exists {
				_ = os.Setenv(name, old)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
	if err := os.MkdirAll(filepath.Join(dir, "aicli"), 0700); err != nil {
		t.Fatal(err)
	}
}
