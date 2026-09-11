package verify

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/diagnostics"
)

type Result struct {
	Tool         string
	Passed       bool
	Output       string
	Diagnostics  []diagnostics.Diagnostic
}

func Detect(root string, touched []string) (Result, error) {
	has := func(name string) bool { _, e := os.Stat(filepath.Join(root, name)); return e == nil }
	switch {
	case has("go.mod"):
		return run(root, "go", "test", "./...")
	case has("package.json") && has("tsconfig.json"):
		if has("package-lock.json") {
			return run(root, "npm", "test", "--", "--runInBand")
		}
		return run(root, "npx", "--yes", "tsc", "--noEmit")
	case has("package.json"):
		if has("package-lock.json") {
			return run(root, "npm", "test", "--", "--runInBand")
		}
		return Result{Tool: "node", Passed: true, Output: "Node project detected; no lockfile-backed test command was inferred."}, nil
	case has("composer.json"):
		return lintPHP(root, touched)
	case has("requirements.txt") || has("pyproject.toml"):
		return lintPython(root, touched)
	default:
		return Result{Tool: "none", Passed: true, Output: "No supported toolchain detected; verification skipped."}, nil
	}
}

func run(root string, name string, args ...string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = root
	b, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(b))
	result := Result{Tool: name, Passed: err == nil, Output: out}
	result.Diagnostics = diagnostics.Parse(out)
	if ctx.Err() != nil {
		result.Passed = false
		result.Output = strings.TrimSpace(out + "\nverification timed out")
	}
	return result, nil
}

func lintPHP(root string, touched []string) (Result, error) {
	for _, rel := range touched {
		if filepath.Ext(rel) != ".php" { continue }
		r, _ := run(root, "php", "-l", filepath.FromSlash(rel))
		if !r.Passed { return r, nil }
	}
	return Result{Tool: "php -l", Passed: true, Output: "PHP syntax checks passed for touched PHP files."}, nil
}

func lintPython(root string, touched []string) (Result, error) {
	for _, rel := range touched {
		if filepath.Ext(rel) != ".py" { continue }
		r, _ := run(root, "python", "-m", "py_compile", filepath.FromSlash(rel))
		if !r.Passed { return r, nil }
	}
	return Result{Tool: "python -m py_compile", Passed: true, Output: "Python compilation checks passed for touched Python files."}, nil
}

func Format(r Result) string {
	status := "PASS"
	if !r.Passed { status = "FAIL" }
	text := fmt.Sprintf("[%s] %s\n%s", status, r.Tool, r.Output)
	if len(r.Diagnostics) > 0 {
		text += "\n" + diagnostics.Summary(r.Diagnostics)
	}
	return text
}
