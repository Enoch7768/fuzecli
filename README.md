# FuzeCLI

FuzeCLI (`aicli.exe`) is a Windows-native Go CLI for chatting with multiple AI providers and generating code directly into a workspace. It is designed to ship as a single statically linked Windows executable with no Go runtime or CGO dependency.

## Features

- Unified provider interface for OpenAI, Gemini, Groq, Anthropic, and a running llama.cpp server.
- `--provider auto` fallback across configured providers on rate limits and provider outages.
- Structured JSON code generation; fenced Markdown parsing is intentionally not used.
- Workspace-bound path validation to block `..` traversal and writes outside the project root.
- Diff preview with `y/n/edit` confirmation, plus `--yes` for automation.
- Automatic verification for Go, TypeScript, PHP, and Python projects.
- Verification-driven self-correction with configurable attempts.
- SQLite conversation history and touched-file context in `.aicli/`.
- Global developer profile in `%APPDATA%\\aicli\\profile.json`.
- Streaming chat output where providers support it.
- No API keys are printed or logged.

## Build

Requirements for development are Go 1.23+ and a network connection so Go can resolve modules. The SQLite driver is `modernc.org/sqlite`, so Windows release builds use `CGO_ENABLED=0`.

```powershell
git clone your-repository-url
cd fuzecli
go test ./...
$env:CGO_ENABLED="0"
go build -trimpath -ldflags="-s -w -X main.version=v0.1.0" -o dist/aicli.exe ./cmd/aicli
```

The resulting `dist/aicli.exe` is the distributable CLI.

## Configuration

FuzeCLI stores configuration in `%APPDATA%\\aicli\\config.yaml`. Example:

```yaml
default_provider: groq
providers:
  openai:
    api_key: ""
    default_model: gpt-4o
  gemini:
    api_key: ""
    default_model: gemini-1.5-pro
  groq:
    api_key: ""
    default_model: llama3-70b-8192
  anthropic:
    api_key: ""
    default_model: claude-sonnet-4-6
  llamacpp:
    base_url: http://localhost:8080
    default_model: local
fallback_order: [groq, gemini, openai]
verification:
  self_correction_attempts: 2
```

Set provider credentials without exposing them:

```powershell
aicli config set openai.api_key sk-...
aicli config set groq.api_key gsk-...
aicli config set default_provider groq
aicli config show
```

`config show` masks all but the last four key characters.

## Workspace

Initialize from the project root:

```powershell
aicli init
```

This creates:

```text
.aicli/
  session.db
  state.json
```

The project `.gitignore` is updated with `.aicli/` automatically.

## Usage

```powershell
aicli chat
aicli ask "Create a production-ready Go HTTP health endpoint"
aicli ask "Refactor this function" --provider groq --model llama3-70b --yes
type file.txt | aicli ask "Refactor this input into idiomatic Go"
aicli profile show
aicli history
```

Interactive chat streams ordinary assistant responses. Use `/code <request>` inside chat to invoke structured code generation. `--yes` skips the write confirmation for `/code` and one-shot `ask`.

## Safety model

Generated files are always resolved relative to the active workspace. Absolute paths and traversal paths containing `..` are rejected. Provider output is parsed with `encoding/json` and unknown JSON fields are rejected. The CLI never executes arbitrary commands returned in the `commands` array; those commands are shown as suggestions only.

File writes are performed through a temporary file followed by a rename. Verification runs only against the detected project toolchain. Verification failures are returned to the configured provider for a bounded self-correction loop.

## Development layout

```text
cmd/aicli/
internal/app/
internal/config/
internal/provider/
internal/generation/
internal/workspace/
internal/profile/
internal/verify/
internal/ui/
```

Provider-specific adapters are isolated under `internal/provider/<name>/` and all implement the shared `provider.Provider` interface.

## Release workflow

`.github/workflows/release.yml` runs tests and produces `dist/aicli.exe` with `CGO_ENABLED=0` on every tag matching `v*`, then attaches the executable to the GitHub Release.
