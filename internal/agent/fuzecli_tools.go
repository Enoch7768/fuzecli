package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// FuzeCLIToolDefinitions is the canonical, strict JSON Schema contract for the
// namespaced agent tools. Legacy MCP tools remain separate and unchanged.
func FuzeCLIToolDefinitions() []ToolDefinition {
	raw := []ToolDefinition{
		{"workspace.list_files", "List workspace-relative files, excluding .git and .aicli.", schema(`{"type":"object","properties":{"prefix":{"type":"string","description":"Optional workspace-relative directory or path prefix."},"limit":{"type":"integer","minimum":1,"maximum":1000,"default":200}},"additionalProperties":false}`)},
		{"workspace.read_file", "Read a UTF-8 text file inside the active workspace.", schema(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"maxLength":1024},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1}},"required":["path"],"additionalProperties":false}`)},
		{"workspace.search_code", "Search project source for text or a pattern with bounded results.", schema(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":500},"paths":{"type":"array","items":{"type":"string"},"maxItems":50},"is_regex":{"type":"boolean","default":false},"case_sensitive":{"type":"boolean","default":false},"max_results":{"type":"integer","minimum":1,"maximum":200,"default":50}},"required":["query"],"additionalProperties":false}`)},
		{"workspace.find_symbols", "Find indexed function, method, type, class, variable, or constant definitions.", schema(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"kind":{"type":"string","enum":["any","function","method","type","class","variable","constant"]},"path":{"type":"string"},"max_results":{"type":"integer","minimum":1,"maximum":100,"default":30}},"required":["query"],"additionalProperties":false}`)},
		{"workspace.apply_edits", "Apply validated file edits through the existing transactional executor; never execute generated commands.", schema(`{"type":"object","properties":{"files":{"type":"array","minItems":1,"maxItems":100,"items":{"type":"object","properties":{"path":{"type":"string","minLength":1},"action":{"type":"string","enum":["create","modify","delete"]},"content":{"type":"string"}},"required":["path","action"],"additionalProperties":false}},"verify":{"type":"boolean","default":true}},"required":["files"],"additionalProperties":false}`)},
		{"project.health", "Read-only inspection of project manifests, runtimes, Git metadata, and likely tests.", schema(`{"type":"object","properties":{"include_runtimes":{"type":"boolean","default":true},"include_git":{"type":"boolean","default":true}},"additionalProperties":false}`)},
		{"terminal.run", "Run an authorized executable with an argument array in a workspace-relative directory.", schema(`{"type":"object","properties":{"executable":{"type":"string","minLength":1,"maxLength":256},"args":{"type":"array","items":{"type":"string"},"maxItems":100},"cwd":{"type":"string","description":"Workspace-relative working directory."},"timeout_seconds":{"type":"integer","minimum":1,"maximum":300,"default":60}},"required":["executable","args"],"additionalProperties":false}`)},
		{"verify.run", "Run selected project checks and return structured diagnostics.", schema(`{"type":"object","properties":{"paths":{"type":"array","items":{"type":"string"},"maxItems":100},"checks":{"type":"array","items":{"type":"string","enum":["format","lint","test","build","typecheck"]},"minItems":1,"uniqueItems":true},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":120}},"required":["checks"],"additionalProperties":false}`)},
		{"git.status", "Inspect local repository branch, changed files, staging, and upstream status.", schema(`{"type":"object","properties":{"include_untracked":{"type":"boolean","default":true}},"additionalProperties":false}`)},
		{"git.diff", "Inspect local changes without modifying the repository.", schema(`{"type":"object","properties":{"path":{"type":"string"},"staged":{"type":"boolean","default":false},"context_lines":{"type":"integer","minimum":0,"maximum":20,"default":3}},"additionalProperties":false}`)},
		{"git.commit", "Commit only explicitly selected changes after checking expected HEAD.", schema(`{"type":"object","properties":{"message":{"type":"string","minLength":1,"maxLength":200},"paths":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":100},"expected_head":{"type":"string","description":"Expected current commit SHA."}},"required":["message","paths","expected_head"],"additionalProperties":false}`)},
		{"github.auth", "Check, start, inspect, or disconnect Studio's GitHub authentication using the supported browser/device flow.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["status","login_start","login_status","disconnect"]}},"required":["action"],"additionalProperties":false}`)},
		{"github.repositories", "List accessible repositories or inspect one repository's metadata.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["list","get"]},"repository":{"type":"string"},"page":{"type":"integer","minimum":1,"default":1},"per_page":{"type":"integer","minimum":1,"maximum":100,"default":30}},"required":["action"],"additionalProperties":false}`)},
		{"github.branches", "List repository branches or create a branch from a known base reference.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["list","create"]},"repository":{"type":"string"},"branch":{"type":"string"},"base":{"type":"string"},"page":{"type":"integer","minimum":1,"default":1}},"required":["action","repository"],"additionalProperties":false}`)},
		{"github.sync", "Pull or push local repository changes without discarding work or force-pushing automatically.", schema(`{"type":"object","properties":{"operation":{"type":"string","enum":["pull","push"]},"remote":{"type":"string","default":"origin"},"branch":{"type":"string"},"expected_head":{"type":"string"}},"required":["operation"],"additionalProperties":false}`)},
		{"github.pull_requests", "List, create, review, comment on, or merge pull requests with action-specific validation.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["list","create","review","comment","merge"]},"repository":{"type":"string"},"number":{"type":"integer","minimum":1},"title":{"type":"string","maxLength":256},"body":{"type":"string","maxLength":50000},"head":{"type":"string"},"base":{"type":"string"},"draft":{"type":"boolean","default":false},"event":{"type":"string","enum":["APPROVE","REQUEST_CHANGES","COMMENT"]},"merge_method":{"type":"string","enum":["merge","squash","rebase"]},"expected_head_sha":{"type":"string"},"state":{"type":"string","enum":["open","closed","all"]}},"required":["action","repository"],"additionalProperties":false}`)},
		{"github.issues", "List, create, update, or comment on repository issues.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["list","create","update","comment"]},"repository":{"type":"string"},"number":{"type":"integer","minimum":1},"title":{"type":"string","maxLength":256},"body":{"type":"string","maxLength":50000},"state":{"type":"string","enum":["open","closed","all"]},"labels":{"type":"array","items":{"type":"string"},"maxItems":50},"assignees":{"type":"array","items":{"type":"string"},"maxItems":50}},"required":["action","repository"],"additionalProperties":false}`)},
		{"github.actions", "Inspect workflow runs, jobs, and logs or rerun failed jobs.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["list_runs","list_jobs","job_logs","rerun_failed"]},"repository":{"type":"string"},"run_id":{"type":"integer","minimum":1},"job_id":{"type":"integer","minimum":1},"page":{"type":"integer","minimum":1,"default":1},"per_page":{"type":"integer","minimum":1,"maximum":100,"default":30}},"required":["action","repository"],"additionalProperties":false}`)},
		{"context.get", "Retrieve context available to the current task or project; identify unavailable history honestly.", schema(`{"type":"object","properties":{"scope":{"type":"string","enum":["task","project","recent_history"]},"query":{"type":"string","maxLength":500},"max_items":{"type":"integer","minimum":1,"maximum":50,"default":10}},"required":["scope"],"additionalProperties":false}`)},
		{"context.checkpoint", "Save a bounded task checkpoint without credentials or copied source files.", schema(`{"type":"object","properties":{"task_id":{"type":"string","minLength":1,"maxLength":128},"summary":{"type":"string","minLength":1,"maxLength":4000},"completed":{"type":"array","items":{"type":"string"},"maxItems":50},"pending":{"type":"array","items":{"type":"string"},"maxItems":50},"files_touched":{"type":"array","items":{"type":"string"},"maxItems":100},"verification":{"type":"string","maxLength":2000}},"required":["task_id","summary","completed","pending"],"additionalProperties":false}`)},
	}
	sort.Slice(raw, func(i, j int) bool { return raw[i].Name < raw[j].Name })
	return raw
}

func schema(raw string) json.RawMessage { return json.RawMessage(raw) }

// ValidateToolArguments validates both the canonical JSON Schema subset used
// by these tools and action-specific rules that JSON Schema cannot express here.
func ValidateToolArguments(def ToolDefinition, raw json.RawMessage) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil { return fmt.Errorf("invalid arguments: %w", err) }
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF { return fmt.Errorf("arguments must contain exactly one JSON value") }
	object, ok := value.(map[string]any)
	if !ok { return fmt.Errorf("arguments must be a JSON object") }
	var spec map[string]any
	if err := json.Unmarshal(def.InputSchema, &spec); err != nil { return fmt.Errorf("invalid schema for %s: %w", def.Name, err) }
	if err := validateSchemaValue("$", object, spec); err != nil { return err }
	return validateToolAction(def.Name, object)
}

func validateSchemaValue(path string, value any, spec map[string]any) error {
	typeName, _ := spec["type"].(string)
	switch typeName {
	case "object":
		obj, ok := value.(map[string]any); if !ok { return fmt.Errorf("%s must be an object", path) }
		props, _ := spec["properties"].(map[string]any)
		required, _ := spec["required"].([]any)
		for _, item := range required { key, _ := item.(string); if _, ok := obj[key]; !ok { return fmt.Errorf("%s.%s is required", path, key) } }
		additional, hasAdditional := spec["additionalProperties"]
		for key, v := range obj {
			p, exists := props[key]
			if !exists {
				if hasAdditional && additional == false { return fmt.Errorf("%s.%s is not allowed", path, key) }
				continue
			}
			ps, ok := p.(map[string]any); if ok { if err := validateSchemaValue(path+"."+key, v, ps); err != nil { return err } }
		}
	case "array":
		arr, ok := value.([]any); if !ok { return fmt.Errorf("%s must be an array", path) }
		if err := checkNumber(path, float64(len(arr)), spec, "minItems", "maxItems"); err != nil { return err }
		if spec["uniqueItems"] == true {
			seen := map[string]bool{}
			for _, item := range arr { b, _ := json.Marshal(item); if seen[string(b)] { return fmt.Errorf("%s must contain unique items", path) }; seen[string(b)] = true }
		}
		if itemSpec, ok := spec["items"].(map[string]any); ok { for i, item := range arr { if err := validateSchemaValue(fmt.Sprintf("%s[%d]", path, i), item, itemSpec); err != nil { return err } } }
	case "string":
		s, ok := value.(string); if !ok { return fmt.Errorf("%s must be a string", path) }
		if err := checkNumber(path, float64(len([]rune(s))), spec, "minLength", "maxLength"); err != nil { return err }
	case "integer":
		n, ok := value.(json.Number); if !ok { return fmt.Errorf("%s must be an integer", path) }; f, err := n.Float64(); if err != nil || f != float64(int64(f)) { return fmt.Errorf("%s must be an integer", path) }
		if err := checkNumber(path, f, spec, "minimum", "maximum"); err != nil { return err }
	case "number":
		n, ok := value.(json.Number); if !ok { return fmt.Errorf("%s must be a number", path) }; f, err := n.Float64(); if err != nil { return fmt.Errorf("%s must be a number", path) }
		if err := checkNumber(path, f, spec, "minimum", "maximum"); err != nil { return err }
	case "boolean":
		if _, ok := value.(bool); !ok { return fmt.Errorf("%s must be a boolean", path) }
	}
	if enum, ok := spec["enum"].([]any); ok {
		matched := false
		for _, allowed := range enum { if fmt.Sprint(allowed) == fmt.Sprint(value) { matched = true; break } }
		if !matched { return fmt.Errorf("%s has an unsupported value", path) }
	}
	return nil
}

func checkNumber(path string, value float64, spec map[string]any, minKey, maxKey string) error {
	if min, ok := spec[minKey].(float64); ok && value < min { return fmt.Errorf("%s must be at least %v", path, min) }
	if max, ok := spec[maxKey].(float64); ok && value > max { return fmt.Errorf("%s must be at most %v", path, max) }
	return nil
}

func validateToolAction(name string, args map[string]any) error {
	require := func(keys ...string) error {
		for _, key := range keys { v, ok := args[key]; if !ok || v == nil || (strings.TrimSpace(fmt.Sprint(v)) == "" && v != false) { return fmt.Errorf("%s.%s is required for this action", name, key) } }
		return nil
	}
	switch name {
	case "workspace.read_file":
		start, hasStart := args["start_line"]; end, hasEnd := args["end_line"]
		if hasStart && hasEnd && start.(json.Number).String() != "" && end.(json.Number).String() != "" { a,_:=start.(json.Number).Int64(); b,_:=end.(json.Number).Int64(); if b<a { return fmt.Errorf("%s.end_line must be greater than or equal to start_line", name) } }
	case "workspace.apply_edits":
		items, _ := args["files"].([]any)
		for i, raw := range items { f, _ := raw.(map[string]any); action, _ := f["action"].(string); content, exists := f["content"]; switch action { case "create","modify": if !exists || content == nil { return fmt.Errorf("%s.files[%d].content is required for %s",name,i,action) }; case "delete": if exists { return fmt.Errorf("%s.files[%d].content must be absent for delete",name,i) } } }
	case "github.repositories":
		if args["action"] == "get" { return require("repository") }
	case "github.branches":
		if args["action"] == "create" { return require("branch","base") }
	case "github.pull_requests":
		switch args["action"] { case "create": return require("title","head","base"); case "review": if err:=require("number","event");err!=nil{return err};case "comment": if err:=require("number","body");err!=nil{return err};case "merge": if err:=require("number","merge_method");err!=nil{return err};case "list": if state,ok:=args["state"];ok && state!="open" && state!="closed" && state!="all" { return fmt.Errorf("%s.state is invalid",name) } }
	case "github.issues":
		switch args["action"] { case "create": return require("title","body"); case "update": if err:=require("number");err!=nil{return err};if state,ok:=args["state"];ok&&state!="open"&&state!="closed"{return fmt.Errorf("%s.state must be open or closed for update",name)};if _,ok:=args["title"];!ok { if _,ok=args["body"];!ok { if _,ok=args["state"];!ok { if _,ok=args["labels"];!ok { if _,ok=args["assignees"];!ok { return fmt.Errorf("%s.update needs at least one field to change",name) } } } } };case "comment": return require("number","body") }
	case "github.actions":
		switch args["action"] { case "list_jobs","rerun_failed": return require("run_id"); case "job_logs": return require("job_id") }
	}
	return nil
}
