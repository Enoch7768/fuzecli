package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Enoch7768/fuzecli/internal/generation"
)

type evaluationFixture struct {
	language, scenario, extension, before, after string
}

func TestDeterministicAgentEvaluationHarness(t *testing.T) {
	languages := []struct{ name, ext, before, after string }{
		{"go", ".go", "package fixture\nfunc Value() int { return 0 }\n", "package fixture\nfunc Value() int { return 42 }\n"},
		{"javascript", ".js", "function value() { return 0; }\n", "function value() { return 42; }\n"},
		{"typescript", ".ts", "export function value(): number { return 0; }\n", "export function value(): number { return 42; }\n"},
		{"python", ".py", "def value():\n    return 0\n", "def value():\n    return 42\n"},
		{"php", ".php", "<?php\nfunction value(): int { return 0; }\n", "<?php\nfunction value(): int { return 42; }\n"},
	}
	scenarios := []string{"clean-feature", "bug-fix", "behavior-preserving-refactor", "multi-file-change", "verification-repair", "verification-rollback", "malformed-json-recovery", "unrelated-file-guard"}
	fixtures := make([]evaluationFixture, 0, len(languages)*len(scenarios))
	for _, language := range languages {
		for _, scenario := range scenarios {
			fixtures = append(fixtures, evaluationFixture{language: language.name, scenario: scenario, extension: language.ext, before: language.before, after: language.after})
		}
	}

	started := time.Now()
	passed, unrelatedChanges, repairAttempts := 0, 0, 0
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fmt.Sprintf("%s/%s", fixture.language, fixture.scenario), func(t *testing.T) {
			root := t.TempDir()
			sourcePath := filepath.ToSlash(filepath.Join("src", "main"+fixture.extension))
			writeFixture(t, root, sourcePath, fixture.before)
			const unrelatedPath = "notes.txt"
			const unrelatedContent = "keep this unrelated user content unchanged\n"
			writeFixture(t, root, unrelatedPath, unrelatedContent)
			plannedContent := fixture.after
			if fixture.scenario == "behavior-preserving-refactor" {
				plannedContent = refactoredSource(fixture.language)
			}
			files := []generation.FileChange{{Path: sourcePath, Content: plannedContent, Action: "modify"}}
			if fixture.scenario == "clean-feature" {
				files = []generation.FileChange{{Path: filepath.ToSlash(filepath.Join("src", "feature"+fixture.extension)), Content: fixture.after, Action: "create"}}
			}
			if fixture.scenario == "multi-file-change" {
				files = append(files, generation.FileChange{Path: filepath.ToSlash(filepath.Join("src", "feature"+fixture.extension)), Content: fixture.after, Action: "create"})
			}
			raw, err := json.Marshal(generation.Plan{Files: files, Explanation: fixture.scenario})
			if err != nil { t.Fatal(err) }
			if fixture.scenario == "malformed-json-recovery" {
				raw = []byte(strings.TrimSuffix(string(raw), "}") + ",}")
			}
			plan, err := generation.ParsePlan(string(raw))
			if err != nil { t.Fatalf("parse fixture plan: %v", err) }
			verify := func() VerificationResult {
				for _, change := range plan.Files {
					data, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(change.Path)))
					if readErr != nil || string(data) != change.Content {
						return VerificationResult{Passed: false, Output: fmt.Sprintf("verification mismatch for %s", change.Path)}
					}
				}
				if fixture.scenario == "clean-feature" {
					original, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(sourcePath)))
					if readErr != nil || string(original) != fixture.before {
						return VerificationResult{Passed: false, Output: "feature addition modified the existing source file"}
					}
				}
				if fixture.scenario == "behavior-preserving-refactor" && !preservesZeroBehavior(fixture.language, plan.Files[0].Content) {
					return VerificationResult{Passed: false, Output: "refactor changed the expected observable result"}
				}
				if fixture.scenario == "bug-fix" && !strings.Contains(plan.Files[0].Content, "42") {
					return VerificationResult{Passed: false, Output: "bug fix did not change the failing return value"}
				}
				data, readErr := os.ReadFile(filepath.Join(root, unrelatedPath))
				if readErr != nil || string(data) != unrelatedContent {
					unrelatedChanges++
					return VerificationResult{Passed: false, Output: "unrelated file changed"}
				}
				return VerificationResult{Passed: true, Output: "fixture assertions passed"}
			}
			executor := Executor{Root: root}
			if fixture.scenario == "verification-repair" {
				initialPlan := plan
				initialPlan.Files = append([]generation.FileChange(nil), plan.Files...)
				initialPlan.Files[0].Content += "invalid repair candidate"
				result, executeErr := executor.Execute(initialPlan, verify)
				if executeErr == nil || result.Passed { t.Fatal("invalid repair candidate must fail verification") }
				original, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(sourcePath)))
				if readErr != nil || string(original) != fixture.before { t.Fatal("failed repair candidate did not roll back") }
				repairAttempts++
			}
			if fixture.scenario == "verification-rollback" {
				result, executeErr := executor.Execute(plan, func() VerificationResult {
					return VerificationResult{Passed: false, Output: "injected deterministic verification failure"}
				})
				if executeErr == nil || result.Passed { t.Fatal("verification failure must be rejected") }
				original, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(sourcePath)))
				if readErr != nil || string(original) != fixture.before { t.Fatal("failed verification did not restore original file") }
				passed++
				return
			}
			result, executeErr := executor.Execute(plan, verify)
			if executeErr != nil { t.Fatalf("execute fixture: %v", executeErr) }
			if !result.Passed { t.Fatal("fixture did not pass verification") }
			passed++
		})
	}

	unsafePaths := []string{"../outside.txt", "../../outside.txt", "src/../../../outside.txt", "src/../../.env", "../.git/config", "./../../outside.txt", "a/../../b.txt", "src/..//../../outside.txt"}
	for i, path := range unsafePaths {
		t.Run(fmt.Sprintf("security/path-escape-%02d", i+1), func(t *testing.T) {
			root := t.TempDir()
			_, err := (Executor{Root: root}).Execute(generation.Plan{Files: []generation.FileChange{{Path: path, Content: "must not write", Action: "create"}}}, func() VerificationResult { return VerificationResult{Passed: true} })
			if err == nil { t.Fatalf("unsafe path %q was accepted", path) }
			passed++
		})
	}
	elapsed := time.Since(started)
	total := len(fixtures) + len(unsafePaths)
	report := map[string]any{
		"fixture_count": total,
		"passed_assertion_groups": passed,
		"success_rate_percent": float64(passed)*100/float64(total),
		"unrelated_file_changes": unrelatedChanges,
		"repair_attempts": repairAttempts,
		"latency_ms": elapsed.Milliseconds(),
		"scope": "deterministic transaction, parsing, rollback, and path-safety fixtures; not a live-model quality score",
	}
	encoded, err := json.Marshal(report)
	if err != nil { t.Fatal(err) }
	t.Logf("FUZECLI_AGENT_EVALUATION %s", encoded)
	if unrelatedChanges != 0 { t.Fatalf("detected %d unrelated file changes", unrelatedChanges) }
	if passed != total { t.Fatalf("only %d of %d fixture assertion groups passed", passed, total) }
}

func refactoredSource(language string) string {
	switch language {
	case "go":
		return "package fixture\nfunc Value() int { result := 0; return result }\n"
	case "javascript":
		return "function value() { const result = 0; return result; }\n"
	case "typescript":
		return "export function value(): number { const result = 0; return result; }\n"
	case "python":
		return "def value():\n    result = 0\n    return result\n"
	case "php":
		return "<?php\nfunction value(): int { $result = 0; return $result; }\n"
	default:
		return ""
	}
}

func preservesZeroBehavior(language, content string) bool {
	if strings.Contains(content, "return 42") || strings.Contains(content, "return 0") {
		return false
	}
	switch language {
	case "go":
		return strings.Contains(content, "result := 0") && strings.Contains(content, "return result")
	case "javascript", "typescript", "python":
		return strings.Contains(content, "result = 0") && strings.Contains(content, "return result")
	case "php":
		return strings.Contains(content, "$result = 0") && strings.Contains(content, "return $result")
	default:
		return false
	}
}

func writeFixture(t *testing.T, root, relative, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(full, []byte(content), 0644); err != nil { t.Fatal(err) }
}
