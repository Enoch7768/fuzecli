# FuzeCLI Tier 1 toolset

Tier 1 extends the original 20 namespaced tools with 25 additional tools for workspace intelligence, execution, testing, debugging, Git history, web/GitHub research, HTTP checks, and read-only SQLite inspection. The registry now contains 45 namespaced tools.

The canonical names and strict JSON Schemas live in `internal/agent/fuzecli_tools.go`. Dispatch and runtime behavior live in `internal/agent/tool_runtime.go` and `internal/agent/tier1_runtime.go`. The runtime validates schema and action-specific constraints before executing a handler.

## New tools

### Workspace and code intelligence

- `workspace.read_files`: read up to 20 UTF-8 files in one request, with optional line ranges and a combined output cap.
- `workspace.find_references`: find textual references to a symbol; results may include comments and declarations, so inspect them before refactoring.
- `workspace.apply_patch`: replace one unique exact text block. Ambiguous matches are rejected. Mutations require explicit approval; `dry_run` previews only.
- `workspace.file_info`: inspect file metadata, language, approximate encoding/binary status, size, line count, permissions, and modification time.
- `workspace.tree`: return a bounded directory tree while respecting workspace ignore rules and excluding `.git` / `.aicli`.
- `workspace.rename`: move/rename workspace paths without intentionally overwriting an existing destination. Mutations require explicit approval.

### Terminal and environment

- `terminal.run_parallel`: execute up to 8 authorized commands concurrently, with per-command timeout and output bounds. Explicit approval is required.
- `terminal.process`: manage a process started by the current FuzeCLI process (`start`, `status`, `stop`, `restart`). Process IDs are session-local; this is not a cross-restart process database.
- `terminal.output`: retrieve bounded captured stdout/stderr for a managed process.
- `environment.inspect`: report OS, architecture, CPU count, runtime versions, discovered tools, workspace root, and actual granted capabilities.

### Testing and debugging

- `test.list`: discover common Go, JavaScript/TypeScript, and Python test files and likely commands without executing them.
- `test.run`: run a test path/name/suite with bounded timeout; execution requires explicit approval. Runner behavior depends on detected project manifests and installed tools.
- `debug.analyze`: map common error signatures to likely causes and investigation steps. It is heuristic and never claims to repair the issue.

### Git

- `git.log`: inspect recent commit history, optionally filtered by author or path.
- `git.show`: inspect a validated commit/ref, with patch or stat-only output.
- `git.branch`: list, create, switch, or safely delete local branches. Mutations require approval; switching refuses a dirty working tree and deletion uses Git's safe `-d` behavior.
- `git.stash`: list, push, apply, pop, or drop stashes. Mutations require approval; stash references are constrained to `stash@{N}`.

### Web and GitHub research

- `web.search`: search DuckDuckGo's public HTML results. Network access must be granted in FuzeCLI capabilities.
- `web.open`: fetch and extract text from HTTPS pages on explicitly allowlisted hosts.
- `github.search_code`: search code using the connected GitHub authentication.
- `github.read_file`: read UTF-8 text from a file in an accessible repository/ref.

### HTTP and database

- `http.request`: make bounded HTTP requests. Localhost is supported for development testing. Remote hosts require the network capability and an exact hostname in `FUZECLI_HTTP_ALLOWED_HOSTS`. Remote requests must use HTTPS; redirects are rechecked; response size is capped and sensitive headers/credential-like body fields are redacted.
- `database.inspect`: discover SQLite files and report which common database configuration variable names are present, without revealing their values.
- `database.schema`: inspect SQLite tables, views, indexes, triggers, and table columns.
- `database.query`: execute a bounded read-only query against a workspace SQLite file. Only SELECT/WITH/EXPLAIN and an allowlist of read-only PRAGMAs are accepted. The database is opened read-only, the query is bounded by time and row count, and writes are rejected.

## Security defaults

- File paths are resolved relative to the active workspace and pass the existing workspace/ignore policy.
- Mutating operations go through FuzeCLI's request-scoped approval flow. A model-provided argument is not itself approval.
- Shell strings are not accepted in place of executable/argument arrays.
- Network access is not part of the default safe capability set. Grant `network_access` explicitly for web research and remote HTTP requests.
- For remote HTTP testing, set `FUZECLI_HTTP_ALLOWED_HOSTS` to comma-separated exact hostnames. For `web.open`, set `FUZECLI_WEB_ALLOWED_HOSTS` similarly. Do not include secrets in these values.
- Database support in this tier is intentionally SQLite-only. Other database engines are identified only through configuration-key presence; no credentials are read or emitted.
- All responses must be based on tool results. A preview is not an applied change, and a command exit code of zero is not a substitute for application-level verification.

## Validation

The tests in `internal/agent/fuzecli_tools_test.go` cover registry schema validity, the expected 45 registered tools, workspace read/preview paths, SQLite read-only querying/schema inspection, and localhost HTTP response redaction. `internal/agent/tool_runtime_test.go` also verifies that every definition is registered and that existing mutation approval behavior remains enforced.

Run locally before release:

```sh
go test ./...
go vet ./...
go build ./cmd/aicli
```

Remote research and GitHub operations still require their respective capabilities/authentication and are not exercised against live external services by unit tests.
