---
title: generate Command
description: Framework-developer command to scaffold new projects, commands, flags, and docs with the gtb CLI.
date: 2026-06-26
tags: [reference, commands, generate, scaffolding, gtb]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# `generate` Command

`gtb generate` scaffolds new projects and commands. It is part of the
**framework-developer** CLI (the `gtb` binary), used while building a tool — not a
runtime command shipped in your tool. See the
[Scaffolding](../../how-to/framework-cli/scaffold-project.md) and
[Generating Commands](../../how-to/framework-cli/generate-commands.md) how-tos for
task walkthroughs.

## Usage

```bash
gtb generate <subcommand> [flags]
```

## Subcommands

| Subcommand | Purpose |
|---|---|
| `project` | Generate a new project skeleton. |
| `command` | Generate a new command or subcommand. |
| `add-flag` | Add a flag to an existing command. |
| `protect <command-path>` | Mark a command as protected from regeneration overwrites. |
| `unprotect <command-path>` | Allow a command to be overwritten again. |
| `docs` | Generate Markdown documentation for a command or package. |
| `man` | Generate roff man pages for the command tree. |

### `generate project`

Generate a new project skeleton.

| Flag | Default | Description |
|------|---------|-------------|
| `--name, -n` | — | Project/module name. |
| `--repo, -r` | — | Repository (`org/name`). |
| `--backend` | `github` | Git backend: `github` or `gitlab`. |
| `--host` | *(backend default)* | Git host (for self-managed instances). |
| `--private` | `false` | Mark the repository private. |
| `--description, -d` | `A tool built with gtb` | Project description. |
| `--features, -f` | — | Built-in features to enable. |
| `--go-version` | *(toolchain)* | Go version for `go.mod`. |
| `--help-channel` | `none` | Help channel: `slack`, `teams`, or `none` (with `--slack-*`/`--teams-*`). |
| `--path, -p` | `.` | Output directory. |

### `generate command`

Generate a new command or subcommand (optionally AI-converted from a script).

| Flag | Default | Description |
|------|---------|-------------|
| `--name, -n` | — | Command name (kebab-case). |
| `--short, -s` / `--long, -l` | — | Short / long help text. |
| `--root` | `root` | Parent command; use `parent/child` for deep nesting. |
| `--args` | — | Positional-arg validator (e.g. `ExactArgs(1)`, `ArbitraryArgs`). |
| `--alias, -a` | — | Command alias(es). |
| `--flag, -f` | — | Flag spec(s) to add (repeatable). |
| `--assets` | `false` | Include assets-directory support. |
| `--script` | — | Path to a script (bash/python/js) to convert to Go. |
| `--prompt` | — | Natural-language description (or a file path) to generate from. |
| `--agentless` | `false` | Use the original retry loop instead of the autonomous repair agent. |
| `--max-steps` | `0` (→20) | Max repair-agent reasoning steps. |
| `--persistent-pre-run` / `--pre-run` | `false` | Generate the corresponding hook. |
| `--path, -p` | `.` | Project root. |

### `generate add-flag`

Add a new flag to an existing command (see [Add Flags](../../how-to/framework-cli/add-flags.md)).

### `generate docs`

Generate Markdown docs for a command or package.

| Flag | Default | Description |
|------|---------|-------------|
| `--command` | — | Name/path of the command to document. |
| `--package` | — | Package to document (relative to project root). |
| `--parent` | — | Parent command name (if not in the manifest). |
| `--agentless` | `false` | Skip AI generation; write boilerplate only. |
| `--path` | `.` | Project root. |

One of `--command`/`--package`/`--source` is required. (`--source` is deprecated; use `--command`.)

### `generate man`

Generate roff man pages for the command tree.

| Flag | Default | Description |
|------|---------|-------------|
| `--dir` | — | Output directory (pages under `<dir>/man<section>`). |
| `--section` | — | Man section number. |
| `--source` | `<tool> <version>` | `TH` source footer. |
| `--manual` | `<Tool> Manual` | `TH` manual title. |

### `generate protect` / `generate unprotect`

`gtb generate protect <command-path>` marks a command so regeneration won't
overwrite it; `unprotect` reverses it. See
[Configure Generator Ignore](../../how-to/configure-generator-ignore.md).

> Run any subcommand with `--help` for the complete, authoritative flag set.
