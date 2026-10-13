package generation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestorePlanSnapshotRevertsModifiedAndCreatedFiles(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "main.go")
	if err := os.WriteFile(existing, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	plan := Plan{Files: []FileChange{
		{Path: "main.go", Content: "package main\nfunc broken( {\n", Action: "modify"},
		{Path: "new.go", Content: "package main\n", Action: "create"},
	}}
	snapshot, err := CreatePlanSnapshot(root, plan)
	if err != nil {
		t.Fatalf("CreatePlanSnapshot returned error: %v", err)
	}
	if _, err := Apply(root, plan); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "new.go")); err != nil {
		t.Fatalf("expected created file before rollback: %v", err)
	}
	if err := RestorePlanSnapshot(root, snapshot); err != nil {
		t.Fatalf("RestorePlanSnapshot returned error: %v", err)
	}
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package main\n" {
		t.Fatalf("existing file after rollback = %q", data)
	}
	if _, err := os.Stat(filepath.Join(root, "new.go")); !os.IsNotExist(err) {
		t.Fatalf("new file still exists after rollback, stat error = %v", err)
	}
}
