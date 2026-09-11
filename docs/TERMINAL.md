# FuzeCLI Terminal

Start the interactive terminal client from the workspace root:

```powershell
aicli init
aicli chat
```

The terminal is workspace-aware and can handle ordinary conversation and project changes in the same prompt flow.

## Commands

```text
/file
/file <relative-path>
/file list
/file clear
/provider <name>
/model <name>
/status
/clear
/help
/exit
```

`/file` opens the Windows file picker. Direct file attachment paths must remain inside the workspace and are limited to UTF-8 text files of 64 KiB or less.

## API keys

Provider keys can be configured from the web Settings screen or through the existing configuration command:

```powershell
aicli config set gemini.api_key YOUR_KEY
```

Do not paste credentials into source files or commit the configuration directory.

## Generation

For a project change, describe the requested change naturally. The model is instructed to return strict FuzeCLI file JSON. When a complete valid document is detected, FuzeCLI applies it immediately, prints the changed files, and marks the response complete.

The terminal streams normal answers as they arrive. Once the provider stream signals completion, the prompt is ready for the next request.

Generated command metadata is never executed automatically.
