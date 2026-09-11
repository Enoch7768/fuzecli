# FuzeCLI

FuzeCLI (`aicli.exe`) is a Windows-native Go CLI for chatting with multiple AI providers and generating code directly into a workspace. It is local-first, workspace-aware, and designed to ship as a single Windows executable with no Go runtime or CGO dependency at runtime.

## What FuzeCLI does

- Unified provider interface for OpenAI, Gemini, Groq, Anthropic, and a running llama.cpp server.
- Configurable provider API keys and models from the local web Settings screen or CLI configuration.
- `--provider auto` fallback across configured providers on rate limits and provider outages.
- Structured JSON code generation with automatic application in interactive chat.
- Immediate generation handling: a complete valid file JSON document is applied as soon as it is detected during streaming, and that response ends immediately.
- Workspace-bound path validation that rejects traversal, absolute paths, and symlink escapes.
- Interactive Windows file picker for chat attachments with multi-select support.
- UTF-8 text attachment validation with a 64 KiB per-file limit and 256 KiB total web-message limit.
- Automatic verification for Go, TypeScript, PHP, and Python projects.
- Verification-driven self-correction with configurable attempts.
- SQLite conversation history and touched-file context in `.aicli/`.
- Global developer profile in `%APPDATA%\\aicli\\profile.json`.
- Streaming chat output with an explicit response-end event for the web interface and immediate next-message availability.
- Local HTTP API for applications and integrations.
- MCP server over stdio for MCP-compatible AI clients.
- Model-provided shell commands are informational only and are never executed automatically.
- No API keys are printed or returned by the web settings API.

## Build

Requirements for development are Go 1.23+ and a network connection so Go can resolve modules. The SQLite driver is `modernc.org/sqlite`, and the official MCP Go SDK is used for the MCP server.

```powershell
git clone your-repository-url
cd fuzecli
go test ./...
$env:CGO_ENABLED="0"
go build -trimpath -ldflags="-s -w -X main.version=v0.1.0" -o dist/aicli.exe ./cmd/aicli
```

For memory-constrained Windows development machines:

```powershell
$env:GOMAXPROCS="2"
go test -p 1 ./...
go build -p 1 -o dist/aicli.exe ./cmd/aicli
```

## Configuration

FuzeCLI stores provider configuration in `%APPDATA%\\aicli\\config.yaml`.

```powershell
aicli config set openai.api_key sk-...
aicli config set gemini.api_key YOUR_KEY
aicli config set groq.api_key gsk-...
aicli config set default_provider gemini
aicli config show
```

`config show` masks API keys. The web Settings screen can replace or clear a provider key without exposing the stored value back to the browser.

## Workspace

Initialize from the project root:

```powershell
aicli init
```

This creates `.aicli/session.db` and workspace state used for history and context.

## Web app

Start the polished local interface with:

```powershell
aicli app
```

Open `http://127.0.0.1:8787`.

The web app provides provider/model selection, local provider-key management, saved chat history, file upload, workspace visibility, streaming responses, generated-file cards, and responsive mobile navigation. The interface uses the repository-root `icon.png` and `icon-mark.png` when those files are present.

See [docs/WEB.md](docs/WEB.md) for the complete web workflow.

## Interactive terminal

```powershell
aicli chat
```

Inside chat:

```text
/file
/file src/app.go
/file list
/file clear
/provider gemini
/model gemini-2.5-flash
/status
/help
/exit
```

The terminal streams normal responses as they arrive. A complete generation JSON document is applied immediately, changed files are reported, and the prompt is ready for the next request when the response is complete.

See [docs/TERMINAL.md](docs/TERMINAL.md).

## API

Start the local application API with:

```powershell
aicli api
```

Default endpoint: `http://127.0.0.1:8787`.

See [docs/API.md](docs/API.md) for HTTP endpoints, request formats, authentication, and workspace access.

## MCP

Start the MCP server with:

```powershell
aicli mcp
```

The server communicates over stdio and exposes FuzeCLI chat, workspace file reading, and workspace file listing tools.

See [docs/MCP.md](docs/MCP.md) for MCP client configuration and tool schemas.

## Security model

FuzeCLI is local-first and binds the web app to loopback by default. State-changing web endpoints validate browser origins and the web server emits restrictive security headers. Provider keys are stored in the local configuration directory with restrictive permissions and are never returned through the web settings API.

Generated file paths are resolved inside the active workspace with traversal and symlink-escape checks. File replacement uses a temporary file followed by a rename. Generated command metadata is never executed automatically. Attachments are read-only UTF-8 text inputs with strict size limits.

These controls reduce the execution surface but cannot make arbitrary user workspaces, third-party dependencies, or provider-generated source code mathematically or operationally guaranteed to be malware-free. Normal endpoint security, source control, dependency review, and safe execution practices still apply.

See [docs/SECURITY.md](docs/SECURITY.md).

## Project documentation

```text
README.md
docs/API.md
docs/MCP.md
docs/WEB.md
docs/TERMINAL.md
docs/SECURITY.md
```
