---
title: MCP Command
description: Expose CLI functionality to AI agents using the Model Context Protocol (MCP).
date: 2026-02-16
tags: [components, commands, mcp, ai, agents]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# MCP Command

The `mcp` command exposes your CLI tool's functionality to AI agents using the **[Model Context Protocol (MCP)](https://modelcontextprotocol.io/)**.

## Usage

```bash
mytool mcp [subcommand] [flags]
```

## Description

Starts an MCP server that allows AI assistants (like Claude or Gemini) to discover and execute your CLI's commands as tools. This enables agentic workflows where an AI can autonomously use your tool to perform tasks.

## Subcommands

### `mcp start`

Starts the MCP server over standard I/O.

```bash
mytool mcp start [--debug]
```

| Flag | Description |
| :--- | :--- |
| `--debug` | Enable debug logging for MCP communication |

### `mcp tools`

Exports the tool definitions to a JSON file for inspection.

```bash
mytool mcp tools
```

This generates an `mcp-tools.json` file in your current directory, showing the JSON schema for each exposed command. This is useful for:

- Debugging which commands are exposed to AI agents
- Understanding the expected input/output format
- Validating tool definitions before deployment

## Common Use Cases

- Integrating your CLI with AI coding assistants (e.g., Cursor, Windsurf).
- Enabling autonomous agents to perform infrastructure or DevOps tasks.
- Providing a standard interface for AI-to-tool communication.

## Implementation

The MCP command is powered by the `ophis` library and is automatically wired into the `root` command registration.

For detailed integration instructions, see the [MCP Server CLI guide](../../cli/mcp.md).
