# Contributing to FuzeCLI

Thank you for contributing to FuzeCLI. The project prioritizes correctness, safety, predictable behavior, and a friendly developer experience over feature count.

## Before opening a change

1. Read `README.md` and the relevant documents under `docs/`.
2. Keep changes focused and avoid unrelated refactors.
3. Never commit API keys, credentials, private keys, `.env` files, local databases, or generated build artifacts.
4. Preserve workspace-boundary and secret-exclusion guarantees.
5. Add or update tests for behavior that changes.

## Local validation

From the repository root:

```powershell
gofmt -w .
go mod tidy
go test ./...
go vet ./...
$env:CGO_ENABLED="0"
go build -trimpath -o dist/aicli.exe ./cmd/aicli
.\dist\aicli.exe version
.\dist\aicli.exe --help
```

If you are changing security-sensitive behavior, also review `docs/SECURITY.md` and add regression coverage for the failure mode.

## Pull requests

A good pull request explains:

- what changed;
- why it changed;
- how it was tested;
- any compatibility or migration considerations;
- any security implications.

Avoid claiming a feature is complete without automated coverage or a clear verification path.

## Design principles

- **Safe by default:** generated code must not silently execute arbitrary commands.
- **Workspace boundaries are strict:** paths must remain inside the selected workspace.
- **Secrets stay out of model context:** explicit secret exclusions are part of the security boundary.
- **Recoverable changes:** destructive or generated changes should have a safe recovery path.
- **Provider-neutral core:** provider-specific behavior belongs behind the provider abstraction.
- **Clear failures:** errors should tell users what failed and what they can do next.
- **Small, testable components:** prefer focused packages and deterministic tests.
