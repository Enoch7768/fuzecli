package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectGoProject(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n\ngo 1.25\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc answer() int { return 42 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package main\n\nimport \"testing\"\n\nfunc TestFailure(t *testing.T) {\n\tt.Fatal(\"intentional verification failure\")\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Detect(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tool != "gofmt" && result.Tool != "go test" && result.Tool != "go test + go vet + gofmt" {
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

func TestDetectNodeManagerUsesDeclaredLockfile(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"package-lock.json", "pnpm-lock.yaml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(""), 0600); err != nil {
			t.Fatal(err)
		}
	}
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}
	if got := detectNodeManager(root, "pnpm@9.0.0", has); got != "pnpm" {
		t.Fatalf("declared pnpm manager = %q, want pnpm", got)
	}
	if got := detectNodeManager(root, "", has); got != "npm" {
		t.Fatalf("default manager = %q, want npm", got)
	}
}

func TestDetectNodeManagerSupportsYarnAndBunLocks(t *testing.T) {
	for _, tc := range []struct {
		lock string
		want string
	}{
		{lock: "yarn.lock", want: "yarn"},
		{lock: "bun.lock", want: "bun"},
		{lock: "bun.lockb", want: "bun"},
	} {
		t.Run(tc.lock, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, tc.lock), []byte(""), 0600); err != nil {
				t.Fatal(err)
			}
			has := func(name string) bool {
				_, err := os.Stat(filepath.Join(root, name))
				return err == nil
			}
			if got := detectNodeManager(root, "", has); got != tc.want {
				t.Fatalf("detected manager = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNodeManagerCommand(t *testing.T) {
	for _, tc := range []struct {
		manager string
		input   []string
		command string
		args    string
	}{
		{manager: "npm", input: []string{"run", "test"}, command: "npm", args: "run test"},
		{manager: "pnpm", input: []string{"run", "test"}, command: "pnpm", args: "run test"},
		{manager: "yarn", input: []string{"test"}, command: "yarn", args: "test"},
		{manager: "bun", input: []string{"run", "test"}, command: "bun", args: "run test"},
	} {
		t.Run(tc.manager, func(t *testing.T) {
			command, args := nodeManagerCommand(tc.manager, "test")
			if command != tc.command || strings.Join(args, " ") != tc.args {
				t.Fatalf("command = %q %v, want %q %q", command, args, tc.command, tc.args)
			}
		})
	}
}
