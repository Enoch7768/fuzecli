# FuzeCLI API

FuzeCLI exposes a local HTTP API and an embedded web application.

## Web application

Start the polished local interface with:

```powershell
aicli app
```

Open:

```text
http://127.0.0.1:8787
```

The interface is embedded into the FuzeCLI executable. It does not require Node, npm, a separate frontend server, or an internet connection.

The web app includes:

- Chat with provider and model selection
- Automatic generated-file application
- Workspace file browser and text preview
- Session activity/history
- Provider/model settings
- Provider-key configuration status without exposing key values
- Responsive navigation for smaller screens

## API

Start the API with:

```powershell
aicli api
```

The default address is:

```text
http://127.0.0.1:8787
```

A non-loopback address is refused unless `FUZECLI_API_TOKEN` is configured.

Example:

```powershell
$env:FUZECLI_API_TOKEN="replace-with-a-long-random-token"
aicli api --addr 0.0.0.0:8787
```

External API requests must send:

```http
Authorization: Bearer YOUR_TOKEN
```

The browser application uses a short-lived local HttpOnly session cookie and strict local-origin checks instead of placing the API token in browser JavaScript.

## Endpoints

### Health

```http
GET /v1/health
```

### Configuration

```http
GET /v1/config
POST /v1/config
```

The GET response contains provider names, default models, configuration status, and workspace path. It never contains provider API-key values.

### Chat

```http
POST /v1/chat
Content-Type: application/json
```

```json
{
  "prompt": "Review app.js and fix the null handling",
  "files": ["app.js"],
  "provider": "gemini",
  "model": "gemini-2.5-flash",
  "apply": true
}
```

`files` contains workspace-relative UTF-8 text paths. Each attachment is limited to 64 KiB and the request body is capped.

### Read a file

```http
GET /v1/file?path=src/app.js
```

### List files

```http
GET /v1/files
GET /v1/files?prefix=src
```

### History

```http
GET /v1/history
```

### Touched files

```http
GET /v1/touched
```

## API keys

Provider keys are stored locally by FuzeCLI. The simple CLI command is:

```powershell
aicli apikey set gemini YOUR_KEY
aicli apikey set openai YOUR_KEY
aicli apikey set groq YOUR_KEY
aicli apikey set anthropic YOUR_KEY
```

Check status:

```powershell
aicli apikey status
```

Remove a key:

```powershell
aicli apikey clear gemini
```

Keys are never returned through the web configuration endpoint.

See [SECURITY.md](SECURITY.md) for the security model.
