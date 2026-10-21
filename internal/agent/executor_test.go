package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/generation"
	"github.com/Enoch7768/fuzecli/internal/security"
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

func TestExecutorDeniesDeleteByDefault(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(path, []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := (Executor{Root: root}).Execute(generation.Plan{Files: []generation.FileChange{
		{Path: "keep.txt", Action: "delete"},
	}}, nil)
	if err == nil || !strings.Contains(err.Error(), string(security.DeleteFile)) {
		t.Fatalf("expected delete capability denial, got %v", err)
	}
	if content, readErr := os.ReadFile(path); readErr != nil || string(content) != "keep me" {
		t.Fatalf("file should remain untouched after denied delete: content=%q err=%v", content, readErr)
	}
}

func TestExecutorDeletesWhenExplicitlyAuthorized(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "remove.txt")
	if err := os.WriteFile(path, []byte("remove me"), 0644); err != nil {
		t.Fatal(err)
	}
	capabilities := security.SafeCapabilities()
	capabilities[security.DeleteFile] = true

	result, err := (Executor{Root: root, Capabilities: capabilities}).Execute(generation.Plan{Files: []generation.FileChange{
		{Path: "remove.txt", Action: "delete"},
	}}, func() VerificationResult {
		return VerificationResult{Passed: true, Output: "ok"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("authorized delete was not reported as verified")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted, got stat error %v", err)
	}
}

func TestExecutorRollsBackWhenVerificationPanics(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := (Executor{Root: root}).Execute(generation.Plan{Files: []generation.FileChange{
		{Path: "main.go", Content: "modified", Action: "modify"},
		{Path: "new.txt", Content: "created", Action: "create"},
	}}, func() VerificationResult {
		panic("verification crashed")
	})
	if err == nil || !strings.Contains(err.Error(), "verification panicked: verification crashed") {
		t.Fatalf("expected verification panic error, got %v", err)
	}
	if result.Passed || !strings.Contains(result.Output, "verification panicked") {
		t.Fatalf("panic was not reported as failed verification: %+v", result)
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
