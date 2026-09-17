package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/config"
	"github.com/Enoch7768/fuzecli/internal/diagnostics"
	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/intelligence"
	"github.com/Enoch7768/fuzecli/internal/provider"
	"github.com/Enoch7768/fuzecli/internal/verify"
)

func (a *App) Debug() error {
	return a.debug(context.Background(), false)
}

func (a *App) DebugLive(ctx context.Context) error {
	return a.debug(ctx, true)
}

func (a *App) debug(ctx context.Context, live bool) error {
	fmt.Println()
	fmt.Println("FuzeCLI DEBUG")
	fmt.Println("=============")
	fmt.Println("Read-only diagnostics; secrets are redacted.")
	fmt.Println()

	root := debugWorkspaceRoot(a)
	fmt.Println("SYSTEM")
	fmt.Printf("  OS/arch:       %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  Go:            %s\n", runtime.Version())
	if info, ok := debug.ReadBuildInfo(); ok {
		fmt.Printf("  Build Go:      %s\n", info.GoVersion)
	}
	fmt.Printf("  Workspace:     %s\n", root)
	fmt.Printf("  Initialized:   %s\n", debugStatus(workspaceInitialized(root), "yes", "no"))

	fmt.Println()
	fmt.Println("CONFIGURATION")
	fmt.Printf("  Default:       %s\n", valueOr(a.Config.DefaultProvider, "not configured"))
	configured := 0
	providerNames := make([]string, 0, len(a.Config.Providers))
	for name := range a.Config.Providers {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)
	for _, name := range providerNames {
		p := a.Config.Providers[name]
		model := valueOr(p.DefaultModel, "default")
		key := "missing"
		if p.APIKey != "" {
			key = "configured (redacted)"
			configured++
		}
		fmt.Printf("  %-14s model=%-28s key=%s\n", name, model, key)
	}
	if configured == 0 {
		fmt.Println("  !! No API keys are configured. Run: aicli setup")
	}

	fmt.Println()
	fmt.Println("WORKSPACE")
	if a.Store == nil {
		fmt.Println("  !! Workspace is not attached to this App instance.")
	} else {
		fmt.Printf("  Database:      %s\n", filepath.Join(a.Store.Root, ".aicli", "session.db"))
		if err := debugDatabase(a.Store.DB); err != nil {
			fmt.Printf("  !! Database:    %v\n", err)
		} else {
			fmt.Println("  Database:      healthy")
		}
		if touched, err := a.Store.Touched(); err != nil {
			fmt.Printf("  !! Touched:     %v\n", err)
		} else {
			fmt.Printf("  Touched files: %d\n", len(touched))
		}
		if state, err := a.Store.LoadState(); err != nil {
			fmt.Printf("  !! State:       %v\n", err)
		} else {
			fmt.Printf("  File hashes:   %d\n", len(state.FileHashes))
		}
	}

	fmt.Println()
	fmt.Println("CODE INTELLIGENCE")
	idx, err := intelligence.Build(root)
	if err != nil {
		fmt.Printf("  !! Index build: %v\n", err)
	} else {
		fmt.Printf("  Source files:  %d\n", len(idx.Files))
		fmt.Printf("  Symbols:       %d\n", len(idx.Symbols))
		if len(idx.Files) == 0 {
			fmt.Println("  !! No supported source files were indexed.")
		} else {
			fmt.Println("  Index:         healthy")
		}
	}

	fmt.Println()
	fmt.Println("VERIFICATION TOOLCHAIN")
	debugTool("go", "Go toolchain")
	debugTool("git", "Git")
	debugTool("node", "Node.js")
	debugTool("npm", "npm")
	debugTool("python", "Python")
	debugTool("php", "PHP")

	fmt.Println()
	fmt.Println("GENERATED-CODE HEALTH")
	if a.Store == nil {
		fmt.Println("  !! Cannot verify workspace: no store attached.")
	} else {
		touched, touchedErr := a.Store.Touched()
		if touchedErr != nil {
			fmt.Printf("  !! Cannot read touched files: %v\n", touchedErr)
		} else {
			result, detectErr := verify.Detect(root, touched)
			if detectErr != nil {
				fmt.Printf("  !! Verification detection failed: %v\n", detectErr)
			} else {
				fmt.Printf("  Tool:          %s\n", valueOr(result.Tool, "none"))
				if result.Passed {
					fmt.Println("  Result:        PASS")
				} else {
					fmt.Println("  Result:        FAIL")
					printDiagnosticHints(result.Output, result.Diagnostics)
				}
			}
		}
	}

	fmt.Println()
	fmt.Println("AI / JSON HEALTH")
	debugChatHistory(a)
	if live {
		fmt.Println()
		fmt.Println("LIVE PROVIDER CHECK")
		debugProviders(ctx, a)
	}

	fmt.Println()
	fmt.Println("NEXT STEPS")
	fmt.Println("  aicli doctor       Provider/workspace health checks")
	fmt.Println("  aicli setup        Configure provider, API key and model")
	fmt.Println("  aicli models       Check models exposed by a provider")
	fmt.Println("  aicli status       Inspect Git/workspace state")
	fmt.Println("  aicli diff         Review current changes")
	fmt.Println("  aicli debug        Repeat local diagnostics")
	fmt.Println("  aicli debug --live Also test configured providers")
	return nil
}

