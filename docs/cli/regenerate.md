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

It reads the `.gtb/manifest.yaml` file and rebuilds all the `cmd.go` files, registration logic, and asset bundles.

```bash
go run main.go regenerate project
```

### When to use it?

- **After editing `manifest.yaml`**: If you manually updated descriptions, flags, or command structures.
- **After updating `gtb`**: To pull in the latest features and bug fixes from the base library.
- **To fix drift**: If you suspect your registration files are out of sync with your intent.

### Flags

- `--path`, `-p`: Path to the project root (default: current directory).
- `--force`: **Danger Zone!** Overwrites existing `main.go` implementation files. Use this only if you want to reset a command's logic to the default starter code.

### What it does

- **Rebuilds `cmd.go`**: Updates Cobra definitions, flags, and descriptions.
- **Refreshes Assets**: Re-bundles any static assets into the binary.
- **Injects Imports**: Ensures all subcommands are correctly imported and registered in parent commands.
- **Manages Lifecycle Files**: Creates or removes `init.go` based on the `withInitializer` value in the manifest for each command. If `withInitializer` is enabled but the `Init<Name>` stub is missing from `main.go`, it is appended automatically.
- **Runs Linting**: Automatically executes `golangci-lint run --fix` to ensure the generated code is squeaky clean.
- **Conflict Detection**: Checks if `cmd.go` files have been manually modified and prompts for confirmation before overwriting (unless `--force` is used).

---

## 2. Regenerate Manifest

The `regenerate manifest` command works in the opposite direction. It scans your existing Go source code and rebuilds the `manifest.yaml`.

```bash
go run main.go regenerate manifest
```

### When to use it?

- **After manual refactoring**: If you moved command files around manually and want the manifest to reflect the new structure.
- **Recovering a lost manifest**: If your `manifest.yaml` was deleted or corrupted, this can reconstruct it from your code.

### Flags

- `--path`, `-p`: Path to the project root (default: current directory).

### How it works

It parses your project's AST (Abstract Syntax Tree) to find `cobra.Command` definitions and extracts:

- Command names, descriptions, aliases, and positional argument validation.
- Flag definitions (name, type, description, shorthand, default, required, persistent).
- Parent/child relationships, including the variadic-argument registration pattern used by the generated root command.
- Enabled options per command: `withAssets` (detected from `assets/` directory or `//go:embed` directive), `persistentPreRun`/`preRun` (detected from hook function calls), and `withInitializer` (detected from the presence of `init.go`).
- Project-level properties: `name`, `description`, `features`, and `release_source` are recovered from the `props.Props{Tool: ...}` literal in `pkg/cmd/root/cmd.go`.

!!! tip "Source of Truth"
    While `regenerate manifest` is a powerful recovery tool, we recommend treating the **Manifest** as your source of truth and driving changes through it (or `generate` commands) rather than the other way around.
