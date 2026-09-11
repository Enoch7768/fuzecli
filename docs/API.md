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

For a non-local deployment, set `FUZECLI_API_TOKEN` and send it as a bearer token.

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

Response:

```json
{
  "content": "{\"files\":[...]} ",
  "provider": "gemini",
  "model": "gemini-2.5-flash",
  "written_files": ["app.js"],
  "applied": true
}
```

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

Keep the service on loopback unless remote access is intentionally configured and protected.
