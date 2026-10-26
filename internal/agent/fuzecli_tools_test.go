package agent

import (
 "context"
 "database/sql"
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestFuzeCLIToolDefinitions(t *testing.T) {
 defs := FuzeCLIToolDefinitions()
 if len(defs) != 45 { t.Fatalf("want 45 tools after Tier 1, got %d", len(defs)) }
 for _, d := range defs {
  if !json.Valid(d.InputSchema) { t.Errorf("%s has invalid schema", d.Name) }
  var s map[string]any
  if err := json.Unmarshal(d.InputSchema, &s); err != nil { t.Fatal(err) }
  if s["type"] != "object" || s["additionalProperties"] != false { t.Errorf("%s is not strict", d.Name) }
 }
}
func toolDef(name string) ToolDefinition { for _, d := range FuzeCLIToolDefinitions() { if d.Name == name { return d } }; return ToolDefinition{} }
func TestToolSchemaRejectsInvalidArguments(t *testing.T) {
 d := toolDef("workspace.read_file")
 for _, raw := range []string{`{}`, `{"path":"a.go","extra":true}`, `{"path":""}`, `{"path":"a.go","start_line":0}`, `[]`} {
  if err := ValidateToolArguments(d, json.RawMessage(raw)); err == nil { t.Errorf("accepted invalid arguments %s", raw) }
 }
 if err := ValidateToolArguments(d, json.RawMessage(`{"path":"a.go","start_line":2,"end_line":4}`)); err != nil { t.Fatal(err) }
}
func TestToolSchemaEditConditionalRules(t *testing.T) {
 d := toolDef("workspace.apply_edits")
 cases := []struct{raw, want string}{
  {`{"files":[{"path":"a.txt","action":"create"}]}`, "content is required"},
  {`{"files":[{"path":"a.txt","action":"delete","content":""}]}`, "content must be absent"},
  {`{"files":[{"path":"a.txt","action":"modify","content":""}]}`, ""},
 }
 for _, tc := range cases {
  err := ValidateToolArguments(d, json.RawMessage(tc.raw))
  if tc.want == "" && err != nil { t.Errorf("valid edit rejected: %v", err) }
  if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) { t.Errorf("wanted %q, got %v", tc.want, err) }
 }
}
func TestToolRegistryValidatesSchemaBeforeHandler(t *testing.T) {
 r := NewToolRegistry()
 d := toolDef("workspace.read_file")
 called := false
 if err := r.Register(d, func(context.Context,json.RawMessage)(any,error){ called=true; return map[string]bool{"ok":true},nil }); err != nil { t.Fatal(err) }
 got := r.Execute(context.Background(), ToolCall{ID:"bad",Name:d.Name,Arguments:json.RawMessage(`{"path":"a.txt","unexpected":true}`)}, nil)
 if got.Status != ToolResultError || called { t.Fatalf("invalid call reached handler: %#v", got) }
 got = r.Execute(context.Background(), ToolCall{ID:"good",Name:d.Name,Arguments:json.RawMessage(`{"path":"a.txt"}`)}, nil)
 if got.Status != ToolResultSuccess || !called { t.Fatalf("valid call failed: %#v", got) }
}


