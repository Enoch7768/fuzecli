package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/agent"
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

func TestApprovalAllowlistCoversEveryMutationTool(t *testing.T) {
	allowed := supportedApprovalToolNames()
	for _, name := range []string{
		"workspace.apply_edits", "workspace.apply_patch", "workspace.rename",
		"terminal.run", "terminal.run_parallel", "terminal.process", "test.run",
		"git.commit", "git.branch", "git.stash", "http.request",
		"github.branches", "github.sync", "github.pull_requests", "github.issues", "github.actions", "github.auth",
		"package.install", "package.update", "docs.generate", "build.run", "database.migrate", "container.run",
		"browser.click", "browser.type", "browser.select", "browser.screenshot", "deploy.run",
	} {
		if !allowed[name] {
			t.Errorf("approval flow does not accept mutation tool %q", name)
		}
	}
	for name := range allowed {
		if strings.TrimSpace(name) == "" {
			t.Fatal("approval allowlist contains an empty tool name")
		}
	}
	_ = agent.ToolProtocolVersion
}
