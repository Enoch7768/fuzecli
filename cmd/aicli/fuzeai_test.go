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
	if got.Providers["llamacpp"].BaseURL != "http://localhost:8080" {
		t.Fatalf("base URL = %q, want http://localhost:8080", got.Providers["llamacpp"].BaseURL)
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

func TestConnectFuzeAIRemoteEndpoint(t *testing.T) {
	dir := t.TempDir()
	setFuzeAITestConfigHome(t, dir)
	if err := connectFuzeAI("http://example.trycloudflare.com"); err == nil {
		t.Fatal("expected insecure HTTP endpoint to be rejected")
	}
	if err := connectFuzeAI("https://demo.trycloudflare.com"); err != nil {
		t.Fatalf("connect remote FuzeAI: %v", err)
	}
	got, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got.DefaultProvider != "llamacpp" {
		t.Fatalf("default provider = %q, want llamacpp", got.DefaultProvider)
	}
	if got.Providers["llamacpp"].BaseURL != "https://demo.trycloudflare.com" {
		t.Fatalf("base URL = %q, want remote endpoint", got.Providers["llamacpp"].BaseURL)
	}
	if got.Providers["llamacpp"].DefaultModel != fuzeAIModel {
		t.Fatalf("model = %q, want %q", got.Providers["llamacpp"].DefaultModel, fuzeAIModel)
	}
	if err := disconnectFuzeAI(); err != nil {
		t.Fatalf("disconnect remote FuzeAI: %v", err)
	}
	got, err = config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if got.Providers["llamacpp"].BaseURL != fuzeAIURL {
		t.Fatalf("disconnect base URL = %q, want %q", got.Providers["llamacpp"].BaseURL, fuzeAIURL)
	}
}

func TestPublicFuzeAIEndpointValidation(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1", "192.168.1.2"} {
		if isPublicHTTPSHost(host) {
			t.Fatalf("host %q should not be considered a public endpoint", host)
		}
	}
	if !isPublicHTTPSHost("demo.trycloudflare.com") {
		t.Fatal("expected public tunnel hostname to be accepted")
	}
}
