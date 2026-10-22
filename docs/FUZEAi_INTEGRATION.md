# Using FuzeAI with FuzeCLI

FuzeCLI's agent runner passes its registered tool definitions and JSON schemas to the selected model on every agent run. Tool execution remains inside FuzeCLI; the model requests tools but never executes them directly.

## Start the published FuzeAI GGUF model

FuzeCLI has built-in commands for setup, launch, and status. Install a compatible [llama.cpp release](https://github.com/ggml-org/llama.cpp/releases) that provides `llama-server`, then run:

```powershell
aicli fuzeai setup
aicli fuzeai start
```

`setup` selects the `llamacpp` provider and configures the published model as the default. `start` runs `llama-server -hf r7194714R/fuzeai-qwen-7b-gguf:Q4_K_M`; llama.cpp downloads and caches the GGUF from Hugging Face if it is not already present. The server binds to `127.0.0.1` on port 8080 by default.

Keep the server terminal open. In another terminal, run `aicli chat` or start the Studio with `aicli app`. Check server availability with `aicli fuzeai status`. To change the port or context window, use `aicli fuzeai start --port 8080 --context 4096`.

The default context is 4096 tokens to limit memory use. A 7B Q4 model still needs several gigabytes of RAM/VRAM, and a machine with 8 GB of system RAM may be tight, especially while other applications are open. If loading fails, close memory-heavy apps or use a smaller compatible quantization. The model is hosted on Hugging Face and cached locally by llama.cpp; the GGUF is not committed to the source repository.

## Registered agent tools

The current tool registry exposes 20 tools:

- Workspace: `workspace.list_files`, `workspace.read_file`, `workspace.search_code`, `workspace.find_symbols`, `workspace.apply_edits`
- Project and verification: `project.health`, `verify.run`, `terminal.run`
- Local Git: `git.status`, `git.diff`, `git.commit`
- GitHub: `github.auth`, `github.repositories`, `github.branches`, `github.sync`, `github.pull_requests`, `github.issues`, `github.actions`
- Task context: `context.get`, `context.checkpoint`

The model receives the actual schemas from `FuzeCLIToolDefinitions()`, not a separately maintained list. FuzeCLI validates tool names and arguments before dispatching handlers.

The same agent runner is available from the terminal. After `aicli fuzeai setup` and `aicli fuzeai start`, `aicli chat` automatically uses the tool-enabled agent when the configured provider/model is this FuzeAI GGUF. One-shot `aicli ask "..." --provider llamacpp` uses the same runner. Other providers and models keep their existing chat/edit path unless explicitly selected for the FuzeAI tool protocol.

The terminal asks for confirmation through FuzeCLI before a destructive deletion or other approval-gated action. `--safe` blocks deletion even if a confirmation is attempted. The model cannot approve its own operation; the approval callback runs in FuzeCLI, and the registry grants the handler permission only after that callback approves the exact tool call.

## Destructive file changes

Deletion is a FuzeCLI-controlled approval, not a decision delegated to the model. When a tool call requests deletion, Studio displays the exact workspace-relative paths and asks the user to confirm. Approval is limited to those exact paths and that edit request. Declining, missing approval, or an invalid path leaves the files untouched. FuzeCLI's registry stops the agent loop immediately when authorization is denied, so the model cannot continue with further tool calls after a blocked destructive request.

FuzeCLI also grants the delete capability only for the exact approved edit request. Workspace path validation and the transactional executor still run after approval. The model must rely on tool results and must not claim a deletion, edit, test, or lint check succeeded without a successful result.
