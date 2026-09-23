package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectGoProject(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n\ngo 1.25\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Detect(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tool != "go test" && result.Tool != "go test + go vet + gofmt" {
		t.Fatalf("expected go verifier, got %q", result.Tool)
	}
	if result.Passed {
		t.Fatal("expected empty Go module test to fail")
	}
}

func TestDetectNodeProjectWithoutLockfile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Detect(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tool != "node" || !result.Passed {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestLintPHPUsesTouchedFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test.php")
	if err := os.WriteFile(path, []byte("<?php echo 'ok';"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := lintPHP(root, []string{"test.php"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected PHP syntax check to pass: %+v", result)
	}
}
