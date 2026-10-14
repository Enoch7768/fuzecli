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

### GitHub context

The selected repository drives branch, pull-request, Actions, comments, and issue requests. Studio authenticates through the official GitHub CLI: use **Sign in with GitHub CLI** and complete the browser flow in the terminal running FuzeCLI. The backend runs `gh auth login --hostname github.com --web --git-protocol https`, configures Git with `gh auth setup-git`, and verifies the signed-in profile. Workspace Pull and Push use the Git credential helper configured by GitHub CLI. Disconnecting disables GitHub access from Studio without logging the user out of `gh`; signing in again re-enables it. Older saved PATs remain usable until replaced by a GitHub CLI sign-in.

## Security

The web UI is intended for loopback use. It receives an HttpOnly SameSite session cookie and state-changing requests are restricted to local browser origins. The server sends CSP, frame, MIME, referrer, and permissions headers.

If the API is intentionally bound to a non-loopback address, a bearer token is mandatory. Set `FUZECLI_API_TOKEN` before starting the server.

## Design

The interface uses a distinctive developer-studio visual system: deep ink surfaces, luminous lime and violet accents, layered glass panels, higher-contrast typography, subtle slide-in transitions, and responsive navigation. Motion respects `prefers-reduced-motion`.

The HTML, JavaScript, and stylesheets are embedded in the Go executable. When changing web assets, rebuild the executable and restart the running `aicli app` process; refreshing a browser tab cannot update assets embedded in an older binary.