func TestFuzeAICompatibilityNormalization(t *testing.T) {
	cases := []struct {
		name string
		raw string
		want string
	}{
		{"workspace.search_code", `{"query":"TODO","limit":20}`, `"max_results":20`},
		{"workspace.apply_edits", `{"operation":"prepare_create","path":"tmp/test.txt","content":"hello"}`, `"dry_run":true`},
		{"project.health", `{"checks":["runtime","git_status"]}`, `"include_runtimes":true`},
		{"verify.run", `{"checks":[{"name":"Timeout test","timeout_seconds":60,"run":false}]}`, `"checks":["test"]`},
		{"git.status", `{"untracked_files":true}`, `"include_untracked":true`},
		{"git.commit", `{"message":"test","expected_head":"abc123","commit":false}`, `"dry_run":true`},
		{"github.repositories", `{"page":1,"per_page":30}`, `"action":"list"`},
		{"github.branches", `{"repository":"owner/repo"}`, `"action":"list"`},
		{"github.sync", `{"operation":"prepare_pull","base_branch":"main","remote":"origin","sync":false}`, `"dry_run":true`},
		{"github.pull_requests", `{"repository":"owner/repo","state":"all"}`, `"action":"list"`},
		{"github.issues", `{"repository":"owner/repo","state":"all"}`, `"action":"list"`},
		{"context.get", `{"key":"recent","limit":5}`, `"scope":"recent_history"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := toolDef(tc.name)
			if err := ValidateToolArguments(def, json.RawMessage(tc.raw)); err != nil {
				t.Fatalf("compatible arguments rejected: %v", err)
			}
			normalized, err := normalizeToolArguments(tc.name, json.RawMessage(tc.raw))
			if err != nil { t.Fatal(err) }
			if !strings.Contains(string(normalized), tc.want) {
				t.Fatalf("normalized arguments %s do not contain %s", normalized, tc.want)
			}
		})
	}
}

func TestFuzeAIDryRunsDoNotMutateOrExecute(t *testing.T) {
	root := t.TempDir()
	registry, err := NewFuzeCLIToolRegistry(ToolRuntime{Root: root})
	if err != nil { t.Fatal(err) }

	edit := registry.Execute(context.Background(), ToolCall{
		ID: "preview-edit", Name: "workspace.apply_edits",
		Arguments: json.RawMessage(`{"operation":"prepare_create","path":"preview.txt","content":"must not be written"}`),
	}, nil)
	if edit.Status != ToolResultSuccess { t.Fatalf("edit preview failed: %s", edit.Error) }
	if _, err := os.Stat(filepath.Join(root, "preview.txt")); !os.IsNotExist(err) {
		t.Fatalf("dry-run unexpectedly created a file; stat error=%v", err)
	}

	verifyResult := registry.Execute(context.Background(), ToolCall{
		ID: "preview-verify", Name: "verify.run",
		Arguments: json.RawMessage(`{"checks":[{"name":"Timeout test","timeout_seconds":60,"run":false} ]}`),
	}, nil)
	if verifyResult.Status != ToolResultSuccess { t.Fatalf("verification preview failed: %s", verifyResult.Error) }
	var verifyPayload map[string]any
	if err := json.Unmarshal(verifyResult.Result, &verifyPayload); err != nil { t.Fatal(err) }
	if verifyPayload["executed"] != false { t.Fatalf("verification dry-run claims execution: %s", verifyResult.Result) }

	syncResult := registry.Execute(context.Background(), ToolCall{
		ID: "preview-sync", Name: "github.sync",
		Arguments: json.RawMessage(`{"operation":"prepare_pull","base_branch":"main","remote":"origin","sync":false}`),
	}, nil)
	if syncResult.Status != ToolResultSuccess { t.Fatalf("sync preview should not need GitHub auth: %s", syncResult.Error) }
}


func TestTier1ReadOnlyWorkspaceTools(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(root, "src", "auth.go"), []byte("package src\n\nfunc authenticateUser() bool { return true }\n"), 0644); err != nil { t.Fatal(err) }
	registry, err := NewFuzeCLIToolRegistry(ToolRuntime{Root: root})
	if err != nil { t.Fatal(err) }
	calls := []ToolCall{
		{ID:"read-many", Name:"workspace.read_files", Arguments:json.RawMessage(`{"files":[{"path":"src/auth.go"}]}`)},
		{ID:"references", Name:"workspace.find_references", Arguments:json.RawMessage(`{"symbol":"authenticateUser","max_results":10}`)},
		{ID:"file-info", Name:"workspace.file_info", Arguments:json.RawMessage(`{"path":"src/auth.go"}`)},
		{ID:"tree", Name:"workspace.tree", Arguments:json.RawMessage(`{"path":".","max_depth":4,"max_entries":100}`)},
		{ID:"debug", Name:"debug.analyze", Arguments:json.RawMessage(`{"error":"Connection refused","max_suggestions":5}`)},
		{ID:"test-list", Name:"test.list", Arguments:json.RawMessage(`{"max_results":20}`)},
		{ID:"patch-preview", Name:"workspace.apply_patch", Arguments:json.RawMessage(`{"path":"src/auth.go","old_text":"return true","new_text":"return false","dry_run":true}`)},
		{ID:"rename-preview", Name:"workspace.rename", Arguments:json.RawMessage(`{"source":"src/auth.go","destination":"src/auth2.go","dry_run":true}`)},
	}
	for _, call := range calls {
		t.Run(call.Name, func(t *testing.T) {
			result := registry.Execute(context.Background(), call, nil)
			if result.Status != ToolResultSuccess { t.Fatalf("tool failed: %s", result.Error) }
		})
	}
	data, err := os.ReadFile(filepath.Join(root, "src", "auth.go"))
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(data), "return true") { t.Fatal("dry-run changed the source file") }
}

func TestTier1ReadOnlyDatabaseQuery(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "app.sqlite")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath))
	if err != nil { t.Fatal(err) }
	if _, err = db.Exec("CREATE TABLE users(id INTEGER PRIMARY KEY, name TEXT); INSERT INTO users(name) VALUES('Ada')"); err != nil { t.Fatal(err) }
	if err = db.Close(); err != nil { t.Fatal(err) }
	registry, err := NewFuzeCLIToolRegistry(ToolRuntime{Root: root})
	if err != nil { t.Fatal(err) }
	result := registry.Execute(context.Background(), ToolCall{ID:"query",Name:"database.query",Arguments:json.RawMessage(`{"path":"app.sqlite","query":"SELECT id, name FROM users","max_rows":10}`)}, nil)
	if result.Status != ToolResultSuccess { t.Fatalf("read-only query failed: %s", result.Error) }
	blocked := registry.Execute(context.Background(), ToolCall{ID:"write",Name:"database.query",Arguments:json.RawMessage(`{"path":"app.sqlite","query":"DELETE FROM users"}`)}, nil)
	if blocked.Status != ToolResultError { t.Fatal("write query was not rejected") }
	var got map[string]any
	if err := json.Unmarshal(result.Result, &got); err != nil { t.Fatal(err) }
	if got["row_count"] != float64(1) { t.Fatalf("unexpected result: %s", result.Result) }
}
