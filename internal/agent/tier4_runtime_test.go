package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTier4MemoryProfileToolsPersistSafely(t *testing.T) {
	root := t.TempDir()
	registry, err := NewFuzeCLIToolRegistry(ToolRuntime{Root: root})
	if err != nil { t.Fatal(err) }
	run := func(id, name, args string, approve bool) ToolResult {
		var authorizer ToolAuthorizer
		if approve { authorizer = func(context.Context, ToolDefinition, json.RawMessage) error { return nil } }
		return registry.Execute(context.Background(), ToolCall{ID:id, Name:name, Arguments:json.RawMessage(args)}, authorizer)
	}
	remember := run("remember", "memory.remember", `{"content":"Use concise beginner-friendly explanations for coding tasks.","kind":"preference","scope":"project","tags":["writing"]}`, false)
	if remember.Status != ToolResultSuccess { t.Fatalf("remember failed: %s", remember.Error) }
	var created map[string]any
	if err := json.Unmarshal(remember.Result, &created); err != nil { t.Fatal(err) }
	id, _ := created["memory_id"].(string)
	if !strings.HasPrefix(id, "mem_") { t.Fatalf("unexpected memory id: %q", id) }

	recall := run("recall", "memory.recall", `{"query":"beginner-friendly coding explanations","kind":"any","scope":"any","limit":5}`, false)
	if recall.Status != ToolResultSuccess || !strings.Contains(string(recall.Result), "concise beginner-friendly") { t.Fatalf("recall failed or missing memory: %#v", recall) }

	profile := run("profile.update", "profile.update", `{"timezone":"Africa/Kampala","tone_preference":"concise"}`, false)
	if profile.Status != ToolResultSuccess { t.Fatalf("profile update failed: %s", profile.Error) }
	profileRead := run("profile.get", "profile.get", `{}`, false)
	if profileRead.Status != ToolResultSuccess || !strings.Contains(string(profileRead.Result), "Africa/Kampala") { t.Fatalf("profile read failed: %#v", profileRead) }

	statePath := filepath.Join(root, ".aicli", "agent_state.json")
	info, err := os.Stat(statePath)
	if err != nil { t.Fatalf("state was not persisted: %v", err) }
	if info.Mode().Perm()&0077 != 0 { t.Fatalf("state file should not be group/world accessible: %v", info.Mode().Perm()) }

	forgetPreview := run("forget-preview", "memory.forget", `{"memory_ids":["`+id+`"],"dry_run":true,"confirm":false}`, false)
	if forgetPreview.Status != ToolResultSuccess || !strings.Contains(string(forgetPreview.Result), "would_forget") { t.Fatalf("forget preview failed: %#v", forgetPreview) }
	if _, err := os.Stat(statePath); err != nil { t.Fatal(err) }
}

func TestTier4DestructiveToolsRequireApproval(t *testing.T) {
	registry, err := NewFuzeCLIToolRegistry(ToolRuntime{Root:t.TempDir()})
	if err != nil { t.Fatal(err) }
	cases := []ToolCall{
		{ID:"forget",Name:"memory.forget",Arguments:json.RawMessage(`{"memory_ids":["mem_a","mem_b","mem_c","mem_d","mem_e","mem_f"],"dry_run":false,"confirm":true}`)},
		{ID:"reset",Name:"profile.reset",Arguments:json.RawMessage(`{"dry_run":false,"confirm":true}`)},
	}
	for _, call := range cases {
		got := registry.Execute(context.Background(), call, nil)
		if got.Status != ToolResultError || !strings.Contains(got.Error, "approval") { t.Errorf("%s should require explicit approval, got %#v", call.Name, got) }
	}
}
