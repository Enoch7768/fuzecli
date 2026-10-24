# FuzeCLI

**FuzeCLI is a terminal-first, safety-focused coding agent with a lightweight local Studio.** It edits real project files, runs bounded verification, explains failures, and can retry repairs without silently executing model-suggested shell commands.

## Try it

Requirements: Go 1.25+ for source builds. A provider API key is needed for hosted models; local OpenAI-compatible providers can be configured instead.

```powershell
git clone https://github.com/Enoch7768/fuzecli.git
cd fuzecli
go run ./cmd/aicli --help
go run ./cmd/aicli init
go run ./cmd/aicli chat
```

For an existing workspace, run `aicli status` and `aicli diff` before applying changes. Use `aicli snapshot` to create a recovery point and `aicli restore` to restore one. Review the proposed diff before trusting an edit.

## Why try it?

FuzeCLI's focus is the verifiable edit loop: workspace-scoped changes, explicit command authorization, diagnostics from verification, bounded repair attempts, and recovery tools. Compared with Aider, Claude Code, Cursor, or Continue, it is not yet a proven replacement; use it when you want to inspect and test a local Go-based agent and help improve its safety.

## Current status

- **Primary interface:** terminal/CLI; Studio is a local companion, not a replacement IDE.
- **Reliability work:** deterministic safety and transaction fixtures run in CI and publish a test artifact. These fixtures measure implementation safeguards, not the success rate of live AI coding tasks.
- **Cross-platform:** Windows is the primary supported experience. Linux/macOS CI and CGO-free build validation are being added; do not assume every integration behaves identically on all systems.
- **Experimental:** broad provider compatibility, Studio integrations, project intelligence, MCP, and benchmark comparisons. Live-model quality, cost, and latency vary by provider and have not yet been independently benchmarked against competitors.
- **Safety:** model-suggested shell commands are informational only and are not automatically executed. Verification commands are subject to the app's command policy. Always review diffs and keep version control backups.

## FuzeAI development

The [FuzeAI Phase 1 audit](docs/FUZEAi_PHASE_1_AUDIT.md) defines the integration boundary, training-data contract, evaluation scorecard, and acceptance gates. It explicitly distinguishes repository CI results from live-model coding benchmarks; base-model and fine-tuned-model scores must be measured and recorded before claiming an improvement. The [reproducible audit and baseline toolkit](training/fuzeai/README.md) contains the JSONL dataset auditor, pinned-revision model runner, unit tests, and instructions for the frozen [20-task evaluation set](evals/fuzeai_baseline_v1.jsonl).

## Additional capabilities

- Unified provider interface with native providers plus 40+ OpenAI-compatible and local inference backends, including OpenAI, Gemini, Groq, Anthropic, OpenRouter, DeepSeek, Mistral, Together, Fireworks, Cerebras, xAI, Ollama, vLLM, LM Studio, LiteLLM, and custom OpenAI-compatible endpoints.
- Configurable provider API keys and models from the local web Settings screen or CLI configuration.
- `--provider auto` fallback across configured providers on rate limits and provider outages.
- Structured JSON code generation with automatic application in interactive chat.
- Immediate generation handling: a complete valid file JSON document is applied as soon as it is detected during streaming, and that response ends immediately.
- Workspace-bound path validation that rejects traversal, absolute paths, and symlink escapes.
- Interactive Windows file picker for chat attachments with multi-select support.
- UTF-8 text attachment validation with a 64 KiB per-file limit and 256 KiB total web-message limit.
- Automatic verification for Go, TypeScript/JavaScript, PHP, and Python projects, with bounded command authorization and structured diagnostics.
- Verification-driven self-correction with configurable attempts.
- SQLite conversation history and touched-file context in `.aicli/`.
- Global developer profile in `%APPDATA%\\aicli\\profile.json`.
- Streaming chat output with an explicit response-end event for the web interface and immediate next-message availability.
- Local HTTP API for applications and integrations.
- MCP server over stdio for MCP-compatible AI clients.
- Repository intelligence with Go AST analysis, source indexing, symbol search, relevance-ranked context, and secret-aware file exclusion.
- Project memory, workspace context graphs, project rules, and reusable skills.
- Background job tracking, local diff review, proof-of-work artifacts, and provider benchmarking.
- Local-only browser inspection and inline code completion through authenticated API endpoints.
- Project Health Center with read-only project-manifest, runtime-availability, Git metadata, and test-file diagnostics.
- Structured diagnostics for verification failures, including stage and severity.
- Versioned Fuze response protocol with compatibility normalization.
- Explicit agent capability boundaries and a bounded task trace.
- Relevance-ranked workspace search over indexed source and symbols.
- Workspace snapshots and restoration for recoverable changes.
- Git status and diff inspection without modifying the repository.
- Model-provided shell commands are informational only and are never executed automatically; verification commands pass through an explicit security policy.
- No API keys are printed or returned by the web settings API; local API requests are rate-limited and protected by loopback/origin/authentication boundaries.

