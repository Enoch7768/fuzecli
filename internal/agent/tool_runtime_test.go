package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFuzeCLIToolRegistryWiresAllDefinitions(t *testing.T) {
	root:=t.TempDir()
	if err:=os.WriteFile(filepath.Join(root,"hello.txt"),[]byte("hello FuzeCLI\nsecond line\n"),0600);err!=nil{t.Fatal(err)}
	registry,err:=NewFuzeCLIToolRegistry(ToolRuntime{Root:root})
	if err!=nil{t.Fatal(err)}
	if got:=len(registry.Definitions());got!=20{t.Fatalf("registered %d tools, want 20",got)}
	read:=registry.Execute(context.Background(),ToolCall{ID:"read-1",Name:"workspace.read_file",Arguments:json.RawMessage(`{"path":"hello.txt","start_line":2}`)},nil)
	if read.Status!=ToolResultSuccess{t.Fatalf("read failed: %s",read.Error)}
	if !strings.Contains(string(read.Result), "second line"){t.Fatalf("read result missing requested line: %s",read.Result)}
	list:=registry.Execute(context.Background(),ToolCall{ID:"list-1",Name:"workspace.list_files",Arguments:json.RawMessage(`{}`)},nil)
	if list.Status!=ToolResultSuccess||!strings.Contains(string(list.Result),"hello.txt"){t.Fatalf("list failed: %#v",list)}
}

func TestFuzeCLIToolRegistryRequiresApprovalForMutations(t *testing.T) {
	root:=t.TempDir()
	registry,err:=NewFuzeCLIToolRegistry(ToolRuntime{Root:root})
	if err!=nil{t.Fatal(err)}
	call:=ToolCall{ID:"edit-1",Name:"workspace.apply_edits",Arguments:json.RawMessage(`{"files":[{"path":"new.txt","action":"create","content":"safe"}],"verify":false}`)}
	denied:=registry.Execute(context.Background(),call,nil)
	if denied.Status!=ToolResultError||!strings.Contains(denied.Error,"approval"){t.Fatalf("mutation without approval was not denied: %#v",denied)}
	allowed:=registry.Execute(context.Background(),call,func(context.Context,ToolDefinition,json.RawMessage)error{return nil})
	if allowed.Status!=ToolResultSuccess{t.Fatalf("approved edit failed: %s",allowed.Error)}
	data,err:=os.ReadFile(filepath.Join(root,"new.txt"));if err!=nil{t.Fatal(err)}
	if string(data)!="safe"{t.Fatalf("unexpected written content %q",data)}
}

func TestFuzeCLIToolRegistryRejectsTraversalAndInternalPaths(t *testing.T) {
	root:=t.TempDir()
	registry,err:=NewFuzeCLIToolRegistry(ToolRuntime{Root:root});if err!=nil{t.Fatal(err)}
	for _,path:=range []string{"../outside.txt",".aicli/session.db",".git/config"} {
		call:=ToolCall{ID:"read-bad",Name:"workspace.read_file",Arguments:json.RawMessage(`{"path":`+jsonString(path)+`}`)}
		got:=registry.Execute(context.Background(),call,nil)
		if got.Status!=ToolResultError{t.Errorf("protected path %q was not rejected: %#v",path,got)}
	}
}
