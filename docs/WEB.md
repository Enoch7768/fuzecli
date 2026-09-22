# FuzeCLI Web App

The FuzeCLI web app is a self-contained local interface served directly by the Go executable.

## Start

```powershell
aicli app
```

Then open:

```text
http://127.0.0.1:8787
```

No Node.js runtime or frontend development server is required.

## Views

### Chat

Chat is the primary workspace. Provider and model selection live in the top bar. Requests can generate and apply workspace changes, and the response reports files changed by the generation engine.

### Workspace

Workspace shows the files visible to FuzeCLI, excludes internal state directories, and lets you inspect text files without leaving the browser.

### Activity

Activity shows the persisted local conversation history so a session can be inspected after a page refresh.

### Settings

Settings controls the default provider and model. API-key values are intentionally never sent to the browser. Use `aicli apikey set <provider> <key>` for direct key management.

## Security

The web UI is intended for loopback use. It receives an HttpOnly SameSite session cookie and state-changing requests are restricted to local browser origins. The server sends CSP, frame, MIME, referrer, and permissions headers.

If the API is intentionally bound to a non-loopback address, a bearer token is mandatory. Set `FUZECLI_API_TOKEN` before starting the server.

## Design

The interface deliberately avoids a generic dashboard template. It uses a restrained developer-tool layout: compact navigation, a dark editor-like surface, narrow typography, quiet borders, workspace-first information density, and responsive behavior that keeps the command composer central.