## Build

Requirements for development are Go 1.25+ and a network connection so Go can resolve modules. The SQLite driver is `modernc.org/sqlite`, and the official MCP Go SDK is used for the MCP server.

```powershell
git clone your-repository-url
cd fuzecli
gofmt -w .
go mod tidy
go test ./...
go vet ./...
$env:CGO_ENABLED="0"
go build -trimpath -ldflags="-s -w -X main.version=v0.1.0" -o dist/aicli.exe ./cmd/aicli
.\\dist\\aicli.exe version
.\\dist\\aicli.exe --help

```

For memory-constrained Windows development machines:

```powershell
$env:GOMAXPROCS="2"
go test -p 1 ./...
go build -p 1 -o dist/aicli.exe ./cmd/aicli

```

It is recommended to add the app's `dist` folder to your system's PATH so you can run `aicli` from anywhere.

## Quick start

From an existing project:

```powershell
aicli init
aicli config set default_provider gemini
aicli chat

```

For a one-shot request:

```powershell
aicli ask "Explain the authentication flow"

```

Before applying a risky change, inspect the repository and keep a recovery point available:

```powershell
aicli status
aicli diff
aicli snapshot

```

If you need to recover from a change:

```powershell
aicli restore

```

Run diagnostics when something is wrong:

```powershell
aicli doctor
aicli platform
aicli review
aicli benchmark "Explain this repository architecture"

```

## Run FuzeAI locally

FuzeCLI includes first-class commands for your published FuzeAI GGUF model. Run the installer command, then configure and start FuzeAI:

```powershell
aicli fuzeai install
aicli fuzeai setup
aicli fuzeai start
```

