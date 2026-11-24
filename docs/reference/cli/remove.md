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

A parent whose `cmd.go` you edited loses the registration and keeps the rest, and
the run warns. Its recorded hash does not move, so the next `regenerate project`
reports the edit. Removing its last child would turn the parent back into a leaf:
an edited `cmd.go` is kept instead, with or without `--force`, unless you agree
to the overwrite at the prompt.

A parent the manifest records no hash for (after `regenerate manifest`, say) is
treated the way `regenerate project` treats it, as not created by gtb: it loses
the registration, stays unrecorded, and is kept when its last child goes.

> Run with `--help` for the complete, authoritative flag set.
