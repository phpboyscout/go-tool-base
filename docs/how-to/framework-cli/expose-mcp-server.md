---
title: Model Context Protocol (MCP) Server
description: Guide to exposing your CLI as a Model Context Protocol (MCP) server for AI integration.
date: 2026-02-16
tags: [cli, mcp, ai, integration]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Model Context Protocol (MCP) Server

Empower your AI assistants to interact directly with your CLI using the Model Context Protocol (MCP). This integration allows tools like Cursor, Claude Desktop, and VS Code (via Copilot) to understand and execute your CLI commands, enabling powerful workflows where your AI partner can perform actions, retrieve information, and automate tasks on your behalf.

The `mcp` command transforms your CLI into an MCP server, automatically exposing your commands as callable tools for the LLM.

## Usage

To explore the available MCP commands:

```bash
my-tool mcp --help
```

### Inspecting Available Tools

To see exactly what tools are exposed to the LLM, you can export the tool definitions:

```bash
my-tool mcp tools
```

This generates an `mcp-tools.json` file in your current directory, showing the JSON Schema for each command. This is useful for:

- **Debugging**: Verify which commands are exposed and their expected parameters
- **Documentation**: Understand the input/output format for each tool
- **Validation**: Check tool definitions before deploying integrations

**Example output structure** (a JSON array, one entry per exposed command):
```json
[
  {
    "name": "my-tool_version",
    "description": "Print version, commit, and build date",
    "inputSchema": { "type": "object", "properties": {} },
    "annotations": {
      "readOnlyHint": true,
      "destructiveHint": false,
      "idempotentHint": true,
      "openWorldHint": false
    }
  }
]
```

### Telling a client what a tool does

The `annotations` block is the structured half of a tool's description: a
client uses it to decide what to confirm and what to call freely. GTB's
built-in commands carry theirs already (`version` above is read-only). For your
own commands, record them with `annotate`:

```bash
my-tool-project$ gtb annotate report --read-only --title "Spend report"
my-tool-project$ gtb annotate generate --open-world --idempotent=false
```

Each hint is tri-state and a later call changes only the hints it names. The
decision lives in the manifest (`mcp_hints`) and in the command's `cmd.go`
(`setup.AnnotateMCP`), so it survives regeneration. See the
[annotate reference](../../reference/cli/annotate.md).

## IDE Integration

Integrating your CLI with your favorite AI-powered editor is straightforward.

### Cursor

Cursor has native support for MCP. Add your CLI as a server in `~/.cursor/mcp.json` or via **Cursor Settings > Features > MCP**.

**Configuration:**

```json
{
  "mcpServers": {
    "my-tool": {
      "command": "/absolute/path/to/my-tool",
      "args": ["mcp", "start"]
    }
  }
}
```

*Note: Be sure to use the absolute path to your tool's binary.*

### Claude Desktop

To use your CLI with the Claude Desktop app, edit your configuration file:

*   **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
*   **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

Add your CLI under `mcpServers`:

```json
{
  "mcpServers": {
    "my-tool": {
      "command": "/absolute/path/to/my-tool",
      "args": ["mcp", "start"]
    }
  }
}
```

### VS Code (GitHub Copilot)

If you are using GitHub Copilot in VS Code with the "Agent Mode" (check for availability), you can configure MCP servers in your workspace settings `.vscode/settings.json` or user settings.

```json
{
  "github.copilot.mcpServers": {
    "my-tool": {
      "command": "/absolute/path/to/my-tool",
      "args": ["mcp", "start"]
    }
  }
}
```

## Debugging

If the integration isn't working as expected, you can enable debug logging to inspect the communication between the IDE and the MCP server.

Add the `--debug` flag to your configuration:

```json
{
  "mcpServers": {
    "my-tool": {
      "command": "/absolute/path/to/my-tool",
      "args": ["mcp", "start", "--debug"]
    }
  }
}
```

The debug logs will be output to `stderr` and should be visible in your IDE's MCP server logs panel.
