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

## Gating sensitive commands

By default **every runnable command is exposed as an MCP tool**. For tools with
commands that publish, spend irreversibly, or touch secrets (`post`, `approve`,
`auth`, `deploy`, …), you usually want those **off the MCP surface** so an
assistant cannot invoke them unprompted — while keeping them fully usable on the
CLI. GTB provides per-command, **build-time** exposure control for exactly this.

### The model

- **Exposed by default.** A command with no decision is an MCP tool, exactly as
  before. Tools that gate nothing behave identically to earlier versions.
- **Explicitly excluded per command.** The decision is a property of the
  command's own manifest entry (`mcp_enabled`), tri-state:
  `true` = exposed, `false` = excluded, absent = inherit.
- **Subtree inheritance with override.** Excluding a parent withholds its whole
  subtree; a descendant may set itself back to exposed to override an excluded
  ancestor (and that descendant's own subtree follows it, until some deeper
  command excludes again). Resolution takes the **nearest explicit decision**
  walking up the command tree, defaulting to exposed.
- **Build-time only — no runtime toggle.** Exposure is baked into the binary as
  a command annotation; no config file, env var, or flag can re-expose an
  excluded command at runtime. The MCP tool surface is therefore **fixed and
  auditable in the shipped binary** — a deliberate security property, not an
  oversight. Changing it requires re-generating from an updated manifest (or the
  `gtb enable/disable mcp` verbs below) and shipping a new build.

### Turning exposure on and off

For a tool built with GTB's generator, use the framework verbs (they update
`.gtb/manifest.yaml` and re-render the affected command's `cmd.go`). `gtb
enable/disable mcp` is **dual-purpose** — with **no argument** it toggles the
`mcp` *feature* (the MCP server subsystem); with **one or more command paths** it
gates those commands' exposure:

```bash
# Withhold a command (and its subtree) from the MCP tool surface
gtb disable mcp post

# Put a command back on the surface (records an explicit, auditable re-enable)
gtb enable mcp post

# Gate several at once (each is a command path)
gtb disable mcp post approve auth

# No argument → toggles the mcp FEATURE on/off (not per-command exposure)
gtb disable mcp
```

Or decide at scaffold time:

```bash
# Generate a command kept off the MCP surface (still runnable on the CLI)
gtb generate command -n post --mcp-enabled=false
```

The interactive `gtb generate command` wizard asks **"Expose to MCP?"** as a
dedicated step (defaulting to expose), so the decision is a conscious one.

A protected command is refused by `enable/disable mcp` — unprotect it first.

### How it works

GTB stamps a `setup.ExcludeFromMCP(cmd)` or `setup.IncludeInMCP(cmd)` marker
into the generated command constructor (`pkg/setup`, `MCPExposure` enum), and
composes a single `ophis` selector in the root that drops any command resolving
to excluded via `setup.IsExposedToMCP` (the nearest-ancestor walk). The marker
round-trips through both `regenerate project` (manifest → code) and `regenerate
manifest` (code → manifest), so the gating is never silently lost.

See the [generated command exposure
spec](../../development/specs/2026-06-19-mcp-command-exposure-gating.md) for the
full design, and `setup.IsExposedToMCP` / `setup.ExcludeFromMCP` for the API.

## Common Use Cases

- Integrating your CLI with AI coding assistants (e.g., Cursor, Windsurf).
- Enabling autonomous agents to perform infrastructure or DevOps tasks.
- Providing a standard interface for AI-to-tool communication.

## Implementation

The MCP command is powered by the **[ophis](https://github.com/njayp/ophis)** library and is automatically wired into the `root` command registration.

### Why Ophis?

GTB uses `ophis` rather than the official `modelcontextprotocol/go-sdk` for several key reasons:

1.  **Seamless Cobra Integration**: Ophis is specifically designed to read Cobra command trees directly. It automatically maps commands to MCP tool definitions and flags to tool parameters, eliminating the need for manual schema duplication.
2.  **Small Footprint**: It acts as a thin translation layer, providing exactly what is needed for CLI-to-MCP bridging without the overhead of a full protocol framework.
3.  **Transitve Compatibility**: The official MCP Go SDK is a transitive dependency of Ophis. If direct protocol access is ever needed or if Ophis is abandoned, migrating to the official SDK is straightforward as the protocol layer is already present in the dependency tree.

For detailed integration instructions, see the [MCP Server CLI guide](mcp.md).
