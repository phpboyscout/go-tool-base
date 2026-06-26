---
title: regenerate Command
description: Framework-developer command to regenerate a project from its manifest, or the manifest from source.
date: 2026-06-26
tags: [reference, commands, regenerate, manifest, gtb]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `regenerate` Command

`gtb regenerate` rebuilds a project from its `manifest.yaml`, or rebuilds the
manifest by scanning source. Part of the **framework-developer** CLI. See
[Regenerating Components](../../how-to/framework-cli/regenerate-components.md).

## Usage

```bash
gtb regenerate <subcommand> [flags]
```

## Subcommands

| Subcommand | Purpose |
|---|---|
| `project` | Regenerate the project from the manifest. |
| `manifest` | Regenerate the manifest from source code (AST scan). |

A persistent `--dry-run` previews changes without writing files.

### `regenerate project`

| Flag | Default | Description |
|------|---------|-------------|
| `--path, -p` | `.` | Project root. |
| `--force` | `false` | Overwrite existing `main.go` implementation files. On a flat-layout project, also migrates the docs to the [Diátaxis layout](../../explanation/concepts/documentation-layout.md). |
| `--overwrite` | `ask` | Conflict handling: `allow`, `deny`, or `ask`. |
| `--update-docs` | `false` | Use AI to update existing documentation. |
| `--dry-run` | `false` | Preview changes without writing. |

### `regenerate manifest`

| Flag | Default | Description |
|------|---------|-------------|
| `--path, -p` | `.` | Project root. |
| `--dry-run` | `false` | Preview changes without writing. |

> Run any subcommand with `--help` for the complete, authoritative flag set.
