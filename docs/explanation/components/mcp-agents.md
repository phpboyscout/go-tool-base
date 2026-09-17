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
hands `setup.IsExposedToMCP` (the nearest-ancestor walk) to the go/mcp Cobra
binding as its exposure policy, so a command resolving to excluded is never
bound. The marker round-trips through both `regenerate project` (manifest → code)
and `regenerate manifest` (code → manifest), so the gating is never silently lost.

See the [generated command exposure
spec](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0089-mcp-command-exposure-gating) for the
full design, and `setup.IsExposedToMCP` / `setup.ExcludeFromMCP` for the API.

## Telling a client what a command does

Exposure says *whether* a command is a tool. MCP's tool annotations say *what
kind* of tool it is: a display `title` and four behavioural hints,
`readOnlyHint`, `destructiveHint`, `idempotentHint` and `openWorldHint`. A
client uses them to decide what to surface, what to confirm and what to call
freely, without reading the description prose. They are hints, not permissions:
a client may act on them, and nothing in GTB grants access because of one.

### The model

- **Per command, tri-state, no inheritance.** Each hint is `true`, `false` or
  unset. Unset writes no key, so a client sees only what was actually declared.
  A hint describes one executable command; a group states nothing about its
  children, unlike exposure.
- **Recorded in the manifest, rendered into the code.** The command's manifest
  entry carries an `mcp_hints` block, and its `cmd.go` carries one
  `setup.AnnotateMCP(cmd, setup.MCPHints{...})` call after the exposure marker.
  `regenerate project` and `regenerate manifest` round-trip it in both
  directions.
- **Additive.** `setup.AnnotateMCP` adds its keys to `cmd.Annotations` beside
  GTB's feature and exposure keys; it never replaces the map.
- **The keys are ophis's spellings** (`readOnlyHint` and friends). A tool that set
  them by hand under the previous library keeps working, and the go/mcp Cobra
  binding reads the same keys.

### Built-in defaults

Every command the framework ships declares its own hints, so a tool built on
GTB gets them without saying anything:

| Commands | read-only | destructive | idempotent | open-world |
|---|---|---|---|---|
| `version`, `docs`, `changelog`, `doctor`, `config get`, `config list`, `config validate`, `telemetry status` | true | false | true | false |
| `config set`, `config unset`, `telemetry enable`, `telemetry disable` | false | false | true | false |
| `update`, `init` and its subcommands | false | false | false | true |
| `config migrate-credentials` | false | true | false | false |

The four presets behind that table are `setup.MCPReadOnly()`,
`setup.MCPLocalWrite()`, `setup.MCPOpenWorld()` and `setup.MCPDestructive()`; a
hand-written command can apply one the same way:

```go
setup.AnnotateMCP(setup.Wrap("", cmd), setup.MCPReadOnly())
```

### Setting them on a generated command

```bash
gtb annotate report --read-only --title "Spend report"
gtb annotate generate --open-world --idempotent=false
gtb annotate generate --clear
```

See the [annotate reference](../../reference/cli/annotate.md) and
[spec 0201](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0201-gtb-consumes-go-mcp),
which answers [issue #36](https://gitlab.com/phpboyscout/go-tool-base/-/issues/36).

## Common Use Cases

- Integrating your CLI with AI coding assistants (e.g., Cursor, Windsurf).
- Enabling autonomous agents to perform infrastructure or DevOps tasks.
- Providing a standard interface for AI-to-tool communication.

## Implementation

The MCP command is the estate's own [`go/mcp`](https://mcp.go.phpboyscout.uk)
module, wired through `pkg/mcp` (spec 0201 D1). `pkg/mcp.NewCmdMCP` builds the
`mcp` tree with GTB's conventions applied once, so a tool built on GTB gets
them without saying anything:

- **Exposure** comes from the tree's own markers (`setup.IsExposedToMCP`), and a
  pure command group (`setup.GroupRunE`) is never published: a tool that prints
  usage is noise to a client.
- **The global flags are withheld.** `--config`, `--debug`, `--ci` and
  `--accessible` steer the process, not the command, and `--config` would let a
  client point the tool at another configuration file. `--output` stays, because
  a client wants JSON.
- **Operations are grouped by feature.** The `setup.Wrap` feature ID becomes the
  operation's group, so `search_tools` narrowed by `config` or `telemetry` means
  the same thing in every GTB tool.
- **The publication mode** comes from `props.Tool.MCP`, rendered from
  `properties.mcp.mode` in the manifest. The binary never reads the manifest.
- **Logs go to stderr** at the level the root's `--debug` and config reload
  already move; stdout carries the protocol.

### Compact and direct publication

By default a client sees **three tools**, whatever the size of the command
tree: `search_tools`, `get_tool_details` and `call_tool`. The model searches or
browses, inspects the schema of the one it needs, and calls it. This is
progressive discovery: the catalogue is not pushed into every context window,
and a tool with 78 commands costs a client the same as one with three.

The trade is native per-tool approval. A client sees `call_tool` as *the* tool,
so its annotations are conservative (destructive, open-world), and the
per-command hints from `annotate` reach the model through `get_tool_details`
rather than the client's approval UI. A tool that wants one native tool per
command, with each tool's own annotations in the client's UI, sets the mode:

```bash
gtb set mcp.mode direct
```

That records `properties.mcp.mode: direct`, renders `MCP:
props.MCPConfig{Mode: props.MCPDirect}` into the generated root, and takes
effect on the next build. `gtb set mcp.mode compact` returns to the default.
The mode is project-level publication configuration: `gtb disable mcp` still
wins, and per-command `mcp_enabled` still gates exposure.

### Execution

Every call runs the command as a **subprocess of the same binary**, with the
host's environment and working directory, stdin closed, a five-minute timeout
and one MiB of retained output. One command runs at a time; a second call gets
a retryable `busy` failure while search and inspection stay available. A
cancelled call stops the command and everything it started (a process group on
Unix, a job object on Windows). Non-zero exit is a `command_failed` result that
still carries stdout, stderr and the exit code.

`mytool mcp tools` writes the full authorised catalogue to `mcp-tools.json`
regardless of mode: it is the offline view of what a client could discover.

For the module's own contracts see [mcp.go.phpboyscout.uk](https://mcp.go.phpboyscout.uk)
and [go/mcp spec 0001](https://gitlab.com/phpboyscout/go/mcp/-/wikis/specs/0001-progressive-mcp);
for GTB's side, [spec 0201](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0201-gtb-consumes-go-mcp).
