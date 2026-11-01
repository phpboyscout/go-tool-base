---
title: Expose your CLI to AI agents
description: Serve a generated CLI's commands to an AI assistant over the Model Context Protocol, see exactly what an agent can reach, and keep a command off that surface.
date: 2026-10-30
tags: [tutorial, mcp, ai, agents, cli]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Expose your CLI to AI agents

By the end of this, an AI assistant can call the commands of the `mytool` project
from [Build your first CLI](build-your-first-cli.md), and you'll know exactly
which ones it can reach. You won't write any AI code. A command already has a
name, a description and typed flags, which is what an agent needs to call it.

Allow about fifteen minutes.

## Before you start

- **The `mytool` project** from [Build your first CLI](build-your-first-cli.md),
  with its `hello` command. [Configure your CLI](configure-your-cli.md) also adds
  `greet`, which this page uses in the last step. Any second command will do.
- **The same gtb release.** Everything below is verified against **v0.50.0**.
- **An MCP-capable assistant** (Claude Desktop, Cursor or VS Code) if you want to
  try the last step for real. The rest runs without one.

## Check the server is there

The [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) is the open
standard assistants use to call local tools. `mcp` is one of the default
features, so your tool has shipped it since you scaffolded:

```bash
./bin/mytool mcp --help
```

```
Available Commands:
  claude      Manage Claude Desktop MCP servers
  cursor      Manage Cursor MCP servers
  start       Start stdio server
  stream      Stream the MCP server over HTTP
  tools       Export available MCP tools
  vscode      Manage VS Code MCP servers
```

## See what an agent can reach

Export the tool definitions:

```bash
./bin/mytool mcp tools
```

That writes `mcp-tools.json` in the current directory: one entry per command an
agent can call. Here's `hello`, trimmed:

```json
{
  "name": "mytool_hello",
  "description": "Say hello",
  "inputSchema": {
    "properties": {
      "args":  { "type": "array", "items": { "type": "string" } },
      "flags": { "properties": { "output": { "type": "string", "default": "text" } } }
    }
  }
}
```

The name is the tool's name and the command path joined with underscores. The
description is the `--short` text you gave the command. The flags are the
command's own, with their types and help. There's no second schema to keep in
step, because the command tree is the schema.

Hidden and deprecated commands, pure command groups, and the `mcp`, `help` and
`completion` plumbing are left out. So the file is a complete audit: it lists
everything an agent can reach and nothing else.

## Start the server

```bash
./bin/mytool mcp start
```

It logs a couple of lines to standard error (`server run start`,
`server connecting`) and waits. It's speaking MCP over standard input and
output, for an assistant to launch and talk to. Press Ctrl-C to stop it. You won't
often run it by hand.

When an agent calls one of your commands, the server runs your own binary with
the arguments the agent supplied and hands back what it printed. The agent
runs `mytool hello`, the same command you would.

By default the server publishes a compact surface: three tools, `search_tools`,
`get_tool_details` and `call_tool`, that let an agent find and call your
commands without loading every schema up front. That scales to a tool with a
hundred commands. If you'd rather publish one tool per command, run
`gtb set mcp.mode direct` and rebuild. The
[mcp reference](../reference/cli/mcp.md) covers both.

## Point an assistant at it

Let the tool write the client config. For Claude Desktop:

```bash
./bin/mytool mcp claude enable
```

```
Enabled MCP server "mytool" in /home/you/.config/Claude/claude_desktop_config.json
```

The path depends on your platform. `cursor` and `vscode` work the same way. The
entry it writes points the client at your binary with `mcp start` as its
arguments:

```json
{
  "mcpServers": {
    "mytool": {
      "command": "/home/you/mytool/bin/mytool",
      "args": ["mcp", "start"]
    }
  }
}
```

That's the whole shape, so a client the tool doesn't know about can be wired by
hand. Restart the assistant, ask it to say hello, and it calls `mytool_hello`.

The binary path is absolute. If you move or rebuild the tool somewhere else, run
`enable` again.

## Keep a command off the surface

An agent can call the commands you shipped, with the flags you defined, and
nothing more. Exposing the tool over MCP doesn't widen what it can do. But you
may still want a command left out. Regenerate `greet` with exposure turned off:

```bash
gtb generate command --name greet --short "Greet someone" --with-initializer --mcp-enabled=false
just build
./bin/mytool mcp tools
```

`mytool_greet` is gone from `mcp-tools.json`, and `./bin/mytool greet` still
runs on the command line. Pass the same flags the command was generated with:
an option you leave off is removed, so dropping `--with-initializer` here would
delete its `init.go`. Its setup step, `mytool_init_greet`, is still listed,
because that's a separate command.

To tell an assistant how a command behaves, for example that it only reads or
that it reaches the network, see `gtb annotate --help`.

## Where this leaves you

Your CLI is an agent-callable tool, with no AI code of your own. Every command
you add from here is a new tool the agent gets, and `mcp tools` shows you the
full list whenever you want to check it.

## What to read next

- [mcp reference](../reference/cli/mcp.md): `stream` for HTTP, and the
  compact and direct modes.
- [AI agents and MCP](../explanation/components/mcp-agents.md): how the server
  runs your commands, and why it publishes a compact surface by default.
