package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/security"
)

func TestTier2ToolDefinitionsAreStrictAndRegistered(t *testing.T) {
	defs := FuzeCLIToolDefinitions()
	if len(defs) != 69 { t.Fatalf("want 69 total tools after Tier 3, got %d", len(defs)) }
	want := []string{"package.inspect","package.install","package.update","package.audit","docs.search","docs.generate","build.inspect","build.run","api.inspect","database.migrate","container.inspect","container.run"}
	seen := map[string]bool{}
	for _, d := range defs {
		seen[d.Name] = true
		if !json.Valid(d.InputSchema) { t.Errorf("%s has invalid JSON schema", d.Name) }
		var schema map[string]any
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil { t.Fatal(err) }
		if schema["type"] != "object" || schema["additionalProperties"] != false { t.Errorf("%s schema is not strict", d.Name) }
	}
	for _, name := range want { if !seen[name] { t.Errorf("missing Tier 2 tool %s", name) } }
}

func TestTier2ReadOnlyInspectionTools(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root,"docs"),0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(root,"package.json"),[]byte(`{"name":"demo","scripts":{"build":"vite build"},"dependencies":{"vite":"^5.0.0"}}`),0644); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(root,"docs","guide.md"),[]byte("# Guide\nRun the build command before release.\n"),0644); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(root,"openapi.json"),[]byte(`{"openapi":"3.0.0","paths":{"/health":{"get":{"summary":"Health check"}}}}`),0644); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(root,"openapi.yaml"),[]byte("openapi: 3.0.0\npaths:\n  /ready:\n    get:\n      summary: Readiness check\n"),0644); err != nil { t.Fatal(err) }
	registry,err:=NewFuzeCLIToolRegistry(ToolRuntime{Root:root});if err!=nil{t.Fatal(err)}
	calls:=[]ToolCall{
		{ID:"pkg",Name:"package.inspect",Arguments:json.RawMessage(`{"manager":"npm"}`)},
		{ID:"docs",Name:"docs.search",Arguments:json.RawMessage(`{"query":"build"}`)},
		{ID:"build",Name:"build.inspect",Arguments:json.RawMessage(`{}`)},
		{ID:"api",Name:"api.inspect",Arguments:json.RawMessage(`{}`)},
	}
	for _,call:=range calls{got:=registry.Execute(context.Background(),call,nil);if got.Status!=ToolResultSuccess{t.Errorf("%s failed: %s",call.Name,got.Error)};if call.Name=="api.inspect"&&got.Status==ToolResultSuccess&&!strings.Contains(string(got.Result),"/ready"){t.Errorf("OpenAPI YAML route was not inspected: %s",got.Result)}}
}

func TestTier2DocsGenerateRequiresApprovalAndNeverOverwrites(t *testing.T) {
	root:=t.TempDir();if err:=os.MkdirAll(filepath.Join(root,"docs"),0755);err!=nil{t.Fatal(err)}
	registry,err:=NewFuzeCLIToolRegistry(ToolRuntime{Root:root});if err!=nil{t.Fatal(err)}
	call:=ToolCall{ID:"doc",Name:"docs.generate",Arguments:json.RawMessage(`{"path":"docs/new-guide.md","title":"New Guide","content":"Useful content"}`)}
	preview:=registry.Execute(context.Background(),ToolCall{ID:"preview",Name:call.Name,Arguments:json.RawMessage(`{"path":"docs/new-guide.md","title":"New Guide","content":"Useful content","dry_run":true}`)},nil)
	if preview.Status!=ToolResultSuccess{t.Fatalf("preview failed: %s",preview.Error)}
	if _,err:=os.Stat(filepath.Join(root,"docs","new-guide.md"));!os.IsNotExist(err){t.Fatal("dry run created a file")}
	denied:=registry.Execute(context.Background(),call,nil)
	if denied.Status!=ToolResultError||!strings.Contains(denied.Error,"approval"){t.Fatalf("write should require approval: %#v",denied)}
	approve:=func(context.Context,ToolDefinition,json.RawMessage)error{return nil}
	created:=registry.Execute(context.Background(),call,approve);if created.Status!=ToolResultSuccess{t.Fatalf("approved doc creation failed: %s",created.Error)}
	again:=registry.Execute(context.Background(),call,approve);if again.Status!=ToolResultError||!strings.Contains(again.Error,"never overwrites"){t.Fatalf("existing doc should not be overwritten: %#v",again)}
}

