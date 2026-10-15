# Quality Gates and Release Validation

This document separates checks that can run in CI from checks that require a real provider, browser, operating system, or workspace. A green unit-test run is necessary but does not by itself prove every user workflow.

## Required automated checks

Run from the repository root:

```powershell
gofmt -w .
go mod tidy
git diff --exit-code -- go.mod go.sum
node --check internal/api/web/app.js
go test ./...
go vet ./...
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w" -o dist/aicli.exe ./cmd/aicli
.\dist\aicli.exe version
.\dist\aicli.exe --help
```

The GitHub Actions workflow additionally tests the supported Go-version matrix, runs race tests, and scans code with `govulncheck`. Do not describe a change as verified unless the relevant checks actually ran.

## End-to-end coding tasks

Run tasks against disposable fixture repositories, never a developer's only working copy. Record the commit, OS, Go version, provider/model, task result, verification output, changed paths, repair count, latency, and whether rollback was needed.

| Scenario | Required assertion |
| --- | --- |
| Correct a broken Go function | Tests pass and only intended files change |
| Implement a small multi-file feature | New behavior works; existing tests remain green |
| Repair a TypeScript/JavaScript failure | Project verification succeeds when its package manager and dependencies are available |
| Repair a PHP project | PHP syntax and configured project checks pass when PHP is installed |
| Repair a Python project | Syntax and configured project checks pass when Python is installed |
| Provider returns empty content | Bounded retry/fallback or a clear actionable error; no empty success |
| Provider rejects JSON mode | Compatible retry/normalization or a clear error; no unvalidated edit |
| Provider returns malformed or truncated JSON | Bounded repair; no partial destructive edit |
| Request times out or is cancelled | Request ends promptly; resources and UI state are cleaned up |
| Verification fails after edits | Failure is surfaced; recovery data remains available and unrelated changes are preserved |
| Repository includes ignored secrets | Secret files do not enter model context or logs |
| Workspace contains symlink/path escape | Access outside the workspace is rejected |
| Model proposes a shell command | Proposal is not executed automatically |
| GitHub CLI is missing or signed out | Studio explains the prerequisite and offers recovery steps |
| Provider/model selection changes quickly | Stale responses cannot overwrite the current selection |

For provider testing, use mock HTTP servers for deterministic protocol/error cases. Live-provider smoke tests should be opt-in, must not print credentials, and should report provider outages separately from product regressions.

## Security review checklist

- [ ] Verify traversal, absolute paths, drive-qualified paths, and symlink escapes on supported platforms.
- [ ] Verify snapshot/rollback behavior when verification fails, panics, or is cancelled.
- [ ] Verify command allowlists reject unexpected executables and unsafe arguments.
- [ ] Verify local API authentication, loopback binding, browser-origin checks, and WebSocket origin handling.
- [ ] Verify API keys never appear in API responses, UI state, diagnostic logs, panic output, or test failure messages.
- [ ] Verify repository files and model output are treated as untrusted input, not instructions that override policy.
- [ ] Verify GitHub sign-in, disconnect, missing `gh`, expired credentials, and insufficient permissions.
- [ ] Run the dependency vulnerability scan and review actionable findings rather than treating a successful command as a complete security audit.

Do not use production secrets or important working directories for adversarial tests.

## Studio and accessibility review

Use the built Windows executable against a disposable workspace. Check Chat, Workspace, Activity, Settings, Terminal, Preview, Debugging/Diagnostics, GitHub, and Project Health Center.

- [ ] Every visible primary action works and has a recoverable error state.
- [ ] Streaming, cancellation, retries, and rapid provider/model changes leave the interface in a consistent state.
- [ ] File edits are reflected on disk and the UI shows the actual changed paths.
- [ ] Terminal and preview use the selected workspace root and cannot escape it.
- [ ] Keyboard-only navigation reaches all primary actions and shows visible focus.
- [ ] Inputs have accessible names; status changes and errors are understandable without color alone.
- [ ] Browser zoom and narrow viewport layouts do not hide essential controls.
- [ ] Contrast and reduced-motion behavior are reviewed.
- [ ] Test with at least one screen reader where available and document barriers in [ACCESSIBILITY.md](../ACCESSIBILITY.md).

Record browser, OS, viewport, keyboard/screen-reader setup, observed behavior, and reproduction steps. Do not claim WCAG conformance without a suitable audit.

## Performance baseline

Measure before optimizing. On the same machine and workspace, capture at least five runs after one warm-up and report median and slowest observed value for:

- cold startup and first Studio response;
- indexing time for small, medium, and large repositories;
- search latency for common and rare symbols;
- memory at idle and during indexing/chat;
- time from prompt submission to first streamed output;
- time from final model output to completed verification;
- cancellation-to-stop latency.

Record hardware, OS, repository size/file count, provider/model, network conditions, and cache state. Compare changes against a saved baseline; avoid machine-specific absolute thresholds until representative baselines exist.

## Release gate

Before a release, verify the CI run for the exact release commit, inspect generated artifacts and checksums, test a clean install and upgrade path, confirm documented commands match the shipped binary, and review known limitations. If a manual check was not run, state that clearly rather than implying complete verification.
