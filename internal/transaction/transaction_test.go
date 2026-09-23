package transaction

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRollbackRestoresModifiedAndCreatedFiles(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing.txt")
	created := filepath.Join(root, "nested", "created.txt")
	if err := os.WriteFile(existing, []byte("original"), 0640); err != nil {
		t.Fatal(err)
	}

	tx := New()
	if err := tx.Write(existing, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := tx.Write(created, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("existing content = %q", string(got))
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("created file still exists: %v", err)
	}
}

func TestRollbackRestoresDeletedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "delete.txt")
	if err := os.WriteFile(path, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	tx := New()
	if err := tx.Delete(path); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" {
		t.Fatalf("restored content = %q", string(got))
	}
}

func TestCommitKeepsChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "committed.txt")

	tx := New()
	if err := tx.Write(path, []byte("committed"), 0644); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "committed" {
		t.Fatalf("content = %q", string(got))
	}
}

func TestClosedTransactionRejectsChanges(t *testing.T) {
	tx := New()
	tx.Commit()
	if err := tx.Capture("file.txt"); err == nil {
		t.Fatal("expected closed transaction to reject capture")
	}
}
