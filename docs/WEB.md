# FuzeCLI Web App

The FuzeCLI web app is a lightweight local chat interface for the FuzeCLI workspace.

## Start

```powershell
aicli app
```

The default address is `http://127.0.0.1:8787`.

## Interface

The app provides a focused chat surface with local conversation history, provider and model selection, workspace file visibility, drag-and-drop or button-based text-file uploads, streaming responses, generated-file notifications, and responsive mobile navigation.

Uploaded files are read in the browser and included in the next prompt as text context. Each file is limited to 64 KiB, with at most six files and 256 KiB total per message.

## Local history

Saved web conversations are kept in browser `localStorage`. The workspace conversation remains in FuzeCLI's SQLite history as well, so reopening the app can restore the current server-side conversation.

## API

The web app communicates with the local FuzeCLI web endpoints under `/api`. The standalone application API remains available through `aicli api` and is documented in `docs/API.md`.

## Design

The interface deliberately avoids a dashboard-heavy layout. The primary surface is chat, with a compact conversation sidebar and small secondary workspace/settings panels. Motion is limited to message entrance, attachment chips, streaming state, drag-and-drop feedback, and small control transitions. Reduced-motion users are respected through `prefers-reduced-motion`.
