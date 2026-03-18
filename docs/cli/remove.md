---
title: Removing Commands 🧹
description: How to cleanly remove commands and their associated assets from your project.
date: 2026-02-16
tags: [cli, generator, maintenance]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Removing Commands 🧹

Sometimes less is more. As your tool evolves, some commands might become obsolete or deprecated. The `remove` command helps you prune your CLI without leaving ghost code behind.

## The Remove Command

The `remove command` utility cleanly excises a command from your project. It handles the cleanup across three layers:

1.  **Filesystem**: Deletes the command's directory (e.g., `pkg/cmd/my-command`).
2.  **Manifest**: Removes the entry from `.gtb/manifest.yaml`.
3.  **Registration**: Updates the parent command's `cmd.go` to remove the `AddCommand` call.

```bash
# formatting: off
go run main.go remove command --name my-command
# formatting: on
```

### Flags

-   `--name`, `-n`: **(Required)** The name of the command to remove (kebab-case).
-   `--parent`: The parent command name (default: `root`). Use path-like syntax for nested parents (e.g., `server/start`).
-   `--path`, `-p`: Path to the project root (default: current directory).

### Examples

**Removing a top-level command:**

```bash
go run main.go remove command --name status
```

**Removing a nested subcommand:**

If you have a command structure like `server -> start`, you can remove the `start` subcommand like this:

```bash
go run main.go remove command --name start --parent server
```

**Removing a deeply nested command:**

For `cloud -> provider -> aws`:

```bash
go run main.go remove command --name aws --parent cloud/provider
```

!!! warning "Destructive Action"
    This command **permanently deletes** the implementation files for the command. Make sure you have committed your changes to git before running this, just in case you delete something you didn't intend to!
