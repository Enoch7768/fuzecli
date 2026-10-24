# Using FuzeAI with FuzeCLI

FuzeCLI can use FuzeAI in two ways:

- **Full Hugging Face Transformers model on Kaggle/Colab:** use `r7194714R/FuzeAI-Final` through the remote endpoint notebook below.
- **Local GGUF through llama.cpp:** use the separate GGUF repository `r7194714R/fuzeai-qwen-7b-gguf:Q4_K_M`. This is the older local inference path; it is not the same artifact as the full Transformers repository.

FuzeCLI's agent runner passes its registered tool definitions and JSON schemas to the selected model. Tool execution remains inside FuzeCLI; the model requests tools but never executes them directly. The remote endpoint supports the OpenAI-compatible `/v1/models` and non-streaming `/v1/chat/completions` endpoints.

## Use the full FuzeAI model on a Kaggle or Colab GPU

The full model repository is [`r7194714R/FuzeAI-Final`](https://huggingface.co/r7194714R/FuzeAI-Final). Use the ready-to-run [Hugging Face GPU endpoint notebook](../training/fuzeai/fuzeai_kaggle_gpu_endpoint.ipynb). It loads the published model in 4-bit mode, serves a local OpenAI-compatible API, installs `cloudflared`, and opens a temporary HTTPS tunnel.

1. Download/import `training/fuzeai/fuzeai_kaggle_gpu_endpoint.ipynb` into Kaggle or Google Colab.
2. Enable a GPU accelerator and Internet access.
3. Run the notebook cells from top to bottom.
4. When prompted, enter a Hugging Face **read** token if the model repository is private. Do not commit or share the token.
5. Wait for the API health check to pass, then run the tunnel cell and copy its HTTPS `trycloudflare.com` URL.
6. In the terminal on your own computer, run:

   ```powershell
   aicli fuzeai connect https://YOUR-URL.trycloudflare.com
   aicli fuzeai status
   aicli chat
   ```

7. When finished, run `aicli fuzeai disconnect`, interrupt the tunnel cell, and stop the GPU session.

The same endpoint can run from the standalone Python script `training/fuzeai/fuzeai_hf_endpoint.py`. In a Kaggle/Colab terminal or cell, run:

```bash
python training/fuzeai/fuzeai_hf_endpoint.py
```

It installs missing Python packages, prompts for a Hugging Face read token if `HF_TOKEN` is not set, loads the model, and serves the API on `http://127.0.0.1:8080`. In another cell, run `cloudflared tunnel --url http://127.0.0.1:8080`.

## Local GGUF mode

FuzeCLI also retains local llama.cpp support. Install the runtime, select the local GGUF provider, and start the server:

```powershell
aicli fuzeai install
aicli fuzeai setup
aicli fuzeai start
aicli fuzeai status
```

This path uses `r7194714R/fuzeai-qwen-7b-gguf:Q4_K_M` and caches the GGUF locally. It does not download the full Transformers model. The default local context is 4096 tokens; a 7B Q4 model still needs several gigabytes of RAM/VRAM.

## Connect and disconnect behavior

When you run `aicli fuzeai connect https://YOUR-URL.trycloudflare.com`, FuzeCLI switches the `llamacpp` provider base URL to the remote endpoint and sets the model identifier to `r7194714R/FuzeAI-Final`. The server accepts OpenAI-compatible chat-completion requests; streaming is intentionally disabled so responses work through the temporary tunnel.

Run `aicli fuzeai disconnect` to reset the model identifier and endpoint to the local GGUF mode. The model and tool execution remain separated: FuzeAI generates tool requests, while FuzeCLI validates and executes its registered workspace, terminal, Git, and GitHub tools locally.

## Registered agent tools

The current tool registry exposes 20 tools:

- Workspace: `workspace.list_files`, `workspace.read_file`, `workspace.search_code`, `workspace.find_symbols`, `workspace.apply_edits`
- Project and verification: `project.health`, `verify.run`, `terminal.run`
- Local Git: `git.status`, `git.diff`, `git.commit`
- GitHub: `github.auth`, `github.repositories`, `github.branches`, `github.sync`, `github.pull_requests`, `github.issues`, `github.actions`
- Task context: `context.get`, `context.checkpoint`

The model receives the actual schemas from FuzeCLI's tool definitions, not a separately maintained list. FuzeCLI validates tool names and arguments before dispatching handlers.

The terminal asks for confirmation through FuzeCLI before a destructive deletion or other approval-gated action. `--safe` blocks deletion even if a confirmation is attempted. The model cannot approve its own operation; the approval callback runs in FuzeCLI, and the registry grants the handler permission only after that callback approves the exact tool call.

## Remote endpoint security and limitations

The notebook binds the API to loopback and uses a temporary Cloudflare Quick Tunnel. The URL is effectively a bearer secret: anyone who obtains it may submit inference requests. Do not publish it, share it broadly, or send private source code/secrets you do not want transmitted to the GPU notebook. Stop the tunnel when finished.

Quick Tunnels are temporary development tunnels, not always-on hosting. The URL changes between sessions; the service stops when the notebook process/session stops. The notebook uses a non-streaming endpoint; streaming/SSE is not supported. Kaggle/Colab GPU availability, memory limits, and session limits still apply.
