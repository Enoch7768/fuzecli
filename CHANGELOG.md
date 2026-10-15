## Unreleased

- Improved live preview startup for npm, pnpm, Yarn, and Bun projects, including declared package-manager precedence and Windows Bun executable handling.
- Hardened absolute workspace file operations against symlink escapes and restricted runtime-preview shutdown to loopback requests.
- Added regression tests for workspace path containment, preview package-manager selection, Windows launch behavior, and local-only preview shutdown.
- Added a read-only Project Health Center for detecting project manifests, relevant runtime availability, Git metadata, and likely test files.

- Replaced the Studio GitHub PAT form with official GitHub CLI browser sign-in.
- Added observable sign-in progress and explicit Studio disconnect behavior.
- Removed the standalone OAuth device-flow endpoints and PAT submission endpoint.
- Separated clearing a saved legacy token from explicitly disconnecting GitHub, so CLI sign-in can be used without stale configuration blocking it.

### Production hardening

- Documented the current multi-provider and local-provider architecture.
- Added API rate limiting, bounded chat concurrency, and request IDs.
- Added generation-plan fuzz coverage for malformed and adversarial JSON input.
- Added focused API regression tests and a coding-agent evaluation harness specification.
- Improved source-search token matching without recompiling regular expressions for every scanned line.

### Documentation and community

- Added a project Code of Conduct.
- Added security reporting guidance.
- Added a support guide for troubleshooting, bug reports, and feature requests.
- Added a central documentation index and cross-links between project policies and technical documentation.

# Changelog

All notable FuzeCLI changes are documented here.

## Unreleased — Final Release Hardening

### Security

- Hardened workspace context handling and sensitive-file exclusion.
- Added `.aicliignore` support and regression coverage for secret-file exclusion.
- Preserved workspace-bound path and symlink protections.
- Kept model-provided shell commands informational rather than automatically executable.

### Intelligence and agent workflow

- Added repository code indexing and symbol-aware search foundations.
- Added structured diagnostics for compiler and test failures.
- Improved verification so failures can be consumed by the correction workflow.
- Added workspace snapshot and restoration foundations for recoverable changes.
- Added Git status and diff inspection commands.

### Developer experience

- Added `doctor`, `snapshot`, `restore`, `status`, and `diff` commands.
- Improved terminal session startup and provider/model visibility.
- Added contribution and quality guidelines.

### Release engineering

- CI validates formatting, modules, tests, vet, builds, and CLI smoke tests.
- Dependency/code vulnerability scanning runs in CI.
- Windows releases publish a SHA-256 checksum alongside the executable.
- Release builds generate GitHub release notes automatically.

This section remains `Unreleased` until a tagged public release has passed the full CI and release validation pipeline.
