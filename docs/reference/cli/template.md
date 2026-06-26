---
title: template Command
description: Framework-developer command to manage custom template-overlay sources on a generated project.
date: 2026-06-26
tags: [reference, commands, template, overlays, gtb]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `template` Command

`gtb template` manages custom template-overlay sources on a generated project,
letting you layer your own scaffolding over the built-in templates. Part of the
**framework-developer** CLI. See [Applying Custom Templates](../../how-to/framework-cli/apply-templates.md)
and [Custom Templates](../../how-to/custom-templates.md).

## Usage

```bash
gtb template <subcommand> [flags]
```

All subcommands accept `--path, -p` (default `.`) for the project root.

## Subcommands

| Subcommand | Purpose |
|---|---|
| `add <src>@<ref>` | Add a custom template-overlay source. |
| `update <name>` | Re-resolve a git source's ref to a new commit and regenerate. |
| `remove <name>` | Remove a source and restore any scaffold it replaced. |
| `list` | Show recorded template sources. |

### `template add <src>@<ref>`

Add an overlay source. `<src>` is a git URL or local path; `@<ref>` pins a git
ref (branch, tag, or commit).

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | *(repo/dir name)* | Source handle used by `update`/`remove`. |
| `--path, -p` | `.` | Project root. |

### `template update <name>` / `remove <name>`

`update` re-resolves the source's ref and regenerates; `remove` deletes the source
and restores any scaffold it replaced. Both take the source handle as their single
argument.

### `template list`

Show the recorded template sources for the project.

> Run any subcommand with `--help` for the complete, authoritative flag set.