func debugWorkspaceRoot(a *App) string {
	if a != nil && a.Store != nil && a.Store.Root != "" {
		return a.Store.Root
	}
	root, err := os.Getwd()
	if err != nil {
		return "."
	}
	return root
}

func workspaceInitialized(root string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	dir := filepath.Join(root, ".aicli")
	for _, name := range []string{"session.db", "state.json"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func debugDatabase(db *sql.DB) error {
	if db == nil {
		return errors.New("database handle is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("integrity check: %s", result)
	}
	return nil
}

func debugTool(name, label string) {
	path, err := exec.LookPath(name)
	if err != nil {
		fmt.Printf("  %-14s missing\n", label+":")
		return
	}
	fmt.Printf("  %-14s available (%s)\n", label+":", path)
}

func debugChatHistory(a *App) {
	if a == nil || a.Store == nil {
		fmt.Println("  !! Conversation history unavailable: workspace not attached.")
		return
	}
	history, err := a.Store.History(50)
	if err != nil {
		fmt.Printf("  !! History read failed: %v\n", err)
		return
	}
	fmt.Printf("  Recent messages: %d\n", len(history))

	var parseFailures, likelyJSONResponses, providerErrors, diagnosticErrors int
	var lastFailure string
	for _, message := range history {
		content := strings.TrimSpace(message.Content)
		if message.Role == "assistant" {
			if looksLikeJSONDocument(content) {
				likelyJSONResponses++
				if _, err := generation.ParseChatResponse(content); err != nil {
					parseFailures++
					lastFailure = err.Error()
				}
			}
		}
		parsed := diagnostics.Parse(content)
		if len(parsed) > 0 {
			diagnosticErrors += len(parsed)
		}
		lower := strings.ToLower(content)
		if strings.Contains(lower, "provider") && (strings.Contains(lower, "rate limit") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "quota") || strings.Contains(lower, "unavailable")) {
			providerErrors++
		}
	}
	fmt.Printf("  JSON responses:  %d\n", likelyJSONResponses)
	fmt.Printf("  JSON parse fail: %d\n", parseFailures)
	fmt.Printf("  Provider errors: %d\n", providerErrors)
	fmt.Printf("  Diagnostics:     %d\n", diagnosticErrors)
	if parseFailures > 0 {
		fmt.Printf("  !! Last JSON parser error: %s\n", lastFailure)
		fmt.Println("     The chat parser is rejecting at least one stored AI response; use the JSON normalizer path during chat and inspect the provider response if this persists.")
	}
}

func looksLikeJSONDocument(s string) bool {
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "```") {
		return strings.Contains(s, "{") || strings.Contains(s, "[")
	}
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

func debugProviders(ctx context.Context, a *App) {
	if a == nil || a.Registry == nil {
		fmt.Println("  !! Provider registry is unavailable.")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	names := make([]string, 0, len(a.Config.Providers))
	for name := range a.Config.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cfg := a.Config.Providers[name]
		if name == "llamacpp" {
			if cfg.BaseURL == "" {
				fmt.Printf("  %-14s SKIP (no base URL)\n", name)
				continue
			}
		} else if cfg.APIKey == "" {
			fmt.Printf("  %-14s SKIP (API key missing)\n", name)
			continue
		}
		started := time.Now()
		models, err := a.Registry.ListModels(ctx, name)
		elapsed := time.Since(started).Round(time.Millisecond)
		if err != nil {
			var pe *provider.ProviderError
			if errors.As(err, &pe) {
				fmt.Printf("  %-14s FAIL %-18s status=%d after=%s\n", name, pe.Kind, pe.StatusCode, elapsed)
				fmt.Printf("                 %s\n", pe.Message)
			} else {
				fmt.Printf("  %-14s FAIL after=%s: %s\n", name, elapsed, err)
			}
			continue
		}
		fmt.Printf("  %-14s OK   models=%d after=%s\n", name, len(models), elapsed)
	}
}

func printDiagnosticHints(output string, parsed []diagnostics.Diagnostic) {
	if len(parsed) > 0 {
		fmt.Printf("  Diagnostics:    %s\n", diagnostics.Summary(parsed))
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	limit := 6
	if len(lines) < limit {
		limit = len(lines)
	}
	for i := 0; i < limit; i++ {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			fmt.Printf("                 %s\n", line)
		}
	}
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func debugStatus(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func DebugConfigSummary(c config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "provider=%s", c.DefaultProvider)
	names := make([]string, 0, len(c.Providers))
	for name := range c.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := c.Providers[name]
		fmt.Fprintf(&b, " %s(model=%s,key=%s)", name, p.DefaultModel, config.MaskSecret(p.APIKey))
	}
	return b.String()
}
