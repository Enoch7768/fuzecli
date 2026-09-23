package generation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePlanRejectsFencesAndUnknownFields(t *testing.T) {
	if _, err := ParsePlan("```json\n{}\n```"); err == nil {
		t.Fatal("expected fence response to fail")
	}
	if _, err := ParsePlan(`{"files":[{"path":"x.go","content":"package x","action":"create","extra":1}],"explanation":"x","commands":[]}`); err == nil {
		t.Fatal("expected unknown field to fail")
	}
}

func TestResolveRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"../x.txt", "foo/../../x", `/absolute.txt`} {
		if _, err := Resolve(root, p); err == nil {
			t.Fatalf("expected rejection for %s", p)
		}
	}
}

func TestApplyCreatesAndDeletes(t *testing.T) {
	root := t.TempDir()
	plan := Plan{Files: []FileChange{{Path: "src/main.go", Content: "package main\n", Action: "create"}}}
	if _, err := Apply(root, plan); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "src", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "package main\n" {
		t.Fatalf("content = %q", string(b))
	}
	if _, err := Apply(root, Plan{Files: []FileChange{{Path: "src/main.go", Action: "delete"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyPreventsPartialChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	plan := Plan{Files: []FileChange{
		{Path: "existing.txt", Content: "changed", Action: "modify"},
		{Path: "existing.txt", Content: "again", Action: "modify"},
	}}
	if _, err := Apply(root, plan); err == nil {
		t.Fatal("expected duplicate plan to fail")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "original" {
		t.Fatalf("partial change was applied: %q", string(b))
	}
}
