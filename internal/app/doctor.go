package app

import (
	"fmt"
	"os"
	"path/filepath"
)

// Doctor performs local, non-network checks that are safe to run before using
// FuzeCLI on a public or sensitive project.
func (a *App) Doctor() error {
	if a == nil {
		return fmt.Errorf("application is not initialized")
	}
	if a.Store == nil {
		return fmt.Errorf("workspace is not attached; run the command from a workspace")
	}

	checks := []struct {
		name string
		ok   bool
		err  error
	}{
		{"workspace", a.Store.Root != "", nil},
		{".aicli directory", directoryExists(filepath.Join(a.Store.Root, ".aicli")), nil},
		{".aicliignore policy", filePolicyLoads(a.Store.Root), nil},
	}

	failed := false
	for _, check := range checks {
		if check.ok && check.err == nil {
			fmt.Printf("✓ %s\n", check.name)
			continue
		}
		failed = true
		if check.err != nil {
			fmt.Printf("✗ %s: %v\n", check.name, check.err)
		} else {
			fmt.Printf("✗ %s\n", check.name)
		}
	}

	if len(a.Config.FallbackOrder) == 0 {
		fmt.Println("⚠ provider fallback order is empty")
	} else {
		fmt.Printf("✓ provider fallback order: %d provider(s)\n", len(a.Config.FallbackOrder))
	}

	if a.Config.DefaultProvider == "" {
		fmt.Println("⚠ default provider is not configured")
	}

	if failed {
		return fmt.Errorf("one or more local checks failed")
	}
	return nil
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func filePolicyLoads(root string) bool {
	_, err := workspacePolicy(root)
	return err == nil
}

func workspacePolicy(root string) (string, error) {
	policy, err := loadPolicy(root)
	if err != nil {
		return "", err
	}
	return policy, nil
}
