package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/diagnostics"
	"github.com/Enoch7768/fuzecli/internal/security"
)

type Result struct {
	Tool        string
	Passed      bool
	Output      string
	Diagnostics []diagnostics.Diagnostic
}

type packageManifest struct {
	PackageManager string            `json:"packageManager"`
	Scripts        map[string]string `json:"scripts"`
}

func Detect(root string, touched []string) (Result, error) {
	has := func(name string) bool { _, e := os.Stat(filepath.Join(root, name)); return e == nil }
	switch {
	case has("go.mod"):
		return verifyGo(root)
	case has("package.json"):
		return verifyNode(root, has)
	case has("composer.json"):
		return lintPHP(root, touched)
	case has("requirements.txt") || has("pyproject.toml"):
		return lintPython(root, touched)
	default:
		return Result{Tool: "none", Passed: true, Output: "No supported toolchain detected; verification skipped."}, nil
	}
}

func verifyNode(root string, has func(string) bool) (Result, error) {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return Result{}, fmt.Errorf("read package.json: %w", err)
	}
	var manifest packageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Result{Tool: "package.json", Passed: false, Output: "Could not parse package.json: " + err.Error()}, nil
	}

	manager := detectNodeManager(root, manifest.PackageManager, has)
	if manager == "" {
		return Result{Tool: "node", Passed: true, Output: "Node project detected; no supported lockfile was found, so package scripts were not executed."}, nil
	}

	if strings.TrimSpace(manifest.Scripts["test"]) != "" {
		name, args := nodeManagerCommand(manager, "test")
		return run(root, name, args...)
	}
	if has("tsconfig.json") && strings.TrimSpace(manifest.Scripts["typecheck"]) != "" {
		name, args := nodeManagerCommand(manager, "run", "typecheck")
		return run(root, name, args...)
	}
	return Result{Tool: manager, Passed: true, Output: "Node project detected; package.json defines no test or typecheck script to run."}, nil
}

func detectNodeManager(root, declared string, has func(string) bool) string {
	declared = strings.TrimSpace(declared)
	if at := strings.IndexByte(declared, '@'); at > 0 {
		declared = declared[:at]
	}
	locks := []struct {
		name    string
		manager string
	}{
		{name: "package-lock.json", manager: "npm"},
		{name: "pnpm-lock.yaml", manager: "pnpm"},
		{name: "yarn.lock", manager: "yarn"},
		{name: "bun.lock", manager: "bun"},
		{name: "bun.lockb", manager: "bun"},
	}
	if declared != "" {
		for _, lock := range locks {
			if lock.manager == declared && has(lock.name) {
				return declared
			}
		}
	}
	for _, lock := range locks {
		if has(lock.name) {
			return lock.manager
		}
	}
	return ""
}

func nodeManagerCommand(manager string, args ...string) (string, []string) {
	switch manager {
	case "npm", "pnpm":
		return manager, append([]string{"run"}, args...)
	case "yarn":
		return "yarn", args
	case "bun":
		return "bun", append([]string{"run"}, args...)
	default:
		return manager, args
	}
}

func verifyGo(root string) (Result, error) {
	checks := []struct {
		name string
		args []string
	}{
		{name: "gofmt", args: []string{"gofmt", "-l", "."}},
		{name: "go test", args: []string{"go", "test", "./..."}},
		{name: "go vet", args: []string{"go", "vet", "./..."}},
	}
	var outputs []string
	for _, check := range checks {
		result, err := run(root, check.args[0], check.args[1:]...)
		if err != nil {
			return Result{}, err
		}
		if check.name == "gofmt" && result.Passed && strings.TrimSpace(result.Output) != "" {
			result.Passed = false
		}
		outputs = append(outputs, fmt.Sprintf("[%s] %s", check.name, result.Output))
		if !result.Passed {
			result.Tool = check.name
			result.Output = strings.Join(outputs, "\n")
			return result, nil
		}
	}
	return Result{Tool: "go test + go vet + gofmt", Passed: true, Output: strings.Join(outputs, "\n")}, nil
}

func run(root string, name string, args ...string) (Result, error) {
	decision := security.Authorize(security.Command{Name: name, Args: args})
	if !decision.Allowed {
		return Result{Tool: name, Passed: false, Output: "verification command blocked by security policy: " + decision.Reason}, nil
	}
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
		if filepath.Ext(rel) != ".php" {
			continue
		}
		r, _ := run(root, "php", "-l", filepath.FromSlash(rel))
		if !r.Passed {
			return r, nil
		}
	}
	return Result{Tool: "php -l", Passed: true, Output: "PHP syntax checks passed for touched PHP files."}, nil
}

func lintPython(root string, touched []string) (Result, error) {
	for _, rel := range touched {
		if filepath.Ext(rel) != ".py" {
			continue
		}
		r, _ := run(root, "python", "-m", "py_compile", filepath.FromSlash(rel))
		if !r.Passed {
			return r, nil
		}
	}
	return Result{Tool: "python -m py_compile", Passed: true, Output: "Python compilation checks passed for touched Python files."}, nil
}

func Format(r Result) string {
	status := "PASS"
	if !r.Passed {
		status = "FAIL"
	}
	text := fmt.Sprintf("[%s] %s\n%s", status, r.Tool, r.Output)
	if len(r.Diagnostics) > 0 {
		text += "\n" + diagnostics.Summary(r.Diagnostics)
	}
	return text
}
