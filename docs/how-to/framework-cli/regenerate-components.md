---
title: Regeneration ♻️
description: Instructions for regenerating project boilerplate and manifest files to keep code in sync.
date: 2026-02-16
tags: [cli, generator, maintenance]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Regeneration ♻️

Keep your project in sync and your sanity intact with the `regenerate` commands.

As your tool evolves, the `gtb` ensures your boilerplate infrastructure keeps up. Whether you've updated your manifest or refactored your code, regeneration is the key to maintaining a healthy project.

## 1. Regenerate Project

The `regenerate project` command is your primary tool for syncing your code with your manifest.

It reads the `.gtb/manifest.yaml` file and rebuilds every generated file: the
root command, each command's `cmd.go`, the adapter links in `cmd/<name>/`, the
skeleton files and the command reference. Run it with the `gtb` binary from
the project root (a generated tool has no `regenerate` command of its own):

```bash
gtb regenerate project
```

### When to use it?

- **After editing `manifest.yaml`**: If you manually updated descriptions, flags, or command structures. For a setting, prefer [`gtb set`](change-settings.md), which edits the manifest and regenerates in one step.
- **After updating `gtb`**: To pull in the latest features and bug fixes from the base library.
- **To fix drift**: If you suspect your registration files are out of sync with your intent.

### Flags

- `--path`, `-p`: Path to the project root (default: current directory).
- `--overwrite`: How to handle a generated file you have modified: `ask` (default, prompts per file), `deny` (keeps every diverged file) or `allow` (re-emits the generator's version).
- `--force`: **Danger Zone!** Overwrites existing `main.go` implementation files. Use this only if you want to reset a command's logic to the default starter code. On a project still using the legacy flat docs layout, `--force` also **migrates the docs to the Diátaxis layout** (see below).
- `--no-verify`: Skip `go mod tidy` and `golangci-lint` afterwards; the run exits 0 unverified. Without it a failed step exits 3.
- `--update-docs`: Use AI to update the existing command documentation.
- `--dry-run`: Preview all changes without writing to disk (see below).

### What it does

- **Rebuilds `cmd.go`**: Updates Cobra definitions, flags, and descriptions.
- **Rewrites the adapter links**: `cmd/<name>/chat.go`, `forge.go` and `keychain.go` follow the manifest's chat providers and enabled features, so a provider or forge removed from the manifest leaves the binary, and a feature the tool does not use leaves no file (no `chat.go` without `ai`, no `forge.go` without a forge).
- **Injects Imports**: Ensures all subcommands are correctly imported and registered in parent commands.
- **Manages Lifecycle Files**: Creates or removes `init.go` based on the `with_initializer` value in the manifest for each command. If `with_initializer` is enabled but the `Init<Name>` stub is missing from `main.go`, it is appended automatically.
- **Runs Linting**: Automatically executes `go mod tidy` and `golangci-lint run --fix` to ensure the generated code is squeaky clean.
- **Conflict Detection**: Checks whether a generated file (a `cmd.go`, or a skeleton file such as `.goreleaser.yaml`) has been modified since it was written and, under the default `--overwrite ask`, prompts per file before overwriting; `deny` keeps every diverged file and `allow` re-emits the skeleton's version wholesale.
- **Leaves your files alone**: `README.md`, `docs/index.md`, `justfile` and the init seed config are scaffolded once and never overwritten, whatever `--overwrite` says. Any other file you take over goes in [`.gtb/ignore`](../configure-generator-ignore.md).

### When a skeleton fix reaches a file you have customised

A fix to a skeleton template lands in your project only where the file is
unmodified. A customised file is a conflict, and `--overwrite allow` would
replace your whole file with the skeleton's, so apply such a fix by hand.

The worked case: gtb releases before v0.43 emitted `.goreleaser.yaml` with
`main: cmd/<name>/main.go`, which builds one file and drops the other
`package main` files the generator writes beside it (`keychain.go`,
`signing.go`), so released binaries lacked the keychain and signing backends
their source declared. The skeleton now emits `main: ./cmd/<name>`. A project
whose `.goreleaser.yaml` carries a `signs:`, `notarize:` or `uploads:` block
picks that up by editing the `main:` line, not by regenerating.

### Migrating docs to the Diátaxis layout

If the project still uses the legacy flat docs layout (`docs/commands/`, `docs/packages/`), `regenerate project --force` migrates it to the [Diátaxis](https://diataxis.fr/) quadrant layout:

- **Moves** existing command pages into `docs/reference/cli/` and package pages into `docs/explanation/components/`, preserving your hand-written content (pages are moved, not regenerated).
- **Stamps** `docs_layout: diataxis` on `.gtb/manifest.yaml` so future generation targets the new tree.
- **Removes** the old `docs/commands/` and `docs/packages/` trees.

!!! tip "Commit first"
    The migration deletes the old trees once content has moved. Commit (or stash) your work before running it so the move is easy to review and revert.

### Dry-Run Mode

Use `--dry-run` to preview what `regenerate project` would do without modifying any files:

```bash
gtb regenerate project --dry-run
```

This produces a summary of:

- **Files to create**: New files that would be generated.
- **Files to modify**: Existing files that would change, shown as unified diffs.

Under the hood, the dry-run materialises all generated files into a temporary directory, runs the same post-processing steps as a real regeneration (`go mod tidy`, `golangci-lint run --fix`), and diffs the result against your current project. This ensures the preview is accurate, including formatting and import tidying.

!!! tip
    Dry-run is particularly useful after editing `manifest.yaml` to verify that a `regenerate project` will produce the changes you expect before committing to them.

---

## 2. Regenerate Manifest

The `regenerate manifest` command works in the opposite direction. It scans your existing Go source code and rebuilds the `manifest.yaml`.

```bash
gtb regenerate manifest
```

### When to use it?

- **After manual refactoring**: If you moved command files around manually and want the manifest to reflect the new structure.
- **Recovering a lost manifest**: If your `manifest.yaml` was deleted or corrupted, this can reconstruct it from your code.

### Flags

- `--path`, `-p`: Path to the project root (default: current directory).

### How it works

It parses your project's AST to find the `setup.Wrap`-ped `cobra.Command` definitions and reconstructs the manifest: command names/descriptions/aliases/args, flag definitions, parent/child relationships, per-command options (`with_assets`, `pre_run` hooks, `with_initializer`), and project-level properties, including what the `provenance.go` file records for settings that leave no other trace in the source. For the full extraction rules, see the [regenerate command explanation](../../explanation/components/cli/commands/regenerate.md).

!!! tip "Source of Truth"
    While `regenerate manifest` is a powerful recovery tool, we recommend treating the **Manifest** as your source of truth and driving changes through it (or `generate` commands) rather than the other way around.