func TestTier2DatabaseMigrationIsTransactionalAndApprovalGated(t *testing.T) {
	root:=t.TempDir();dbPath:=filepath.Join(root,"app.sqlite")
	db,err:=sql.Open("sqlite","file:"+filepath.ToSlash(dbPath));if err!=nil{t.Fatal(err)}
	if _,err=db.Exec("CREATE TABLE users(id INTEGER PRIMARY KEY, name TEXT)");err!=nil{t.Fatal(err)}
	if err=db.Close();err!=nil{t.Fatal(err)}
	if err=os.WriteFile(filepath.Join(root,"001_add_role.sql"),[]byte("ALTER TABLE users ADD COLUMN role TEXT;"),0644);err!=nil{t.Fatal(err)}
	registry,err:=NewFuzeCLIToolRegistry(ToolRuntime{Root:root});if err!=nil{t.Fatal(err)}
	raw:=json.RawMessage(`{"database_path":"app.sqlite","migration_path":"001_add_role.sql"}`)
	denied:=registry.Execute(context.Background(),ToolCall{ID:"migration",Name:"database.migrate",Arguments:raw},nil)
	if denied.Status!=ToolResultError||!strings.Contains(denied.Error,"approval"){t.Fatalf("migration should require approval: %#v",denied)}
	approve:=func(context.Context,ToolDefinition,json.RawMessage)error{return nil}
	applied:=registry.Execute(context.Background(),ToolCall{ID:"migration-approved",Name:"database.migrate",Arguments:raw},approve)
	if applied.Status!=ToolResultSuccess{t.Fatalf("approved migration failed: %s",applied.Error)}
	db,err=sql.Open("sqlite","file:"+filepath.ToSlash(dbPath));if err!=nil{t.Fatal(err)};defer db.Close()
	rows,err:=db.Query("PRAGMA table_info(users)");if err!=nil{t.Fatal(err)};defer rows.Close()
	found:=false;for rows.Next(){var cid int;var name,typ string;var notnull,pk int;var def any;if err:=rows.Scan(&cid,&name,&typ,&notnull,&def,&pk);err!=nil{t.Fatal(err)};if name=="role"{found=true}}
	if !found{t.Fatal("migration did not add role column")}
}

func TestTier2InstallAndContainerDryRunsDoNotExecute(t *testing.T) {
	root:=t.TempDir()
	caps:=security.SafeCapabilities();caps[security.InstallDependency]=true
	registry,err:=NewFuzeCLIToolRegistry(ToolRuntime{Root:root,Capabilities:caps});if err!=nil{t.Fatal(err)}
	install:=registry.Execute(context.Background(),ToolCall{ID:"install",Name:"package.install",Arguments:json.RawMessage(`{"manager":"npm","packages":["left-pad"],"dry_run":true}`)},nil)
	if install.Status!=ToolResultSuccess{t.Fatalf("package install dry run failed: %s",install.Error)}
	container:=registry.Execute(context.Background(),ToolCall{ID:"container",Name:"container.run",Arguments:json.RawMessage(`{"engine":"docker","image":"alpine:3.20","command":["echo","safe"],"dry_run":true}`)},nil)
	build:=registry.Execute(context.Background(),ToolCall{ID:"build",Name:"build.run",Arguments:json.RawMessage(`{"target":"go","dry_run":true}`)},nil)
	if build.Status!=ToolResultSuccess{t.Fatalf("build dry run failed: %s",build.Error)}
	if container.Status!=ToolResultSuccess{t.Fatalf("container dry run failed: %s",container.Error)}
}

func TestTier2MutationsRequireRequestScopedApproval(t *testing.T) {
	mutations := []struct{name, args string}{
		{"package.install", `{"manager":"npm","packages":["demo"]}`},
		{"package.update", `{"manager":"npm"}`},
		{"docs.generate", `{"path":"docs/new.md","title":"New","content":"body"}`},
		{"build.run", `{"target":"go"}`},
		{"database.migrate", `{"database_path":"app.sqlite","migration_path":"001.sql"}`},
		{"container.run", `{"image":"alpine:3.20"}`},
	}
	for _, item := range mutations {
		required, _ := RequiresToolApproval(item.name, json.RawMessage(item.args))
		if !required { t.Errorf("%s must require approval", item.name) }
		var args map[string]any
		if err := json.Unmarshal([]byte(item.args), &args); err != nil { t.Fatal(err) }
		args["dry_run"] = true
		raw, err := json.Marshal(args); if err != nil { t.Fatal(err) }
		required, _ = RequiresToolApproval(item.name, raw)
		if required { t.Errorf("%s dry run must not require mutation approval", item.name) }
	}
}
