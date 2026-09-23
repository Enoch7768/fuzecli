package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/generation"
)

func TestAgentEndToEndRepairsRealGoWorkspace(t *testing.T) {
	root := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/e2e\n\ngo 1.25\n")
	write("main.go", "package main\n\nfunc answer() int { return }\n")

	executor := Executor{Root: root}
	broken := generation.Plan{Files: []generation.FileChange{
		{Path: "main.go", Content: "package main\n\nfunc answer() int { return }\n", Action: "modify"},
	}}

	var verifyCalls int
	verify := func() VerificationResult {
		verifyCalls++
		cmd := exec.Command("go", "test", "./...")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		return VerificationResult{
			Passed: err == nil,
			Output: strings.TrimSpace(string(out)),
		}
	}

	if result, err := executor.Execute(broken, verify); err == nil || result.Passed {
		t.Fatalf("broken plan unexpectedly passed: result=%#v err=%v", result, err)
	}

	write("main.go", "package main\n\nfunc answer() int { return 42 }\n")

	repaired := generation.Plan{Files: []generation.FileChange{
		{Path: "main.go", Content: "package main\n\nfunc answer() int { return 42 }\n", Action: "modify"},
	}}

	result, err := executor.Execute(repaired, verify)
	if err != nil {
		t.Fatalf("repaired plan failed: %v", err)
	}
	if !result.Passed {
		t.Fatal("repaired plan was not verified")
	}
	if verifyCalls != 2 {
		t.Fatalf("expected two real verification runs, got %d", verifyCalls)
	}

	var events []Phase
	loop := Loop{
		MaxRepairAttempts: 1,
		Plan:              func(context.Context) error { return nil },
		Context:           func(context.Context) error { return nil },
		Edit:              func(context.Context) error { return nil },
		Verify:            func(context.Context) error {
			cmd := exec.Command("go", "test", "./...")
			cmd.Dir = root
			return cmd.Run()
		},
		OnEvent: func(event Event) {
			if event.Phase == PhasePlan || event.Phase == PhaseContext || event.Phase == PhaseEdit || event.Phase == PhaseVerify || event.Phase == PhaseComplete {
				events = append(events, event.Phase)
			}
		},
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatalf("agent loop failed against real workspace: %v", err)
	}
	expected := []Phase{PhasePlan, PhaseContext, PhaseEdit, PhaseVerify, PhaseComplete}
	if len(events) != len(expected) {
		t.Fatalf("unexpected end-to-end event count: got %d want %d", len(events), len(expected))
	}
	for i, want := range expected {
		if events[i] != want {
			t.Fatalf("event %d = %q, want %q", i, events[i], want)
		}
	}
}
