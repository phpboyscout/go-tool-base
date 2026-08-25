---
title: AI Agents & MCP
description: Expose your CLI as an autonomous agent using the Model Context Protocol (MCP).
date: 2026-02-16
tags: [concepts, ai, mcp, agents]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# AI Agents & MCP

GTB enables your CLI applications to act as powerful autonomous agents through native support for the **Model Context Protocol (MCP)**. Instead of just being a manual tool, your application can become a "brain extension" for an AI assistant.

## The Model Context Protocol (MCP)

MCP is an open standard that allows AI models (like Claude or Gemini) to safely discover and interact with local tools. By implementing this protocol, GTB removes the need for custom integrations or wrapper scripts for every different AI service.

### How it Works

When you build a CLI with GTB, the framework automatically maps your **Cobra command tree** to a set of **MCP Tool Definitions**:

- **Command Name** -> **Tool Name**
- **Short Description** -> **Tool Description**
- **Flags & Arguments** -> **JSON Schema Parameters**

## Exposing your Tool

Every GTB application includes a built-in `mcp` command group. `mcp start` runs a
JSON-RPC server over standard I/O, the default transport for editor and desktop
integrations such as Claude Desktop:

```bash
mytool mcp start
```

For networked clients, `mcp stream` serves the same tools over a streamable-HTTP
endpoint (configurable via `--host` and `--port`):

```bash
mytool mcp stream
```

### Integration with AI Assistants

To use your tool as an agent, you simply configure your preferred AI client (like Claude Desktop) to run your tool in MCP mode. The assistant will:

1.  **Call `mytool mcp start`** on startup.
2.  **Discover** all available commands as tools.
3.  **Contextually call** your commands when a user's prompt requires it.

## Why use MCP?

- **Universal Compatibility**: Write once, and your tool works across any AI assistant that supports the protocol.
- **Zero Effort**: No extra code required. If it's a command in your CLI, it's a tool for the agent.
- **Safety & Control**: The AI is restricted to the specific commands and parameters you've defined in your tool's manifest.

---

!!! tip
    To see how to configure specific AI clients (like Cursor or Claude Desktop) to use your tool as an agent, refer to the [MCP CLI Guide](../../reference/cli/mcp.md).


## Gating sensitive commands

By default **every runnable command is exposed as an MCP tool**. For tools with
commands that publish, spend irreversibly, or touch secrets (`post`, `approve`,
`auth`, `deploy`, …), you usually want those **off the MCP surface** so an
assistant cannot invoke them unprompted, while keeping them fully usable on the
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
- **Build-time only: no runtime toggle.** Exposure is baked into the binary as
  a command annotation; no config file, env var, or flag can re-expose an
  excluded command at runtime. The MCP tool surface is therefore **fixed and
  auditable in the shipped binary**, a deliberate security property, not an
  oversight. Changing it requires re-generating from an updated manifest (or the
  `gtb enable/disable mcp` verbs below) and shipping a new build.

### Turning exposure on and off

For a tool built with GTB's generator, use the framework verbs (they update
`.gtb/manifest.yaml` and re-render the affected command's `cmd.go`). `gtb
enable/disable mcp` is **dual-purpose**, with **no argument** it toggles the
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

A protected command is refused by `enable/disable mcp`, unprotect it first.

### How it works

GTB stamps a `setup.ExcludeFromMCP(cmd)` or `setup.IncludeInMCP(cmd)` marker
into the generated command constructor (`pkg/setup`, `MCPExposure` enum), and
composes a single `ophis` selector in the root that drops any command resolving
to excluded via `setup.IsExposedToMCP` (the nearest-ancestor walk). The marker
round-trips through both `regenerate project` (manifest → code) and `regenerate
manifest` (code → manifest), so the gating is never silently lost.

See the [generated command exposure
spec](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0089-mcp-command-exposure-gating) for the
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

For detailed integration instructions, see the [MCP Server CLI guide](mcp-agents.md).
