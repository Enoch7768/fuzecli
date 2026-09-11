# FuzeCLI API

FuzeCLI exposes a local HTTP API for applications that need access to the active workspace and FuzeCLI chat engine.

## Start

```powershell
aicli api
```

Default address:

```text
http://127.0.0.1:8787
```

Custom address:

```powershell
aicli api --addr 127.0.0.1:9000
```

For deployments that intentionally expose the application API beyond loopback, use `FUZECLI_API_TOKEN` and send it as a bearer token. Keep the service on loopback unless remote access is deliberately configured and protected.

## Health

```http
GET /v1/health
```

Response:

```json
{"ok":true,"workspace":"C:/project"}
```

## Chat

```http
POST /v1/chat
Content-Type: application/json
```

Request:

```json
{
  "prompt": "Review app.js and fix the null handling",
  "files": ["app.js"],
  "provider": "gemini",
  "model": "gemini-2.5-flash",
  "apply": true
}
```

`files` contains workspace-relative text paths. Files are validated as workspace-bound UTF-8 text and limited to 64 KiB each. When the model returns valid FuzeCLI file JSON and `apply` is true, the changes are applied automatically.

Generated command metadata is not executed automatically.

## Web event lifecycle

The local web application consumes `/api/events` as server-sent events. A normal request emits planning/generating events, streamed `chat_token` events, and one terminal `chat_end` event. A generated-file event may precede `chat_end`.

The client must treat `chat_end` as the authoritative response-completion signal and must make the next request available immediately after receiving it.

## Read a file

```http
GET /v1/file?path=src/app.js
```

The endpoint returns the UTF-8 text of a workspace-relative file and rejects paths outside the workspace.

## List files

```http
GET /v1/files
GET /v1/files?prefix=src
```

The listing excludes `.git` and `.aicli` and is capped at 1000 paths.

## Authentication

By default the API binds to loopback and does not need a token. Set `FUZECLI_API_TOKEN` to require:

```http
Authorization: Bearer YOUR_TOKEN
```

## Provider keys

Provider API keys are configured locally through FuzeCLI configuration. The web application's `/api/config` endpoint accepts a key for the selected provider, but its GET response exposes only `api_key_configured: true|false`; the stored key value is never returned to the browser.

Changing a key is a state-changing request and is limited to the local browser origin. Keys are stored in the local FuzeCLI configuration, not in the workspace database or browser history.

See [SECURITY.md](SECURITY.md) for the complete security model.
