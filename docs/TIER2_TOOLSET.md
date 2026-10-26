# FuzeCLI Tier 2 toolset

Tier 2 adds 12 namespaced tools to the 45-tool Tier 1 registry, for **57 total tools**. The canonical strict JSON Schemas live in `internal/agent/fuzecli_tools.go`; dispatch is in `internal/agent/tool_runtime.go`; Tier 2 implementations are in `internal/agent/tier2_runtime.go`.

## Dependency management

- `package.inspect` detects common Go, JavaScript/TypeScript, Python, and PHP manifests. It returns bounded metadata and declared dependency lists where supported.
- `package.install` installs explicitly named dependencies with a supported package manager.
- `package.update` updates named dependencies or the manager's declared dependencies when no package list is supplied.
- `package.audit` runs supported package-manager audits. Python uses `pip-audit` only if already installed; the tool does not install an auditor automatically. Live audits require `network_access`.

Dependency changes require `run_build`, `install_dependency`, and `network_access` capabilities plus explicit approval. The network capability is checked only for real operations; dry runs remain offline. `install_dependency` is intentionally not enabled by the default safe capability set. Because package managers may run install scripts, treat approval as a code-execution decision. Package names are validated and native executables receive argument arrays directly. On Windows, `.cmd`/`.bat` shims use a constrained `cmd.exe` wrapper that rejects shell metacharacters and quotes before invocation. `dry_run` previews the exact command without execution.

## Documentation

- `docs.search` searches Markdown, MDX, reStructuredText, and text documentation with bounded results.
- `docs.generate` creates a new Markdown file under `docs/`. It never overwrites an existing file. File creation requires explicit approval; dry-run mode creates nothing.

## Build and API inspection

- `build.inspect` detects common build manifests and available build tools without executing build scripts.
- `build.run` runs only commands permitted by the existing security.Authorize allowlist, uses a timeout and bounded output, and requires explicit approval. Project-defined build scripts are code and may have side effects; approve them only when expected.
- `api.inspect` reads OpenAPI/Swagger JSON documents and scans common source files for recognizable HTTP route declarations. It does not start services. Results are best-effort and should be cross-checked against the actual framework/router.

## Database migrations

- `database.migrate` applies a workspace SQL file to an existing workspace SQLite database.
- It requires the `write_workspace` capability and explicit approval.
- It runs in a transaction with a timeout; errors trigger rollback. Empty SQL, non-SQL migration paths, oversized files, and SQLite administrative statements such as `ATTACH`, `DETACH`, `VACUUM`, `PRAGMA`, and extension loading are rejected.
- Use `dry_run` first. The migration file and database path must be workspace-relative and pass the existing workspace access policy.

## Containers

- `container.inspect` checks Docker/Podman availability and can list containers or images without modifying them.
- `container.run` starts a disposable container. It requires explicit approval, drops Linux capabilities, enables `no-new-privileges`, uses a read-only root filesystem, does not accept host mounts or privileged mode, and binds published ports to loopback only. Container execution is still code execution: only run trusted images and commands.
- Container execution requires `run_build` and `network_access` capabilities, explicit approval, and a working Docker/Podman installation and daemon. Dry runs do not contact the daemon.

## Safety and honest reporting

- A model argument is never approval. If no request-scoped approval handler exists, a mutating tool fails closed.
- Package installation/update requires `run_build`, `install_dependency`, `network_access`, and explicit request-scoped approval. The model cannot bypass the approval check.
- Build/container commands use direct process invocation where native executables are available; Windows command shims use the same constrained wrapper and reject unsafe arguments. Process timeouts and output limits are enforced.
- Read-only inspections do not change project files. A dry run reports a plan, not a successful execution.
- Tool results report the actual command exit code/output or the reason execution was unavailable. Remote registries, package audit services, and container daemons require their own network and local environment availability.

## Validation

Tier 2 tests cover all 57 tool schemas and registration, read-only package/docs/build/API inspection, docs creation approval and overwrite protection, transactional SQLite migration approval, and dependency/container dry-run behavior.

Run the repository's standard validation suite before release:

```sh
go test ./...
go vet ./...
go build ./cmd/aicli
```

Unit tests do not prove that a specific machine has Docker, Podman, package registries, or a working external network. Those integrations must be tested in the target environment.
