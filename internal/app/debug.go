package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/diagnostics"
)

// Debug prints a developer-focused diagnostic report without exposing secrets.
// It is intentionally read-only so it is safe to run when troubleshooting.
func (a *App) Debug() error {
	fmt.Println("FuzeCLI DEBUG")
	fmt.Println("==============")
	fmt.Printf("Go:        %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Go version: %s\n", runtime.Version())
	if info, ok := debug.ReadBuildInfo(); ok {
		fmt.Printf("Build:     %s\n", info.GoVersion)
	}

	root := ""
	if a.Store != nil {
		root = a.Store.Root
	}
	if root == "" {
		root, _ = os.Getwd()
	}
	fmt.Printf("Workspace: %s\n", root)
	fmt.Printf("Initialized: %t\n", workspaceInitialized(root))

	if a.Config.DefaultProvider != "" {
		fmt.Printf("Provider:   %s\n", a.Config.DefaultProvider)
		if p, ok := a.Config.Providers[a.Config.DefaultProvider]; ok {
			fmt.Printf("Model:      %s\n", p.DefaultModel)
			if p.APIKey != "" {
				fmt.Println("API key:    configured")
			} else {
				fmt.Println("API key:    not configured")
			}
		}
	}

	if a.Store != nil {
		if history, err := a.Store.History(20); err == nil {
			fmt.Printf("History:    %d recent message(s)\n", len(history))
			var combined strings.Builder
			for _, message := range history {
				combined.WriteString(message.Content)
				combined.WriteByte('\n')
			}
			parsed := diagnostics.Parse(combined.String())
			fmt.Printf("Diagnostics: %d structured issue(s) in recent history\n", len(parsed))
		}
	}

	fmt.Println()
	fmt.Println("Useful diagnostics:")
	fmt.Println("  aicli doctor     Provider and workspace checks")
	fmt.Println("  aicli status     Git/workspace status")
	fmt.Println("  aicli diff       Review current changes")
	fmt.Println("  aicli debug      This report (safe to paste when asking for help)")
	return nil
}

func workspaceInitialized(root string) bool {
	info, err := os.Stat(filepath.Join(root, ".aicli", "session.db"))
	return err == nil && !info.IsDir()
}

// DebugConfigSummary exposes a redacted configuration summary for diagnostics.
func DebugConfigSummary(c config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "provider=%s", c.DefaultProvider)
	for name, p := range c.Providers {
		fmt.Fprintf(&b, " %s(model=%s,key=%s)", name, p.DefaultModel, config.MaskSecret(p.APIKey))
	}
	return b.String()
}
