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
		{"workspace.apply_edits", "Apply validated file edits through the existing transactional executor; never execute generated commands. Set dry_run=true to preview only.", schema(`{"type":"object","properties":{"files":{"type":"array","minItems":1,"maxItems":100,"items":{"type":"object","properties":{"path":{"type":"string","minLength":1},"action":{"type":"string","enum":["create","modify","delete"]},"content":{"type":"string"}},"required":["path","action"],"additionalProperties":false}},"verify":{"type":"boolean","default":true},"dry_run":{"type":"boolean","default":false}},"required":["files"],"additionalProperties":false}`)},
		{"project.health", "Read-only inspection of project manifests, runtimes, Git metadata, and likely tests.", schema(`{"type":"object","properties":{"include_runtimes":{"type":"boolean","default":true},"include_git":{"type":"boolean","default":true}},"additionalProperties":false}`)},
		{"terminal.run", "Run an authorized executable with an argument array in a workspace-relative directory.", schema(`{"type":"object","properties":{"executable":{"type":"string","minLength":1,"maxLength":256},"args":{"type":"array","items":{"type":"string"},"maxItems":100},"cwd":{"type":"string","description":"Workspace-relative working directory."},"timeout_seconds":{"type":"integer","minimum":1,"maximum":300,"default":60}},"required":["executable","args"],"additionalProperties":false}`)},
		{"verify.run", "Run selected project checks and return structured diagnostics. Set dry_run=true to preview without executing checks.", schema(`{"type":"object","properties":{"paths":{"type":"array","items":{"type":"string"},"maxItems":100},"checks":{"type":"array","items":{"type":"string","enum":["format","lint","test","build","typecheck"]},"minItems":1,"uniqueItems":true},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":120},"dry_run":{"type":"boolean","default":false}},"required":["checks"],"additionalProperties":false}`)},
		{"git.status", "Inspect local repository branch, changed files, staging, and upstream status.", schema(`{"type":"object","properties":{"include_untracked":{"type":"boolean","default":true}},"additionalProperties":false}`)},
		{"git.diff", "Inspect local changes without modifying the repository.", schema(`{"type":"object","properties":{"path":{"type":"string"},"staged":{"type":"boolean","default":false},"context_lines":{"type":"integer","minimum":0,"maximum":20,"default":3}},"additionalProperties":false}`)},
		{"git.commit", "Commit only explicitly selected changes after checking expected HEAD. Set dry_run=true to preview without committing.", schema(`{"type":"object","properties":{"message":{"type":"string","minLength":1,"maxLength":200},"paths":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":100},"expected_head":{"type":"string","description":"Expected current commit SHA."},"dry_run":{"type":"boolean","default":false}},"required":["message","expected_head"],"additionalProperties":false}`)},
		{"github.auth", "Check, start, inspect, or disconnect Studio's GitHub authentication using the supported browser/device flow.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["status","login_start","login_status","disconnect"]}},"required":["action"],"additionalProperties":false}`)},
		{"github.repositories", "List accessible repositories or inspect one repository's metadata.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["list","get"]},"repository":{"type":"string"},"page":{"type":"integer","minimum":1,"default":1},"per_page":{"type":"integer","minimum":1,"maximum":100,"default":30}},"required":["action"],"additionalProperties":false}`)},
		{"github.branches", "List repository branches or create a branch from a known base reference.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["list","create"]},"repository":{"type":"string"},"branch":{"type":"string"},"base":{"type":"string"},"page":{"type":"integer","minimum":1,"default":1}},"required":["action","repository"],"additionalProperties":false}`)},
		{"github.sync", "Pull or push local repository changes without discarding work or force-pushing automatically. Set dry_run=true to preview only.", schema(`{"type":"object","properties":{"operation":{"type":"string","enum":["pull","push"]},"remote":{"type":"string","default":"origin"},"branch":{"type":"string"},"expected_head":{"type":"string"},"dry_run":{"type":"boolean","default":false}},"required":["operation"],"additionalProperties":false}`)},
		{"github.pull_requests", "List, create, review, comment on, or merge pull requests with action-specific validation.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["list","create","review","comment","merge"]},"repository":{"type":"string"},"number":{"type":"integer","minimum":1},"title":{"type":"string","maxLength":256},"body":{"type":"string","maxLength":50000},"head":{"type":"string"},"base":{"type":"string"},"draft":{"type":"boolean","default":false},"event":{"type":"string","enum":["APPROVE","REQUEST_CHANGES","COMMENT"]},"merge_method":{"type":"string","enum":["merge","squash","rebase"]},"expected_head_sha":{"type":"string"},"state":{"type":"string","enum":["open","closed","all"]}},"required":["action","repository"],"additionalProperties":false}`)},
		{"github.issues", "List, create, update, or comment on repository issues.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["list","create","update","comment"]},"repository":{"type":"string"},"number":{"type":"integer","minimum":1},"title":{"type":"string","maxLength":256},"body":{"type":"string","maxLength":50000},"state":{"type":"string","enum":["open","closed","all"]},"labels":{"type":"array","items":{"type":"string"},"maxItems":50},"assignees":{"type":"array","items":{"type":"string"},"maxItems":50}},"required":["action","repository"],"additionalProperties":false}`)},
		{"github.actions", "Inspect workflow runs, jobs, and logs or rerun failed jobs.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["list_runs","list_jobs","job_logs","rerun_failed"]},"repository":{"type":"string"},"run_id":{"type":"integer","minimum":1},"job_id":{"type":"integer","minimum":1},"page":{"type":"integer","minimum":1,"default":1},"per_page":{"type":"integer","minimum":1,"maximum":100,"default":30}},"required":["action","repository"],"additionalProperties":false}`)},
		{"context.get", "Retrieve context available to the current task or project; identify unavailable history honestly.", schema(`{"type":"object","properties":{"scope":{"type":"string","enum":["task","project","recent_history"]},"query":{"type":"string","maxLength":500},"max_items":{"type":"integer","minimum":1,"maximum":50,"default":10}},"required":["scope"],"additionalProperties":false}`)},
		{"context.checkpoint", "Save a bounded task checkpoint without credentials or copied source files.", schema(`{"type":"object","properties":{"task_id":{"type":"string","minLength":1,"maxLength":128},"summary":{"type":"string","minLength":1,"maxLength":4000},"completed":{"type":"array","items":{"type":"string"},"maxItems":50},"pending":{"type":"array","items":{"type":"string"},"maxItems":50},"files_touched":{"type":"array","items":{"type":"string"},"maxItems":100},"verification":{"type":"string","maxLength":2000}},"required":["task_id","summary","completed","pending"],"additionalProperties":false}`)},

		{"workspace.read_files", "Read multiple UTF-8 text files in one call with per-file line ranges and bounded output.", schema(`{"type":"object","properties":{"files":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","properties":{"path":{"type":"string","minLength":1,"maxLength":1024},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1}},"required":["path"],"additionalProperties":false}}},"required":["files"],"additionalProperties":false}`)},
		{"workspace.find_references", "Find textual/code references to a symbol across the workspace before refactoring.", schema(`{"type":"object","properties":{"symbol":{"type":"string","minLength":1,"maxLength":256},"paths":{"type":"array","items":{"type":"string"},"maxItems":50},"max_results":{"type":"integer","minimum":1,"maximum":200,"default":50},"case_sensitive":{"type":"boolean","default":true}},"required":["symbol"],"additionalProperties":false}`)},
		{"workspace.apply_patch", "Replace one exact unique text range in a workspace file; preview with dry_run before applying if uncertain.", schema(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"maxLength":1024},"old_text":{"type":"string","minLength":1,"maxLength":100000},"new_text":{"type":"string","maxLength":100000},"dry_run":{"type":"boolean","default":false}},"required":["path","old_text","new_text"],"additionalProperties":false}`)},
		{"workspace.file_info", "Return safe metadata about a workspace file without reading its contents.", schema(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"maxLength":1024}},"required":["path"],"additionalProperties":false}`)},
		{"workspace.tree", "Return a bounded directory tree for the workspace, excluding ignored and protected paths.", schema(`{"type":"object","properties":{"path":{"type":"string","default":"."},"max_depth":{"type":"integer","minimum":1,"maximum":12,"default":5},"max_entries":{"type":"integer","minimum":1,"maximum":2000,"default":500}},"additionalProperties":false}`)},
		{"workspace.rename", "Rename or move a workspace file or directory without overwriting an existing destination.", schema(`{"type":"object","properties":{"source":{"type":"string","minLength":1,"maxLength":1024},"destination":{"type":"string","minLength":1,"maxLength":1024},"dry_run":{"type":"boolean","default":false}},"required":["source","destination"],"additionalProperties":false}`)},
		{"terminal.run_parallel", "Run up to 8 independent authorized commands concurrently with bounded timeouts and output.", schema(`{"type":"object","properties":{"commands":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"object","properties":{"executable":{"type":"string","minLength":1,"maxLength":256},"args":{"type":"array","items":{"type":"string"},"maxItems":100},"cwd":{"type":"string"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":120,"default":60}},"required":["executable","args"],"additionalProperties":false}}},"required":["commands"],"additionalProperties":false}`)},
		{"terminal.process", "Start, inspect, stop, or restart a bounded long-running authorized process.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["start","status","stop","restart"]},"process_id":{"type":"string"},"executable":{"type":"string","maxLength":256},"args":{"type":"array","items":{"type":"string"},"maxItems":100},"cwd":{"type":"string"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":300,"default":60}},"required":["action"],"additionalProperties":false}`)},
		{"terminal.output", "Retrieve recent bounded stdout and stderr captured from a managed process.", schema(`{"type":"object","properties":{"process_id":{"type":"string","minLength":1},"max_bytes":{"type":"integer","minimum":1,"maximum":65536,"default":16384}},"required":["process_id"],"additionalProperties":false}`)},
		{"environment.inspect", "Inspect OS, architecture, runtimes, versions, package managers, and available capabilities.", schema(`{"type":"object","properties":{"check_versions":{"type":"boolean","default":true}},"additionalProperties":false}`)},
		{"test.list", "Discover test files, test commands, and test scripts without executing tests.", schema(`{"type":"object","properties":{"path":{"type":"string"},"max_results":{"type":"integer","minimum":1,"maximum":200,"default":100}},"additionalProperties":false}`)},
		{"test.run", "Run a specific test file, test name, or suite using a detected supported test runner.", schema(`{"type":"object","properties":{"path":{"type":"string"},"name":{"type":"string","maxLength":256},"suite":{"type":"string","enum":["unit","integration","e2e","component","all"]},"timeout_seconds":{"type":"integer","minimum":1,"maximum":300,"default":120},"dry_run":{"type":"boolean","default":false}},"additionalProperties":false}`)},
		{"debug.analyze", "Analyze supplied error text or logs and return probable causes and investigation steps without claiming to fix them.", schema(`{"type":"object","properties":{"error":{"type":"string","minLength":1,"maxLength":30000},"path":{"type":"string"},"max_suggestions":{"type":"integer","minimum":1,"maximum":20,"default":8}},"required":["error"],"additionalProperties":false}`)},
		{"git.log", "Inspect bounded local Git commit history.", schema(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":100,"default":20},"path":{"type":"string"},"author":{"type":"string","maxLength":256}},"additionalProperties":false}`)},
		{"git.show", "Inspect one local Git commit, optionally scoped to a workspace-relative path.", schema(`{"type":"object","properties":{"revision":{"type":"string","minLength":1,"maxLength":128,"default":"HEAD"},"path":{"type":"string"},"stat_only":{"type":"boolean","default":false}},"additionalProperties":false}`)},
		{"git.branch", "Inspect, create, switch, or safely delete local Git branches.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["list","create","switch","delete"]},"name":{"type":"string","maxLength":256},"start_point":{"type":"string","maxLength":256},"dry_run":{"type":"boolean","default":false}},"required":["action"],"additionalProperties":false}`)},
		{"git.stash", "List, save, apply, pop, or drop Git stashes while protecting user changes.", schema(`{"type":"object","properties":{"action":{"type":"string","enum":["list","push","apply","pop","drop"]},"message":{"type":"string","maxLength":256},"stash_ref":{"type":"string","maxLength":64,"default":"stash@{0}"},"include_untracked":{"type":"boolean","default":false},"dry_run":{"type":"boolean","default":false}},"required":["action"],"additionalProperties":false}`)},
		{"web.search", "Search current public web information for technical documentation and errors; requires network capability.", schema(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":1000},"max_results":{"type":"integer","minimum":1,"maximum":10,"default":5}},"required":["query"],"additionalProperties":false}`)},
		{"web.open", "Fetch and extract readable text from an HTTPS page on an approved public host.", schema(`{"type":"object","properties":{"url":{"type":"string","minLength":8,"maxLength":2048},"max_chars":{"type":"integer","minimum":1000,"maximum":30000,"default":12000}},"required":["url"],"additionalProperties":false}`)},
		{"github.search_code", "Search code in accessible GitHub repositories using the authenticated GitHub API.", schema(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":500},"repository":{"type":"string"},"per_page":{"type":"integer","minimum":1,"maximum":100,"default":20}},"required":["query"],"additionalProperties":false}`)},
		{"github.read_file", "Read a UTF-8 text file from an accessible GitHub repository and reference.", schema(`{"type":"object","properties":{"repository":{"type":"string","minLength":3},"path":{"type":"string","minLength":1,"maxLength":1024},"ref":{"type":"string","maxLength":256},"max_bytes":{"type":"integer","minimum":1,"maximum":200000,"default":100000}},"required":["repository","path"],"additionalProperties":false}`)},
		{"http.request", "Make a bounded HTTP request to localhost or a host explicitly allowlisted in FUZECLI_HTTP_ALLOWED_HOSTS; remote access requires network capability.", schema(`{"type":"object","properties":{"method":{"type":"string","enum":["GET","POST","PUT","PATCH","DELETE","HEAD"]},"url":{"type":"string","minLength":8,"maxLength":2048},"headers":{"type":"object"},"body":{"type":"string","maxLength":65536},"timeout_seconds":{"type":"integer","minimum":1,"maximum":30,"default":10}},"required":["method","url"],"additionalProperties":false}`)},
		{"database.inspect", "Discover workspace database files and database configuration keys without exposing credentials.", schema(`{"type":"object","properties":{"max_results":{"type":"integer","minimum":1,"maximum":100,"default":30}},"additionalProperties":false}`)},
		{"database.schema", "Inspect tables, columns, keys, indexes, and foreign keys in a workspace SQLite database.", schema(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"maxLength":1024}},"required":["path"],"additionalProperties":false}`)},
		{"database.query", "Run a bounded read-only SELECT, EXPLAIN, or PRAGMA query against a workspace SQLite database.", schema(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"maxLength":1024},"query":{"type":"string","minLength":1,"maxLength":10000},"max_rows":{"type":"integer","minimum":1,"maximum":200,"default":50}},"required":["path","query"],"additionalProperties":false}`)},


		{"package.inspect", "Inspect declared project dependencies and package manager manifests without changing files.", schema(`{"type":"object","properties":{"manager":{"type":"string","enum":["auto","go","npm","pnpm","yarn","bun","python","php"],"default":"auto"},"max_items":{"type":"integer","minimum":1,"maximum":300,"default":100}},"additionalProperties":false}`)},
		{"package.install", "Install explicitly named dependencies using a detected supported package manager; requires approval plus run_build and install_dependency capabilities.", schema(`{"type":"object","properties":{"manager":{"type":"string","enum":["go","npm","pnpm","yarn","bun","python","php"]},"packages":{"type":"array","items":{"type":"string","minLength":1,"maxLength":200},"minItems":1,"maxItems":30},"dev":{"type":"boolean","default":false},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":180},"dry_run":{"type":"boolean","default":false}},"required":["manager","packages"],"additionalProperties":false}`)},
		{"package.update", "Update dependencies using a supported package manager; requires approval plus run_build and install_dependency capabilities.", schema(`{"type":"object","properties":{"manager":{"type":"string","enum":["go","npm","pnpm","yarn","bun","python","php"]},"packages":{"type":"array","items":{"type":"string","minLength":1,"maxLength":200},"maxItems":30},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":180},"dry_run":{"type":"boolean","default":false}},"required":["manager"],"additionalProperties":false}`)},
		{"package.audit", "Run a read-only dependency vulnerability audit when a supported manifest and package manager are available.", schema(`{"type":"object","properties":{"manager":{"type":"string","enum":["auto","go","npm","pnpm","yarn","python","php"],"default":"auto"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":300,"default":120}},"additionalProperties":false}`)},
		{"docs.search", "Search project documentation and README files for relevant text with bounded results.", schema(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":500},"paths":{"type":"array","items":{"type":"string"},"maxItems":30},"max_results":{"type":"integer","minimum":1,"maximum":100,"default":30}},"required":["query"],"additionalProperties":false}`)},
		{"docs.generate", "Create a new Markdown documentation file inside docs/; never overwrites existing files and requires approval.", schema(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"maxLength":512},"title":{"type":"string","minLength":1,"maxLength":200},"content":{"type":"string","minLength":1,"maxLength":50000},"dry_run":{"type":"boolean","default":false}},"required":["path","title","content"],"additionalProperties":false}`)},
		{"build.inspect", "Inspect build manifests, scripts, and detected build tools without executing them.", schema(`{"type":"object","properties":{"max_items":{"type":"integer","minimum":1,"maximum":100,"default":50}},"additionalProperties":false}`)},
		{"build.run", "Run a supported project build command with a bounded timeout; requires approval before execution.", schema(`{"type":"object","properties":{"target":{"type":"string","enum":["auto","go","npm","pnpm","yarn","bun","python","php"] ,"default":"auto"},"args":{"type":"array","items":{"type":"string","maxLength":500},"maxItems":20},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"default":180},"dry_run":{"type":"boolean","default":false}},"additionalProperties":false}`)},
		{"api.inspect", "Inspect OpenAPI JSON/YAML files and common source-code route declarations without starting a server.", schema(`{"type":"object","properties":{"path":{"type":"string","maxLength":1024},"max_results":{"type":"integer","minimum":1,"maximum":300,"default":100}},"additionalProperties":false}`)},
		{"database.migrate", "Apply a workspace SQL migration file to a workspace SQLite database in a transaction; requires explicit approval.", schema(`{"type":"object","properties":{"database_path":{"type":"string","minLength":1,"maxLength":1024},"migration_path":{"type":"string","minLength":1,"maxLength":1024},"dry_run":{"type":"boolean","default":false},"timeout_seconds":{"type":"integer","minimum":1,"maximum":120,"default":30}},"required":["database_path","migration_path"],"additionalProperties":false}`)},
		{"container.inspect", "Inspect available Docker/Podman tooling and list containers/images without modifying them.", schema(`{"type":"object","properties":{"engine":{"type":"string","enum":["auto","docker","podman"],"default":"auto"},"target":{"type":"string","enum":["version","containers","images"],"default":"version"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":60,"default":15}},"additionalProperties":false}`)},
		{"container.run", "Run an image in a disposable container with no host mounts, privileged mode, host networking, or daemon socket; requires approval.", schema(`{"type":"object","properties":{"engine":{"type":"string","enum":["docker","podman"],"default":"docker"},"image":{"type":"string","minLength":1,"maxLength":255},"command":{"type":"array","items":{"type":"string","maxLength":1000},"maxItems":30},"ports":{"type":"array","items":{"type":"string","pattern":"^[0-9]{1,5}:[0-9]{1,5}$"},"maxItems":10},"timeout_seconds":{"type":"integer","minimum":1,"maximum":300,"default":60},"dry_run":{"type":"boolean","default":false}},"required":["image"],"additionalProperties":false}`)},
		{"image.inspect", "Inspect a workspace image file's format, dimensions, color model, and size without modifying it.", schema(`{"type":"object","properties":{"path":{"type":"string","minLength":1,"maxLength":1024}},"required":["path"],"additionalProperties":false}`)},
		{"image.compare", "Compare two workspace images using bounded pixel-level metrics and report dimension differences; this is not semantic visual recognition.", schema(`{"type":"object","properties":{"path_a":{"type":"string","minLength":1,"maxLength":1024},"path_b":{"type":"string","minLength":1,"maxLength":1024},"max_pixels":{"type":"integer","minimum":1000,"maximum":1000000,"default":250000}},"required":["path_a","path_b"],"additionalProperties":false}`)},
		{"browser.open", "Open an HTTP(S) URL in a local Chrome/Chromium instance configured with FUZECLI_BROWSER_CDP_URL.", schema(`{"type":"object","properties":{"url":{"type":"string","minLength":8,"maxLength":2048},"wait_ms":{"type":"integer","minimum":0,"maximum":15000,"default":500}},"required":["url"],"additionalProperties":false}`)},
		{"browser.click", "Click a CSS-selected element in a controlled local Chrome/Chromium target; may trigger external actions and requires approval.", schema(`{"type":"object","properties":{"target_id":{"type":"string","minLength":1,"maxLength":128},"selector":{"type":"string","minLength":1,"maxLength":1000}},"required":["target_id","selector"],"additionalProperties":false}`)},
		{"browser.type", "Type text into a focused CSS-selected input or editable element in Chrome/Chromium; requires approval.", schema(`{"type":"object","properties":{"target_id":{"type":"string","minLength":1,"maxLength":128},"selector":{"type":"string","minLength":1,"maxLength":1000},"text":{"type":"string","maxLength":20000}},"required":["target_id","selector","text"],"additionalProperties":false}`)},
		{"browser.select", "Select an option by value in a CSS-selected HTML select element; requires approval.", schema(`{"type":"object","properties":{"target_id":{"type":"string","minLength":1,"maxLength":128},"selector":{"type":"string","minLength":1,"maxLength":1000},"value":{"type":"string","maxLength":2000}},"required":["target_id","selector","value"],"additionalProperties":false}`)},
		{"browser.screenshot", "Capture a PNG or JPEG screenshot from Chrome/Chromium; optionally save it to a new workspace file with approval.", schema(`{"type":"object","properties":{"target_id":{"type":"string","minLength":1,"maxLength":128},"path":{"type":"string","maxLength":1024},"format":{"type":"string","enum":["png","jpeg"],"default":"png"},"quality":{"type":"integer","minimum":30,"maximum":90,"default":70}},"required":["target_id"],"additionalProperties":false}`)},
		{"browser.inspect", "Inspect the current page title, URL, ready state, visible text, and optionally a CSS-selected element.", schema(`{"type":"object","properties":{"target_id":{"type":"string","minLength":1,"maxLength":128},"selector":{"type":"string","maxLength":1000},"max_chars":{"type":"integer","minimum":100,"maximum":20000,"default":6000}},"required":["target_id"],"additionalProperties":false}`)},
		{"browser.console", "Read the bounded console log buffer instrumented by FuzeCLI in a Chrome/Chromium page.", schema(`{"type":"object","properties":{"target_id":{"type":"string","minLength":1,"maxLength":128},"clear":{"type":"boolean","default":false},"max_entries":{"type":"integer","minimum":1,"maximum":200,"default":50}},"required":["target_id"],"additionalProperties":false}`)},
		{"browser.network", "Inspect bounded browser resource timing entries for requests made by the current page; response bodies and full DevTools network events are not captured.", schema(`{"type":"object","properties":{"target_id":{"type":"string","minLength":1,"maxLength":128},"max_entries":{"type":"integer","minimum":1,"maximum":200,"default":50}},"required":["target_id"],"additionalProperties":false}`)},
		{"deploy.inspect", "Inspect deployment manifests and locally available provider CLIs without deploying or exposing credentials.", schema(`{"type":"object","properties":{"provider":{"type":"string","enum":["auto","vercel","netlify","fly","railway"]}},"additionalProperties":false}`)},
		{"deploy.run", "Run a supported deployment provider's fixed deploy command; requires explicit approval, build capability, and network capability.", schema(`{"type":"object","properties":{"provider":{"type":"string","enum":["vercel","netlify","fly","railway"]},"production":{"type":"boolean","default":false},"directory":{"type":"string","maxLength":512},"timeout_seconds":{"type":"integer","minimum":10,"maximum":900,"default":300},"dry_run":{"type":"boolean","default":false}},"required":["provider"],"additionalProperties":false}`)},

	}
	sort.Slice(raw, func(i, j int) bool { return raw[i].Name < raw[j].Name })
	return raw
}

func schema(raw string) json.RawMessage { return json.RawMessage(raw) }

func normalizeToolArguments(name string, raw json.RawMessage) (json.RawMessage, error) {
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil || args == nil {
		return nil, fmt.Errorf("arguments must be a JSON object")
	}
	move := func(from, to string) {
		if value, exists := args[from]; exists {
			if _, canonicalExists := args[to]; !canonicalExists {
				args[to] = value
			}
			delete(args, from)
		}
	}
	switch name {
	case "workspace.search_code":
		move("limit", "max_results")
	case "workspace.apply_edits":
		if _, ok := args["files"]; !ok {
			op, _ := args["operation"].(string)
			if strings.HasPrefix(op, "prepare_") {
				args["dry_run"] = true
				op = strings.TrimPrefix(op, "prepare_")
			}
			if op == "create" || op == "modify" || op == "delete" {
				file := map[string]any{}
				if v, ok := args["path"]; ok { file["path"] = v }
				file["action"] = op
				if v, ok := args["content"]; ok { file["content"] = v }
				args["files"] = []any{file}
				delete(args, "path")
				delete(args, "content")
				delete(args, "operation")
			}
		}
	case "project.health":
		if checks, ok := args["checks"].([]any); ok {
			for _, rawCheck := range checks {
				switch strings.ToLower(fmt.Sprint(rawCheck)) {
				case "runtime", "runtimes", "include_runtimes": args["include_runtimes"] = true
				case "git", "git_status", "include_git": args["include_git"] = true
				}
			}
			delete(args, "checks")
		}
	case "verify.run":
		if checks, ok := args["checks"].([]any); ok {
			normalized := make([]any, 0, len(checks))
			for _, item := range checks {
				if obj, ok := item.(map[string]any); ok {
					nameText := strings.ToLower(fmt.Sprint(obj["name"], " ", obj["check"], " ", obj["type"]))
					found := ""
					for _, candidate := range []string{"format", "lint", "test", "build", "typecheck"} {
						if strings.Contains(nameText, candidate) { found = candidate; break }
					}
					if found == "" { return nil, fmt.Errorf("cannot map verification object to a supported check") }
					normalized = append(normalized, found)
					if run, exists := obj["run"].(bool); exists && !run { args["dry_run"] = true }
					if timeout, exists := obj["timeout_seconds"]; exists {
						if _, already := args["timeout_seconds"]; !already { args["timeout_seconds"] = timeout }
					}
				} else {
					normalized = append(normalized, item)
				}
			}
			args["checks"] = normalized
		}
	case "git.status":
		move("untracked_files", "include_untracked")
	case "git.commit":
		if commit, ok := args["commit"].(bool); ok {
			if !commit { args["dry_run"] = true }
			delete(args, "commit")
		}
	case "github.repositories":
		if _, ok := args["action"]; !ok { args["action"] = "list" }
	case "github.branches":
		if _, ok := args["action"]; !ok { args["action"] = "list" }
	case "github.sync":
		if op, ok := args["operation"].(string); ok && strings.HasPrefix(op, "prepare_") {
			args["operation"] = strings.TrimPrefix(op, "prepare_")
			args["dry_run"] = true
		}
		move("base_branch", "branch")
		if syncValue, ok := args["sync"].(bool); ok {
			if !syncValue { args["dry_run"] = true }
			delete(args, "sync")
		}
	case "github.pull_requests", "github.issues":
		if _, ok := args["action"]; !ok { args["action"] = "list" }
	case "context.get":
		if _, ok := args["scope"]; !ok {
			key, _ := args["key"].(string)
			if key == "recent" || key == "recent_history" || key == "" {
				args["scope"] = "recent_history"
			} else if key == "task" || key == "project" {
				args["scope"] = key
			}
		}
		delete(args, "key")
		move("limit", "max_items")
	}
	encoded, err := json.Marshal(args)
	if err != nil { return nil, fmt.Errorf("normalize arguments: %w", err) }
	return encoded, nil
}

// ValidateToolArguments validates both the canonical JSON Schema subset used
// by these tools and action-specific rules that JSON Schema cannot express here.
func ValidateToolArguments(def ToolDefinition, raw json.RawMessage) error {
	normalized, err := normalizeToolArguments(def.Name, raw)
	if err != nil { return err }
	raw = normalized
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

	case "workspace.read_files":
		items, _ := args["files"].([]any)
		for i, raw := range items { f, _ := raw.(map[string]any); start, hasStart := f["start_line"]; end, hasEnd := f["end_line"]; if hasStart && hasEnd { a, _ := start.(json.Number).Int64(); b, _ := end.(json.Number).Int64(); if b < a { return fmt.Errorf("%s.files[%d].end_line must be greater than or equal to start_line", name, i) } } }
	case "workspace.apply_patch":
		if strings.TrimSpace(fmt.Sprint(args["old_text"])) == "" { return fmt.Errorf("%s.old_text must not be empty", name) }
	case "terminal.process":
		switch args["action"] { case "start": return require("executable","args"); case "status","stop","restart": return require("process_id") }
	case "test.run":
		if _,ok:=args["path"];!ok { if _,ok:=args["name"];!ok { if _,ok:=args["suite"];!ok { return fmt.Errorf("%s needs at least one of path, name, or suite",name) } } }
	case "git.branch":
		switch args["action"] { case "create": return require("name"); case "switch","delete": return require("name") }
	case "git.stash":
		if args["action"]=="push" { return nil }; if args["action"]=="list" { return nil }
	case "github.search_code":
		if repo,ok:=args["repository"];ok && !validRepositoryIdentifier(fmt.Sprint(repo)) { return fmt.Errorf("%s.repository must be owner/name",name) }
	case "github.read_file":
		if !validRepositoryIdentifier(fmt.Sprint(args["repository"])) { return fmt.Errorf("%s.repository must be owner/name",name) }
	case "http.request":
		if method:=fmt.Sprint(args["method"]); method=="GET"||method=="HEAD" { if _,ok:=args["body"];ok { return fmt.Errorf("%s.body is not allowed for %s",name,method) } }
	case "database.query":
		if strings.TrimSpace(fmt.Sprint(args["query"]))=="" { return fmt.Errorf("%s.query must not be empty",name) }
	case "git.commit":
		if !boolArg(args, "dry_run", false) { return require("paths") }
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
	case "image.inspect":
		return require("path")
	case "image.compare":
		return require("path_a","path_b")
	case "browser.open":
		if err := require("url"); err != nil { return err }; if _, err := tier3BrowserURL(fmt.Sprint(args["url"])); err != nil { return err }
	case "browser.click":
		if strings.TrimSpace(fmt.Sprint(args["selector"])) == "" { return fmt.Errorf("%s.selector must not be empty", name) }
	case "browser.type":
		if strings.TrimSpace(fmt.Sprint(args["selector"])) == "" { return fmt.Errorf("%s.selector must not be empty", name) }
	case "browser.select":
		if strings.TrimSpace(fmt.Sprint(args["selector"])) == "" { return fmt.Errorf("%s.selector must not be empty", name) }
	case "browser.screenshot", "browser.inspect", "browser.console", "browser.network":
		return require("target_id")
	case "deploy.run":
		return require("provider")
	}
	return nil
}
