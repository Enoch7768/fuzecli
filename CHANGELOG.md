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
