# FuzeCLI MCP

FuzeCLI includes a Model Context Protocol server over stdio. It lets MCP-compatible clients use the active FuzeCLI workspace and coding engine.

## Start

From the project workspace:

```powershell
aicli mcp
```

The process communicates through stdin/stdout using the MCP stdio transport.

## Tools

### `fuze_chat`

Sends a prompt to FuzeCLI. Workspace-relative files can be attached through `files`. Valid FuzeCLI file-generation JSON is applied automatically.

Arguments:

```json
{
  "prompt": "Inspect the attached files and fix the bug",
  "files": ["src/app.go", "src/config.go"],
  "provider": "gemini",
  "model": "gemini-2.5-flash"
}
```

### `fuze_read_file`

Reads a UTF-8 text file from the active workspace.

```json
{"path":"src/app.go"}
```

### `fuze_list_files`

Lists workspace files while excluding `.git` and `.aicli`.

```json
{"prefix":"src"}
```

## Claude Desktop-style configuration

Build the executable first, then configure the MCP client to launch:

```json
{
  "mcpServers": {
    "fuzecli": {
      "command": "C:\\path\\to\\aicli.exe",
      "args": ["mcp"]
    }
  }
}
```

The client process should be started with the intended project directory as its working directory so FuzeCLI attaches the correct workspace.

## Security

The MCP server is local-process based and inherits FuzeCLI workspace path validation. File reads are constrained to the active workspace and limited to UTF-8 text files up to 64 KiB per attachment. Generated file writes continue through FuzeCLI's safe workspace resolver.
