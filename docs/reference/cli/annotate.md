---
title: annotate Command
description: Record a generated command's MCP tool annotations (title, read-only, destructive, idempotent, open-world) in the manifest and its cmd.go.
date: 2026-09-17
tags: [reference, commands, annotate, mcp, gtb]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `annotate` Command

`gtb annotate` records the MCP tool annotations a command declares about itself
and re-renders that command's `cmd.go` to carry them. Part of the
**framework-developer** CLI. The annotations are the structured counterpart to
the command's description: an MCP client reads them to decide what to surface,
what to confirm and what to call freely, without parsing prose.

## Usage

```bash
gtb annotate <command-path> [flags]
```

`<command-path>` is slash-separated: `post`, or `post/due` for a subcommand.

## Flags

| Flag | Meaning |
|---|---|
| `--title <text>` | Display title shown by MCP clients. |
| `--read-only[=false]` | The command mutates nothing. |
| `--destructive[=false]` | The command may remove or rewrite state. |
| `--idempotent[=false]` | Repeating the call with the same arguments has no further effect. |
| `--open-world[=false]` | The command reaches outside the tool (a provider, a release source). |
| `--clear` | Remove every recorded annotation. Cannot be combined with a hint flag. |
| `--path, -p` | Project root (default `.`). |

Each hint flag is **tri-state**. Given bare (`--read-only`) it records `true`;
given as `--read-only=false` it records `false`; not given, the recorded value is
left as it is. So a second `annotate` merges with the first rather than
replacing it. An invocation that names no hint and no `--clear` is an error, not
a silent no-op.

## What it writes

The manifest entry gains an `mcp_hints` block holding only the fields that were
set:

```yaml
- name: post
  description: publish
  mcp_hints:
    title: Publish
    read_only: false
    open_world: true
```

The command's `cmd.go` gains one call after its exposure marker:

```go
setup.AnnotateMCP(cmd, setup.MCPHints{Title: "Publish", ReadOnly: new(false), OpenWorld: new(true)})
```

`setup.AnnotateMCP` **adds** the keys `title`, `readOnlyHint`, `destructiveHint`,
`idempotentHint` and `openWorldHint` to the command's `Annotations`, beside
GTB's own feature and exposure keys. The block round-trips through both
`regenerate project` (manifest to code) and `regenerate manifest` (code to
manifest).

## Rules

- **Hints do not inherit.** A hint describes one executable command; a group
  says nothing about its children. Exposure (`enable`/`disable mcp`) inherits,
  hints do not.
- **A protected command is refused.** Unprotect it first.
- **The title follows the description rules**: at most 500 bytes, no control
  characters, no `{{` or `}}`.
- **Built-ins already carry hints.** `version`, `docs`, `changelog`, `doctor`,
  `config get|list|validate` and `telemetry status` are read-only; `config
  set|unset` and `telemetry enable|disable` are local writes; `update` and
  `init` are open-world; `config migrate-credentials` is destructive. See
  [AI Agents & MCP](../../explanation/components/mcp-agents.md#telling-a-client-what-a-command-does).

## Examples

```bash
# A read-only report
gtb annotate report --read-only --title "Spend report"

# A provider call that spends money
gtb annotate generate --open-world --idempotent=false --destructive=false

# Flip one hint later; the title and the rest stay as recorded
gtb annotate generate --idempotent

# Start again
gtb annotate generate --clear
```

## See also

- [enable / disable](enable-disable.md) for per-command MCP exposure.
- [Exposing an MCP Server](../../how-to/framework-cli/expose-mcp-server.md).
- [Spec 0201](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0201-gtb-consumes-go-mcp).
