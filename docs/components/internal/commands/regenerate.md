---
title: Regenerate Command
description: Internal command for synchronizing project state with the manifest file.
date: 2026-02-16
tags: [components, internal, commands, regenerate]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Regenerate Command

The `regenerate` command group is used to synchronize the project state with the `manifest.yaml` Source of Truth.

## Help Output (`regenerate --help`)

```text
Regenerate project components from manifest or rebuild the manifest from existing source code.

Usage:
  gtb regenerate [command]

Available Commands:
  manifest    Regenerate manifest from source code
  project     Regenerate project from manifest

Flags:
  -h, --help   help for regenerate

Global Flags:
      --ci                   flag to indicate the tools is running in a CI environment
      --config stringArray   config files to use (default [/home/mcockayne/.gtb/config.yaml,/etc/gtb/config.yaml])
      --debug                forces debug log output
```

## Subcommands

### Manifest

Scans the filesystem for existing commands and updates `manifest.yaml`. Use this if you have manually added files or if the manifest is out of sync.

**Help (`regenerate manifest --help`):**

```text
Scan the project for cobra.Command definitions and rebuild the manifest.yaml file.

Usage:
  gtb regenerate manifest [flags]

Flags:
  -h, --help          help for manifest
  -p, --path string   Path to project root (default ".")
```

### Project

Re-renders all `cmd.go` boilerplate files based on the structure defined in `manifest.yaml`. This is non-destructive to `main.go` files unless `--force` is used.

**Help (`regenerate project --help`):**

```text
Regenerate all command registration files (cmd.go) based on the manifest.yaml.
Does not overwrite implementation files (main.go) unless --force is provided.

Usage:
  gtb regenerate project [flags]

Flags:
      --force         Overwrite existing main.go implementation files
  -h, --help          help for project
  -p, --path string   Path to project root (default ".")
```
