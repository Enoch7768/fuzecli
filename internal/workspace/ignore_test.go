package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIgnorePolicyBlocksSecrets(t *testing.T) {
	root := t.TempDir()
	policy, err := LoadIgnorePolicy(root)
	if err != nil {
		t.Fatal(err)
	}

	blocked := []string{
		".env",
		".env.production",
		"config/server.pem",
		"keys/id_ed25519",
		"credentials.json",
		"nested/secrets.yml",
	}
	for _, path := range blocked {
		if !policy.Ignored(path) {
			t.Fatalf("expected %q to be blocked", path)
		}
	}
}

func TestIgnorePolicyReadsProjectRules(t *testing.T) {
	root := t.TempDir()
	ignore := filepath.Join(root, ".aicliignore")
	if err := os.WriteFile(ignore, []byte("generated/\n*.local\n"), 0600); err != nil {
		t.Fatal(err)
	}

	policy, err := LoadIgnorePolicy(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"generated/app.go", "settings.local"} {
		if !policy.Ignored(path) {
			t.Fatalf("expected %q to be ignored", path)
		}
	}
	if policy.Ignored("src/app.go") {
		t.Fatal("unexpected ignore for src/app.go")
	}
}

func TestWorkspaceContextExcludesIgnoredFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=do-not-send"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}

	store, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	context, err := store.WorkspaceContext()
	if err != nil {
		t.Fatal(err)
	}
	if contains := string(context); contains == "" || !containsText(context, "main.go") {
		t.Fatal("expected normal source file in workspace context")
	}
	if containsText(context, "do-not-send") || containsText(context, ".env") {
		t.Fatal("secret file leaked into workspace context")
	}
}

func containsText(value, needle string) bool {
	return len(needle) > 0 && len(value) >= len(needle) && indexOf(value, needle) >= 0
}

func indexOf(value, needle string) int {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
