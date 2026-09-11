package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Enoch7768/fuzecli/internal/workspace"
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
	}
	if _, err := workspace.LoadIgnorePolicy(a.Store.Root); err != nil {
		checks = append(checks, struct {
			name string
			ok   bool
			err  error
		}{".aicliignore policy", false, err})
	} else {
		checks = append(checks, struct {
			name string
			ok   bool
			err  error
		}{".aicliignore policy", true, nil})
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
