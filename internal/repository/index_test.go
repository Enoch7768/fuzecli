package repository

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildIndexesGoSymbolsAndImports(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nimport \"fmt\"\n\ntype Server struct{}\nfunc Run() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	index, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Files) != 1 {
		t.Fatalf("expected one indexed file, got %d", len(index.Files))
	}
	file := index.Files[0]
	if file.Language != "go" {
		t.Fatalf("expected go language, got %q", file.Language)
	}
	if !contains(file.Imports, "fmt") {
		t.Fatalf("expected fmt import, got %#v", file.Imports)
	}
	if !contains(file.Symbols, "Server") || !contains(file.Symbols, "Run") {
		t.Fatalf("expected Go symbols, got %#v", file.Symbols)
	}
}

func TestBuildHonorsIgnorePolicy(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	index, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Files) != 1 || index.Files[0].Path != "main.go" {
		t.Fatalf("unexpected indexed files: %#v", index.Files)
	}
}

func TestRelevantRanksPathAndSymbolMatches(t *testing.T) {
	index := &Index{Files: []File{
		{Path: "internal/api/server.go", Language: "go", Symbols: []string{"Server", "HandleChat"}},
		{Path: "internal/config/config.go", Language: "go", Symbols: []string{"Config"}},
	}}
	results := index.Relevant("fix Server chat handler", 2)
	if len(results) != 1 || results[0].Path != "internal/api/server.go" {
		t.Fatalf("unexpected relevance results: %#v", results)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
