# FuzeCLI Tier 3 toolset

Tier 3 completes the planned image, browser, and deployment tools. The full registry now contains **69 tools**: the original 20, 25 Tier 1 tools, 12 Tier 2 tools, and 12 Tier 3 tools. The canonical strict schemas are in `internal/agent/fuzecli_tools.go`; dispatch is in `internal/agent/tool_runtime.go`; implementation is in `internal/agent/tier3_runtime.go`.

## Image tools

- `image.inspect` reads supported PNG, JPEG, and GIF image metadata: format, dimensions, pixel count, the decoder-reported color-model type, file size, and modification time.
- `image.compare` compares two supported images with a bounded sample of pixel channels. It reports dimensions, sampled pixel count, mean absolute channel difference, and exact-sample match ratio.
- Image comparison is **not** semantic image understanding. Different encodings, scaling, color profiles, and small image shifts can change the pixel metrics. Each input is limited to 64 MiB and 16 million decoded pixels.

## Browser tools

Browser tools connect to a local Chrome/Chromium DevTools Protocol endpoint. Set `FUZECLI_BROWSER_CDP_URL` to a local origin such as `http://127.0.0.1:9222`; by default the tool uses that endpoint. The endpoint must resolve to localhost or a loopback IP. Start Chrome/Chromium with remote debugging enabled in a trusted local environment. Do not expose the DevTools port to a network: it grants powerful control over browser tabs and authenticated sessions.

- `browser.open` opens a new tab at a valid HTTP(S) URL. The tool rejects non-HTTP(S) schemes and embedded URL credentials.
- `browser.inspect` returns the page title, URL, readiness state, bounded visible text, and optional selected-element details.
- `browser.click`, `browser.type`, and `browser.select` interact with CSS-selected page elements and require explicit approval. Clicking can submit forms or trigger external actions.
- `browser.screenshot` captures PNG/JPEG. Inline return is bounded; a workspace path saves a new file only after approval and never overwrites an existing file.
- `browser.console` instruments common console methods when a tab is opened through FuzeCLI. It cannot recover console messages emitted before instrumentation, and navigation/reloads can reset the buffer.
- `browser.network` reads browser resource-timing entries. It does not provide HTTP status codes, response bodies, or a complete record of failed requests.

Browser use requires the `network_access` capability. These tools do not bypass website authentication or browser security boundaries. Results depend on the installed Chrome/Chromium version and its DevTools protocol.

## Deployment tools

- `deploy.inspect` lists known deployment manifests and reports whether Vercel, Netlify, Fly, and Railway CLIs are installed. CLI presence does not prove that the project is linked, authenticated, or configured correctly.
- `deploy.run` supports fixed, provider-specific commands for Vercel, Netlify, Fly, and Railway. It does not accept arbitrary shell commands. `dry_run=true` returns the exact planned argument list and performs no deployment.
- A real deployment requires both `run_build` and `network_access` capabilities, an explicit approval grant for the exact arguments, the corresponding CLI, and valid provider-side configuration. Vercel and Netlify use their explicit production flags; Railway can target its named production environment. Fly deployment targets the app configured in `fly.toml`, so `production=true` is rejected for Fly rather than pretending the flag selects a safe environment. For Railway with `production=false`, the active linked environment may still be production; verify provider state before approval. Production deployments are external, potentially irreversible operations.
- Command output is bounded and secret-shaped values are redacted before returning results. Redaction is best-effort, not a guarantee that arbitrary provider output contains no sensitive data.

## Safety and error handling

- Mutating browser actions and deployments require request-scoped approval; model arguments alone never count as approval.
- Tool commands are invoked as argument arrays, never via a shell string.
- Browser DevTools access is restricted to loopback endpoints to avoid turning a configured remote endpoint into an SSRF path.
- Unsupported image formats, unavailable browser endpoints, missing deployment CLIs, invalid selectors, rejected permissions, and command failures return explicit errors or structured failure results. A dry-run response always states that execution did not occur.

## Validation

Tier 3 tests cover strict registration of all 69 schemas, duplicate names, image metadata, identical/different pixel comparisons, browser URL/DevTools endpoint restrictions, approval policies, deployment inspection, and deployment dry runs.

Run the complete CI validation locally:

```sh
go test ./...
go vet ./...
go build ./cmd/aicli
```

A successful unit suite does not replace a live smoke test against a local Chrome/Chromium instance or authenticated staging deployment. Those tests require the relevant local browser, capabilities, CLI tools, and credentials.
