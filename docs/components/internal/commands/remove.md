---
title: Remove Command
description: Internal command for safely removing components and their registrations.
date: 2026-02-16
tags: [components, internal, commands, remove]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Remove Command

The `remove` command group handles the safe deletion of components.

## Help Output (`remove --help`)

```text
Remove commands or other components from an existing gtb project.

Usage:
  gtb remove [flags]
  gtb remove [command]

Available Commands:
  command     Remove a command from the project

Flags:
  -h, --help   help for remove

Global Flags:
      --ci                   flag to indicate the tools is running in a CI environment
      --config stringArray   config files to use (default [/home/mcockayne/.gtb/config.yaml,/etc/gtb/config.yaml])
      --debug                forces debug log output
```

## Subcommands

### Command

Deletes a command's implementation (`pkg/cmd/<name>`) and registration, and removes it from `manifest.yaml`.

**Help (`remove command --help`):**

```text
Remove a command from the project, including filesystem cleanup, manifest update, and parent de-registration.

Examples:
  # Remove a command named 'test-command'
  gtb remove command --name test-command

  # Remove a subcommand 'child' under 'parent'
  gtb remove command --name child --parent parent

Usage:
  gtb remove command [flags]

Flags:
  -h, --help            help for command
  -n, --name string     Command name (kebab-case)
      --parent string   Parent command name (default: root) (default "root")
  -p, --path string     Path to project root (default ".")
```
