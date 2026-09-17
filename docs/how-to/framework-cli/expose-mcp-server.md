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

The `mcp` helpers register the tool with an editor and keep the rest of the
editor's configuration as it is. Each writes an entry pointing at this binary's
`mcp start`; run it again after moving the binary and it replaces that entry
only.

```bash
my-tool mcp cursor enable            # ~/.cursor/mcp.json
my-tool mcp cursor enable --workspace   # .cursor/mcp.json in the project
my-tool mcp claude enable            # Claude Desktop's claude_desktop_config.json
my-tool mcp vscode enable            # VS Code's user mcp.json
my-tool mcp vscode enable --workspace   # .vscode/mcp.json in the project
```

`disable` removes the entry and `list` shows what the file holds. `--env
KEY=value` adds environment for the server and `--log-level debug` starts it
verbose. What `enable` writes, for Cursor:

```json
{
  "mcpServers": {
    "my-tool": {
      "type": "stdio",
      "command": "/absolute/path/to/my-tool",
      "args": ["mcp", "start"]
    }
  }
}
```

## What a client sees

By default the client lists three tools: `search_tools`, `get_tool_details` and
`call_tool`. The assistant searches the catalogue (or browses it with an empty
query), inspects the schema of the command it wants, and calls it. A tool with
eighty commands costs the client's context the same as one with three.

If you would rather each command appear as its own native tool, with its
annotations in the client's approval UI, switch the project to direct
publication and rebuild:

```bash
gtb set mcp.mode direct
```

## Debugging

Start the server with `--log-level debug` (in the editor entry, `my-tool mcp
vscode enable --log-level debug`) to see every operation the client invokes on
stderr. `my-tool mcp tools` shows exactly which commands are exposed and what a
client is told about each, including the annotations set with `gtb annotate`.