The first start downloads/caches the GGUF from [Hugging Face](https://huggingface.co/r7194714R/fuzeai-qwen-7b-gguf) through llama.cpp. Keep that terminal open while the model server is running; in a second terminal, run `aicli chat` or `aicli app`. FuzeAI is selected as the default provider by `aicli fuzeai setup`.

Useful commands:

```powershell
aicli fuzeai status
aicli fuzeai start --port 8080 --context 4096
aicli fuzeai help
```

The GGUF is intentionally downloaded on demand rather than committed to Git history. It is several gigabytes, and the local llama.cpp cache avoids repeated downloads. The default 4096-token context is a conservative starting point for machines with limited RAM; larger contexts require more memory. The local API binds to loopback, not the public network.

### Run the full FuzeAI Hugging Face model on Kaggle or Colab

The full Transformers model is published at [`r7194714R/FuzeAI-Final`](https://huggingface.co/r7194714R/FuzeAI-Final). This is distinct from the older GGUF repository used by local llama.cpp mode. The remote notebook loads the full model in 4-bit GPU mode and serves an OpenAI-compatible API.

1. Open [`training/fuzeai/fuzeai_kaggle_gpu_endpoint.ipynb`](training/fuzeai/fuzeai_kaggle_gpu_endpoint.ipynb) in GitHub, download it, then import/upload it into Kaggle or Google Colab.
2. Enable a GPU accelerator and Internet access.
3. Run the cells in order. Enter a Hugging Face **read** token when prompted if the model repository is private.
4. Keep the model-server and Cloudflare tunnel cells running. Copy the temporary HTTPS URL printed by the tunnel cell.
5. In PowerShell, connect FuzeCLI to the remote full model:

   ```powershell
   aicli fuzeai connect https://YOUR-URL.trycloudflare.com
   aicli fuzeai status
   aicli chat
   ```

6. When finished, run `aicli fuzeai disconnect` and stop the notebook session.

The same server can be run from a standalone Python file in a Kaggle or Colab terminal/cell after uploading the file or cloning this repository:

```bash
python training/fuzeai/fuzeai_hf_endpoint.py
```

The script installs missing Python dependencies, prompts for a Hugging Face read token if `HF_TOKEN` is not already set, loads the model in 4-bit mode, and serves `/v1/models` and `/v1/chat/completions` on `127.0.0.1:8080`. In a separate cell, start a Quick Tunnel with `cloudflared tunnel --url http://127.0.0.1:8080`. The notebook automates this setup for you.

**Security and limits:** the Quick Tunnel URL is a bearer secret; anyone who gets it may submit prompts to your model. Do not publish it or send sensitive code/secrets. The URL is temporary and changes between sessions. Quick Tunnels are for development/testing, not production hosting. The model requires GPU memory; 4-bit loading reduces memory use but does not guarantee every accelerator/runtime can fit it. Kaggle/Colab sessions can end and their GPUs are not always available.

## Configuration

FuzeCLI stores provider configuration in `%APPDATA%\\aicli\\config.yaml`.

```powershell
aicli apikey set openai sk-...
aicli apikey set gemini YOUR_KEY
aicli apikey set groq gsk-...
aicli apikey status
aicli config set default_provider gemini

```

`config show` masks API keys. The web Settings screen can replace or clear a provider key without exposing the stored value back to the browser.

## GitHub connection

Studio uses the official GitHub CLI (`gh`) for browser-based sign-in. It does not ask you to paste a personal access token or require a separately configured OAuth App.

1. Install [GitHub CLI](https://cli.github.com/) if it is not already installed.
2. Open Studio, select **GitHub**, and choose **Sign in with GitHub CLI**.
3. Complete the browser sign-in flow in the terminal window running FuzeCLI. Studio will verify the session and load repositories when sign-in completes.
4. Use **Disconnect** to disable GitHub access from Studio. This does not sign you out of GitHub CLI itself.

You can also sign in from a terminal:

```powershell
aicli github auth login
aicli github auth status
```

FuzeCLI uses the `gh` session for GitHub API calls and `gh auth setup-git` to configure Git credential integration. Existing saved tokens from older versions remain supported until you sign in through GitHub CLI, which switches FuzeCLI to the `gh` session. Keep your local FuzeCLI configuration private and never commit it.

## Workspace safety

Initialize from the project root:

```powershell
aicli init

```

This creates `.aicli/session.db` and workspace state used for history and context.

FuzeCLI applies two layers of context protection: project-specific `.aicliignore` rules and a built-in list of sensitive-file patterns that cannot be overridden. This is designed to keep environment files, credentials, private keys, and similar secrets out of AI context.

Snapshots provide an explicit local recovery point. Generated paths are constrained to the workspace and symlink escapes are rejected. These controls are safety boundaries, not a guarantee that third-party source code or dependencies are harmless.

A starter ignore file is available as `.aicliignore.example`.

## Web app

Start the polished local interface with:

```powershell
aicli app

```

Open `[http://127.0.0.1:8787](http://127.0.0.1:8787)`. The web UI is embedded in the Go binary; no Node.js server is required.

The web app provides provider/model selection, local provider-key management, saved chat history, file upload, workspace visibility, streaming responses, generated-file cards, and responsive mobile navigation. The interface uses the repository-root `icon.png` and `icon-mark.png` when those files are present.

See [docs/WEB.md](docs/WEB.md) for the complete web workflow and [docs/API.md](docs/API.md) for API authentication.

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
/model gemini-3.8-flash
/status
/clear
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

Default endpoint: `[http://127.0.0.1:8787](http://127.0.0.1:8787)`.

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

See [SECURITY.md](SECURITY.md) for vulnerability reporting and [docs/SECURITY.md](docs/SECURITY.md) for the detailed security model.

## Accessibility and contributions

FuzeCLI is working toward more accessible keyboard, screen-reader, contrast, and responsive workflows. Known limitations and reporting guidance are documented in [ACCESSIBILITY.md](ACCESSIBILITY.md). Contributions should follow [CONTRIBUTING.md](CONTRIBUTING.md); pull requests use a quality checklist, and GitHub issue forms help collect reproducible bug reports and focused feature proposals.

## CI and releases

Every supported development build is validated with formatting checks, module consistency, tests, `go vet`, a CGO-free Windows build, and a CLI smoke test. A separate vulnerability scan runs with `govulncheck`.

Tagged Windows releases are built from a clean checkout and publish both `aicli.exe` and a SHA-256 checksum. GitHub release notes are generated automatically.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the project quality bar, [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for community standards, [SUPPORT.md](SUPPORT.md) for troubleshooting, [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the system boundaries and roadmap, and [evals/README.md](evals/README.md) for the coding-agent evaluation harness.

## Quality gates and end-to-end validation

The [quality-gate checklist](docs/QUALITY_GATES.md) defines repeatable coding-agent scenarios, adversarial security checks, Studio and accessibility review, performance baselines, and release validation. These checks distinguish automated CI results from manual or live-provider tests that still need to be run.

## Project documentation

```text
README.md
CONTRIBUTING.md
CODE_OF_CONDUCT.md
SECURITY.md
ACCESSIBILITY.md
SUPPORT.md
.github/ISSUE_TEMPLATE/bug_report.yml
.github/ISSUE_TEMPLATE/feature_request.yml
.github/pull_request_template.md
CHANGELOG.md
docs/README.md
docs/API.md
docs/MCP.md
docs/WEB.md
docs/TERMINAL.md
docs/SECURITY.md
docs/ARCHITECTURE.md
docs/AGENT_ENGINE.md
docs/QUALITY_GATES.md
evals/README.md

```
