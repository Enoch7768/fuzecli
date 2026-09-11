package intelligence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildAndSearch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc HelloWorld() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("API_KEY=do-not-index"), 0600); err != nil {
		t.Fatal(err)
	}
	idx, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Files) != 1 || idx.Files[0] != "main.go" {
		t.Fatalf("unexpected files: %#v", idx.Files)
	}
	if len(idx.Symbols) != 1 || idx.Symbols[0].Name != "HelloWorld" {
		t.Fatalf("unexpected symbols: %#v", idx.Symbols)
	}
	matches, err := idx.Search("hello world", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected line search to require same-line terms, got %#v", matches)
	}
	matches, err = idx.Search("HelloWorld", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Path != "main.go" {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestFindSymbols(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "service.py"), []byte("def process_order():\n    pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	idx, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	got := idx.FindSymbols("process", 10)
	if len(got) != 1 || got[0].Name != "process_order" || got[0].Kind != "function" {
		t.Fatalf("unexpected symbols: %#v", got)
	}
}
