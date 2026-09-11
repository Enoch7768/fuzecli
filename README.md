# FuzeCLI

FuzeCLI (`aicli.exe`) is a Windows-native Go CLI for chatting with multiple AI providers and generating code directly into a workspace. It is designed to ship as a single Windows executable with no Go runtime or CGO dependency at runtime.

## Features

- Unified provider interface for OpenAI, Gemini, Groq, Anthropic, and a running llama.cpp server.
- `--provider auto` fallback across configured providers on rate limits and provider outages.
- Structured JSON code generation with automatic application in interactive chat.
- Workspace-bound path validation to block `..` traversal and writes outside the project root.
- Interactive Windows file picker for chat attachments with multi-select support.
- UTF-8 text attachment validation with a 64 KiB per-file limit.
- Automatic verification for Go, TypeScript, PHP, and Python projects.
- Verification-driven self-correction with configurable attempts.
- SQLite conversation history and touched-file context in `.aicli/`.
- Global developer profile in `%APPDATA%\\aicli\\profile.json`.
- Streaming chat output where providers support it.
- Local HTTP API for applications and integrations.
- MCP server over stdio for MCP-compatible AI clients.
- No API keys are printed or logged.

## Build

Requirements for development are Go 1.23+ and a network connection so Go can resolve modules. The SQLite driver is `modernc.org/sqlite`, and the official MCP Go SDK is used for the MCP server.

```powershell
git clone your-repository-url
cd fuzecli
go test ./...
$env:CGO_ENABLED="0"
go build -trimpath -ldflags="-s -w -X main.version=v0.1.0" -o dist/aicli.exe ./cmd/aicli
```

The resulting `dist/aicli.exe` is the distributable CLI.

## Configuration

FuzeCLI stores configuration in `%APPDATA%\\aicli\\config.yaml`.

```powershell
aicli config set openai.api_key sk-...
aicli config set groq.api_key gsk-...
aicli config set default_provider groq
aicli config show
```

`config show` masks API keys.

## Workspace

Initialize from the project root:

```powershell
aicli init
```

This creates `.aicli/session.db` and workspace state used for history and context.

## Interactive chat

```powershell
aicli chat
```

Inside chat:

```text
/file
```

opens the native Windows multi-file picker. Selected files are attached to the session and supplied directly to the next prompt.

```text
/file src/app.go
/file list
/file clear
```

You can also attach a file directly by its workspace-relative path.

Project changes are requested naturally. When the model returns valid FuzeCLI file JSON, FuzeCLI applies it automatically.

## API

Start the local application API with:

```powershell
aicli api
```

Default endpoint: `http://127.0.0.1:8787`.

See [docs/API.md](docs/API.md) for the HTTP endpoints, request formats, file attachment behavior, and authentication.

## MCP

Start the MCP server with:

```powershell
aicli mcp
```

The server communicates over stdio and exposes FuzeCLI chat, workspace file reading, and workspace file listing tools.

See [docs/MCP.md](docs/MCP.md) for MCP client configuration and tool schemas.

## Safety model

Generated file paths are resolved relative to the active workspace. Absolute paths and traversal paths are rejected. Attached files are read-only inputs and are limited to UTF-8 text files of at most 64 KiB each. The CLI never executes arbitrary commands returned in generated JSON. File writes use a temporary file followed by a rename. API access is loopback by default and can use `FUZECLI_API_TOKEN` for bearer authentication.

## Development layout

```text
cmd/aicli/
internal/app/
internal/api/
internal/mcpserver/
internal/config/
internal/provider/
internal/generation/
internal/workspace/
internal/profile/
internal/verify/
internal/ui/
docs/API.md
docs/MCP.md
```
