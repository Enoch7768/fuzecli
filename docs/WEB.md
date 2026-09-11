# FuzeCLI Web App

The FuzeCLI web app is the primary local interface for the workspace-aware assistant.

## Start

```powershell
aicli app
```

The default address is `http://127.0.0.1:8787`.

## Interface

The app provides a focused chat surface with local conversation history, provider and model selection, local provider-key management, workspace file visibility, drag-and-drop or button-based text-file uploads, streaming responses, generated-file notifications, and responsive mobile navigation.

Uploaded files are read in the browser and included in the next prompt as text context. Each file is limited to 64 KiB, with at most six files and 256 KiB total per message.

## Provider keys

Open Settings and choose a provider. Enter a key only when adding or replacing it. The server stores the key in the local FuzeCLI configuration and returns only a configured/not-configured state to the browser.

The `Clear key` control removes the stored key. Keys are not written into the workspace and are not placed in browser local storage.

## Response completion

Normal model output is streamed into the active assistant message. The server emits a dedicated `chat_end` event when the response or generation task is finished. The interface handles that event exactly once, persists the completed assistant response, clears its working state, and focuses the composer so the next message can be sent immediately.

For project changes, a complete valid FuzeCLI JSON plan is detected during streaming and applied immediately. The raw JSON is not shown as ordinary chat output. The provider stream is cancelled after the valid plan is applied so unrelated trailing model text cannot extend the response.

## Local history

Saved web conversations are kept in browser `localStorage`. The workspace conversation remains in FuzeCLI's SQLite history as well, so reopening the app can restore the current server-side conversation.

## Assets

The interface uses `icon.png` and `icon-mark.png` from the workspace/root when those assets are present. `icon-mark.png` is used for the browser icon and compact brand surfaces.

## API

The web app communicates with the local FuzeCLI web endpoints under `/api`. The standalone application API remains available through `aicli api` and is documented in `docs/API.md`.

## Security

The web server binds to loopback by default, rejects foreign browser origins for state-changing requests, applies restrictive response headers, limits request bodies, and never returns provider key values through the settings API.

See [docs/SECURITY.md](SECURITY.md) for the security model.

## Design

The interface deliberately avoids a dashboard-heavy layout. The primary surface is chat, with a compact conversation sidebar and small secondary workspace/settings panels. Motion is limited to message entrance, attachment chips, streaming state, drag-and-drop feedback, and small control transitions. Reduced-motion users are respected through `prefers-reduced-motion`.
