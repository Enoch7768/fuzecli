# FuzeCLI Security

FuzeCLI is designed as a local-first coding assistant. Its security boundary is the user's workspace, the local configuration directory, and the provider connection selected by the user.

## Provider credentials

Provider API keys are stored in `%APPDATA%\\aicli\\config.yaml`. FuzeCLI creates the configuration directory with restrictive permissions and the configuration file with owner-only permissions where the operating system supports them. The web settings endpoint never returns the key value; it returns only whether a key is configured.

The web application sends a new key only when the user explicitly enters one. Clearing a key removes it from the local configuration.

## Local web boundary

The web application binds to `127.0.0.1:8787` by default. State-changing web endpoints reject foreign browser origins. Responses include restrictive security headers including a Content Security Policy, frame protection, MIME sniffing protection, and a no-referrer policy.

The application API has a separate bearer-token option documented in `docs/API.md` for deployments that intentionally expose it beyond loopback.

## Workspace isolation

Generated paths must be relative to the active workspace. Absolute paths, drive-qualified paths, traversal segments, and symlink paths that escape the workspace are rejected.

Workspace context is filtered before it is sent to a model. FuzeCLI always excludes known secret-bearing paths including `.env*`, private-key formats, credential/secret files, and common local credential stores. Projects can add further exclusions in a root `.aicliignore` file. Ignore rules only add restrictions; they cannot override the built-in secret blocklist.

Binary files and common dependency/build directories are excluded from automatic workspace context. Large text files are split into bounded source chunks before being assembled into model context.

Workspace attachments are limited to UTF-8 text and capped at 64 KiB per file in the web and terminal interfaces. The web interface caps a message's attachment payload at 256 KiB and six files.

## Generated code

A model-generated file plan is parsed as strict JSON before it can change the workspace. Unknown JSON fields, invalid actions, invalid paths, and non-empty delete content are rejected.

When a complete valid generation JSON document is detected during streaming, FuzeCLI applies it immediately and ends that response. It does not wait for the model to continue with unrelated trailing text.

Model-provided shell commands are informational data only. FuzeCLI does not automatically execute commands returned inside generation JSON.

File replacement is performed through a temporary file followed by a rename. Conversation state and file hashes are stored locally in `.aicli/`.

## Recovery

FuzeCLI provides explicit local snapshots through `aicli snapshot` and restoration through `aicli restore`. Snapshots are stored under `.aicli/snapshots` with restrictive directory permissions. Restoring a snapshot is deliberately explicit and does not execute commands or remove unrelated workspace files.

For source-controlled projects, use `aicli status` and `aicli diff` to inspect the Git working tree before and after an AI-assisted change.

## Response lifecycle

Streaming providers emit a terminal completion signal through the provider interface. The web server converts completed work into a dedicated `chat_end` event. The web client unlocks the composer exactly once when that event arrives, allowing the next message to be sent immediately.

## Threat model limitations

FuzeCLI cannot guarantee that arbitrary user code, dependencies, provider output, or a workspace is malware-free. FuzeCLI limits what it executes automatically and restricts generated writes to the workspace boundary, but users should still use normal operating-system, dependency, source-control, and endpoint-security practices.
