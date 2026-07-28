---
title: remove Command
description: Framework-developer command to remove a generated command from a project.
date: 2026-06-26
tags: [reference, commands, remove, gtb]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `remove` Command

`gtb remove` removes generated components from a project. Part of the
**framework-developer** CLI. See [Removing Commands](../../how-to/framework-cli/remove-commands.md).

## Usage

```bash
gtb remove command --name <name> [flags]
```

## Subcommands

| Subcommand | Purpose |
|---|---|
| `command` | Remove a command from the project. |

### `remove command`

| Flag | Default | Description |
|------|---------|-------------|
| `--name, -n` | — | Command name to remove (kebab-case). |
| `--parent` | `root` | Parent command name. |
| `--path, -p` | `.` | Project root. |
| `--force, -f` | `false` | Remove even if the command is marked protected in the manifest. |

A command marked **protected** in the manifest is refused (it carries
hand-written logic that the whole-directory removal would destroy). Pass
`--force` to override the guard and remove it anyway. Removal also deletes the
command's documentation page (resolved through the project's docs layout, flat
or Diátaxis) and de-registers it from its parent.

> Run with `--help` for the complete, authoritative flag set.
