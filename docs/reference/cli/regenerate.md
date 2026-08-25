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
| `--overwrite` | `ask` | Conflict handling: `allow`, `deny`, or `ask`. Applies to every generated file: skeleton assets and per-command `cmd.go`/`init.go`/`main_test.go` alike. |
| `--update-docs` | `false` | Use AI to update existing documentation. |
| `--dry-run` | `false` | Preview changes without writing. |

#### Conflicts

A generated file whose content no longer matches the hash recorded in the
manifest raises a conflict. Keeping it is a **skip, not a failure**: the file is
left exactly as it is, the run continues through every remaining command, and
the summary at the end names what was kept along with the rule that would make
it permanent.

```
WARN kept your version path=pkg/cmd/deploy/cmd.go reason="declined at the prompt" remedy="gtb ignore add pkg/cmd/deploy/cmd.go"
```

The exit code is **0**. You asked for your changes to be kept and they were
kept. Only a genuine failure (an unreadable file, a render fault) aborts the run.

- `--overwrite ask` (default) prompts per file. With no usable terminal: under
  `--ci`, `CI=true`, `GTB_NON_INTERACTIVE=true`, or no TTY. Nothing is
  prompted and every conflict resolves to keep.
- `--overwrite allow` rebuilds everything, `.gtb/ignore` still excepted. This is
  the deterministic mode for a pipeline that regenerates.
- `--overwrite deny` keeps every diverged file without asking.

A file covered by [`.gtb/ignore`](../../how-to/configure-generator-ignore.md) is
not compared, prompted about or written at all, and outranks both `--force` and
`--overwrite allow`. To find diverged files before regenerating, run
`gtb doctor`.

A plain rule stops the file being **regenerated**. It does not stop the
localised edits that wire a subcommand into its parent, refusing those would
leave the command absent from the built CLI with nothing to say why. Add the
`sealed` attribute (`gtb ignore seal <path>`) to forbid every write; the run
then names what it could not register and still exits 0.

### `regenerate manifest`

| Flag | Default | Description |
|------|---------|-------------|
| `--path, -p` | `.` | Project root. |
| `--dry-run` | `false` | Preview changes without writing. |

> Run any subcommand with `--help` for the complete, authoritative flag set.
