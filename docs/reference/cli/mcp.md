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

### `mcp stream`

Starts the MCP server over a networked **streamable-HTTP** endpoint, serving the
same tools as `mcp start` but over HTTP instead of standard I/O.

```bash
mytool mcp stream [--host <host>] [--port <port>]
```

| Flag | Description |
| :--- | :--- |
| `--host` | Host/interface to bind the HTTP listener to |
| `--port` | Port to serve the streamable-HTTP endpoint on |

Use `mcp start` (stdio) for editor and desktop integrations that launch your
tool as a subprocess (e.g. Claude Desktop); use `mcp stream` for remote or
networked clients that connect to a running HTTP endpoint rather than spawning
the process locally.

### `mcp tools`

Exports the tool definitions to a JSON file for inspection.

```bash
mytool mcp tools
```

This generates an `mcp-tools.json` file in your current directory, showing the JSON schema for each exposed command. This is useful for:

- Debugging which commands are exposed to AI agents
- Understanding the expected input/output format
- Validating tool definitions before deployment

## Gating sensitive commands

Every command in the tree is on the MCP tool surface by default. A command that
should stay runnable on the CLI but *not* be callable by an agent is excluded at
build time:

```go
setup.ExcludeFromMCP(mycmd.NewCmdDeploy(props))
```

Exclusion is inherited: descendants of an excluded command are excluded too,
unless one of them opts back in.

```go
setup.IncludeInMCP(mycmd.NewCmdDeployStatus(props))
```

`IsExposedToMCP` walks a command and its ancestors and takes the nearest
explicit decision: exposed wins for that command, excluded hides the subtree,
and a tree with no decision anywhere is exposed.

The generator carries the same tri-state through `.gtb/manifest.yaml`:

```bash
gtb generate command -n post --mcp-enabled=false
```

Omitting the flag leaves the decision unset (inherit); `--mcp-enabled=true`
records an explicit exposure, which is how you re-expose one subcommand of an
excluded parent.

### Exposure is build-time only

There is no configuration key or flag that changes the MCP surface at runtime.
The decision is baked into the binary as a cobra annotation
(`gtb.mcp.exposure`), so the surface a shipped binary presents is fixed and
auditable. An operator cannot widen it, and a compromised config file cannot
either.

Confirm what a given build actually exposes with `mytool mcp tools`, which
writes the full tool definitions to `mcp-tools.json`.
