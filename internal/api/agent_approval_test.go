package api

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestToolDeletionPathsRequireExplicitPathApproval(t *testing.T) {
	raw := json.RawMessage(`{"files":[{"path":"dist/","action":"delete"},{"path":"src/app.go","action":"modify","content":"x"},{"path":"node_modules\\cache","action":"delete"}]}`)
	got := toolDeletionPaths("workspace.apply_edits", raw)
	want := []string{"dist", "node_modules/cache"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("toolDeletionPaths() = %#v, want %#v", got, want)
	}
	if got := toolDeletionPaths("workspace.read_file", raw); len(got) != 0 {
		t.Fatalf("read-only tool returned deletion targets: %#v", got)
	}
}

func TestCanonicalWorkspacePath(t *testing.T) {
	for input, want := range map[string]string{
		"dist/": "dist",
		"src\\app.go": "src/app.go",
		"./build/output": "build/output",
	} {
		if got := canonicalWorkspacePath(input); got != want {
			t.Errorf("canonicalWorkspacePath(%q) = %q, want %q", input, got, want)
		}
	}
}


func TestToolRequestContainsOnlyDeletes(t *testing.T) {
	if !toolRequestContainsOnlyDeletes(json.RawMessage(`{"files":[{"path":"dist","action":"delete"}]}`)) {
		t.Fatal("delete-only request was not recognized")
	}
	for _, raw := range []string{
		`{"files":[{"path":"dist","action":"delete"},{"path":"src/a.go","action":"modify","content":"x"}]}`,
		`{"files":[]}`,
		`not-json`,
	} {
		if toolRequestContainsOnlyDeletes(json.RawMessage(raw)) {
			t.Errorf("non-delete-only request accepted: %s", raw)
		}
	}
}
