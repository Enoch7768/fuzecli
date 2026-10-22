# Using FuzeAI with FuzeCLI

FuzeCLI's agent runner passes its registered tool definitions and JSON schemas to the selected model on every agent run. Tool execution remains inside FuzeCLI; the model requests tools but never executes them directly.

## Start the published FuzeAI GGUF model

The Hugging Face repository is `r7194714R/fuzeai-qwen-7b-gguf`, with the `Q4_K_M` file `Qwen2.5-Coder-7B-Instruct.Q4_K_M.gguf`.

Install llama.cpp, then run this in a terminal:

```powershell
llama serve -hf r7194714R/fuzeai-qwen-7b-gguf:Q4_K_M --host 127.0.0.1 --port 8080
```

Keep that terminal running. FuzeCLI's `llamacpp` provider connects to `http://localhost:8080/v1`. Its default model is configured to `r7194714R/fuzeai-qwen-7b-gguf:Q4_K_M`. In the FuzeCLI Studio provider selector, choose `llamacpp`; the default cloud provider is unchanged.

The model is hosted as a downloadable GGUF, not a hosted Hugging Face Inference Provider endpoint. Running llama.cpp locally serves the model through the OpenAI-compatible API that FuzeCLI already supports.

## Registered agent tools

The current tool registry exposes 20 tools:

- Workspace: `workspace.list_files`, `workspace.read_file`, `workspace.search_code`, `workspace.find_symbols`, `workspace.apply_edits`
- Project and verification: `project.health`, `verify.run`, `terminal.run`
- Local Git: `git.status`, `git.diff`, `git.commit`
- GitHub: `github.auth`, `github.repositories`, `github.branches`, `github.sync`, `github.pull_requests`, `github.issues`, `github.actions`
- Task context: `context.get`, `context.checkpoint`

The model receives the actual schemas from `FuzeCLIToolDefinitions()`, not a separately maintained list. FuzeCLI validates tool names and arguments before dispatching handlers.

## Destructive file changes

Deletion is a FuzeCLI-controlled approval, not a decision delegated to the model. When a tool call requests deletion, Studio displays the exact workspace-relative paths and asks the user to confirm. Approval is limited to those exact paths and that edit request. Declining, missing approval, or an invalid path leaves the files untouched. FuzeCLI's registry stops the agent loop immediately when authorization is denied, so the model cannot continue with further tool calls after a blocked destructive request.

FuzeCLI also grants the delete capability only for the exact approved edit request. Workspace path validation and the transactional executor still run after approval. The model must rely on tool results and must not claim a deletion, edit, test, or lint check succeeded without a successful result.
