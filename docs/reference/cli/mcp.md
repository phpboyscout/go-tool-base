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

Serves your CLI's commands to AI assistants (Claude, Cursor, VS Code and any
MCP client) as tools, exports the catalogue, and registers the server with an
editor. Every call runs the command as a subprocess of the same binary.

By default the server publishes **compactly**: a client lists three discovery
tools (`search_tools`, `get_tool_details`, `call_tool`) and finds the command it
needs through them. `gtb set mcp.mode direct` publishes one native tool per
command instead. See [AI Agents & MCP](../../explanation/components/mcp-agents.md#compact-and-direct-publication).

## Subcommands

### `mcp start`

Serves over standard input and output. Logs go to standard error.

```bash
mytool mcp start [--log-level debug|info|warn|error]
```

### `mcp stream`

Serves the same tools over a **Streamable HTTP** endpoint, under the estate's
controls lifecycle: interrupting the process drains active calls and cleans up
running commands within the shutdown budget.

```bash
mytool mcp stream [--host <host>] [--port <port>] [--origin <origin>]... [--log-level <level>]
```

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--host` | all interfaces | Interface to bind |
| `--port` | `8080` | Port to serve on |
| `--origin` | none | A browser origin to trust besides the server's own (repeatable). Any other browser origin is refused with 403; clients that are not browsers send no `Origin`. |
| `--log-level` | | Log level |

Use `mcp start` for editor and desktop integrations that launch your tool as a
subprocess; use `mcp stream` for a client that connects to a running endpoint.

### `mcp tools`

Writes the full authorised catalogue as JSON.

```bash
mytool mcp tools [--output mcp-tools.json]
```

Each entry carries `name`, `title`, `description`, `inputSchema`,
`outputSchema` and `annotations`. It is an offline export of what a client
could discover; in compact mode the wire still publishes the three discovery
tools. Use it to confirm which commands a build exposes and what a client is
told about each.

### `mcp claude`, `mcp cursor`, `mcp vscode`

Register the server with an editor:

```bash
mytool mcp vscode enable [--workspace] [--config-path <file>] [--server-name <name>] [--log-level <level>] [--env KEY=value]...
mytool mcp vscode disable [--workspace] [--config-path <file>] [--server-name <name>]
mytool mcp vscode list [--workspace] [--config-path <file>]
```

`enable` writes an entry pointing at this binary's `mcp start` into the
editor's MCP configuration (Claude Desktop's `claude_desktop_config.json`,
`~/.cursor/mcp.json`, VS Code's user `mcp.json`; `--workspace` uses the
project's `.cursor/mcp.json` or `.vscode/mcp.json`). Other entries are
preserved, running it again replaces this entry only, `disable` removes it, and
a file that is not valid JSON is refused untouched.

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
writes the full tool definitions to `mcp-tools.json`. A pure command group is
never exposed, whatever the markers say: it has nothing to run.
