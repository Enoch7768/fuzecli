package api

import (
	"encoding/json"
	"reflect"
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
	cases := map[string]string{
		"workspace.apply_edits": `{}`,
		"workspace.apply_patch": `{}`,
		"workspace.rename": `{}`,
		"terminal.run": `{}`,
		"terminal.run_parallel": `{}`,
		"terminal.process": `{"action":"start"}`,
		"test.run": `{}`,
		"git.commit": `{}`,
		"git.branch": `{"action":"create"}`,
		"git.stash": `{"action":"push"}`,
		"http.request": `{"method":"POST"}`,
		"github.branches": `{"action":"create"}`,
		"github.sync": `{}`,
		"github.pull_requests": `{"action":"create"}`,
		"github.issues": `{"action":"create"}`,
		"github.actions": `{"action":"rerun_failed"}`,
		"github.auth": `{"action":"login_start"}`,
		"package.install": `{}`,
		"package.update": `{}`,
		"docs.generate": `{}`,
		"build.run": `{}`,
		"database.migrate": `{}`,
		"container.run": `{}`,
		"browser.click": `{}`,
		"browser.type": `{}`,
		"browser.select": `{}`,
		"browser.screenshot": `{"path":"screenshot.png"}`,
		"deploy.run": `{}`,
		"memory.forget": `{"memory_ids":["a","b","c","d","e","f"],"dry_run":false,"confirm":true}`,
		"profile.reset": `{"dry_run":false,"confirm":true}`,
	}
	for name, raw := range cases {
		if required, _ := agent.RequiresToolApproval(name, json.RawMessage(raw)); !required {
			t.Errorf("test fixture for %q does not require approval", name)
		}
		if !allowed[name] {
			t.Errorf("approval flow does not accept mutation tool %q", name)
		}
	}
}
