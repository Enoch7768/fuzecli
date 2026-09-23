package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/generation"
)

func TestExecutorRollsBackFailedVerification(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	executor := Executor{Root: root}
	plan := generation.Plan{Files: []generation.FileChange{
		{Path: "main.go", Content: "broken", Action: "modify"},
		{Path: "new.txt", Content: "created", Action: "create"},
	}}

	result, err := executor.Execute(plan, func() VerificationResult {
		return VerificationResult{Passed: false, Output: "go test failed"}
	})
	if err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("expected verification error, got %v", err)
	}
	if result.Passed {
		t.Fatal("failed verification reported as passed")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Fatalf("rollback did not restore original content: %q", content)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("rollback did not remove created file")
	}
}

func TestExecutorCommitsVerifiedPlan(t *testing.T) {
	root := t.TempDir()
	executor := Executor{Root: root}
	plan := generation.Plan{Files: []generation.FileChange{
		{Path: "app.txt", Content: "ready", Action: "create"},
	}}

	result, err := executor.Execute(plan, func() VerificationResult {
		return VerificationResult{Passed: true, Output: "ok"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("verified plan was not reported as passed")
	}
	content, err := os.ReadFile(filepath.Join(root, "app.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "ready" {
		t.Fatalf("unexpected committed content: %q", content)
	}
}
